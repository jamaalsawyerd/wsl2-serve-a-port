package netinfo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/winexec"
)

// WindowsAddr is an IPv4 address on a Windows adapter that has a default
// route, i.e. one other devices could plausibly reach.
type WindowsAddr struct {
	Alias       string
	Description string
	IP          netip.Addr
	Metric      int
	// Hardware is false for virtual adapters (VPNs, ZeroTier, Hyper-V).
	Hardware bool
}

const windowsIPScript = `
$routes = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue
$out = foreach ($r in $routes) {
  $a = Get-NetAdapter -InterfaceIndex $r.ifIndex -ErrorAction SilentlyContinue
  if (-not $a -or $a.Status -ne 'Up') { continue }
  $ip = Get-NetIPAddress -InterfaceIndex $r.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
    Where-Object { $_.PrefixOrigin -ne 'WellKnown' } | Select-Object -First 1
  if (-not $ip) { continue }
  $if = Get-NetIPInterface -InterfaceIndex $r.ifIndex -AddressFamily IPv4
  [pscustomobject]@{
    alias       = $a.Name
    description = $a.InterfaceDescription
    ip          = $ip.IPAddress
    metric      = [int]($r.RouteMetric + $if.InterfaceMetric)
    hardware    = [bool]$a.HardwareInterface
  }
}
ConvertTo-Json -Compress -InputObject @($out)
`

// WindowsIPs returns the host's LAN-facing addresses, best guess first:
// physical adapters before virtual ones, then by route metric.
func WindowsIPs(ctx context.Context, ps winexec.PowerShell) ([]WindowsAddr, error) {
	out, err := ps.Command(ctx, windowsIPScript)
	if err != nil {
		return nil, fmt.Errorf("querying Windows adapters: %w", err)
	}
	return parseWindowsIPs([]byte(out))
}

func parseWindowsIPs(b []byte) ([]WindowsAddr, error) {
	var raw []struct {
		Alias       string `json:"alias"`
		Description string `json:"description"`
		IP          string `json:"ip"`
		Metric      int    `json:"metric"`
		Hardware    bool   `json:"hardware"`
	}
	if len(b) == 0 || string(b) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parsing adapter list: %w", err)
	}

	var addrs []WindowsAddr
	for _, r := range raw {
		ip, err := netip.ParseAddr(r.IP)
		if err != nil {
			continue
		}
		addrs = append(addrs, WindowsAddr{
			Alias: r.Alias, Description: r.Description,
			IP: ip, Metric: r.Metric, Hardware: r.Hardware,
		})
	}
	slices.SortStableFunc(addrs, func(a, b WindowsAddr) int {
		if a.Hardware != b.Hardware {
			if a.Hardware {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Metric, b.Metric)
	})
	return addrs, nil
}
