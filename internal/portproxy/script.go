package portproxy

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// Firewall rules we create are tagged with this group and named
// "<prefix><port>" so we can find and remove exactly our own.
const (
	FirewallGroup      = "WSL2 Serve-a-Port"
	firewallRulePrefix = "wsl2-serve-a-port-tcp-"
	hyperVRulePrefix   = "wsl2-serve-a-port-hv-tcp-"
	// wslVMCreatorID identifies WSL to the Hyper-V firewall.
	wslVMCreatorID = "{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}"
)

// OpKind is what to do with a port.
type OpKind string

const (
	// OpAdd forwards a port. It's idempotent, so it also repairs a rule
	// pointing at a stale WSL IP.
	OpAdd OpKind = "add"
	// OpRemove deletes the forward and firewall rule. Already-missing rules
	// count as success.
	OpRemove OpKind = "remove"
)

// Op is one change to apply.
type Op struct {
	Kind OpKind
	Port int
	// ConnectAddress is the WSL IP; required for OpAdd in NAT mode.
	ConnectAddress netip.Addr
}

// Result is the outcome of one Op, as reported by the elevated script.
type Result struct {
	Op      OpKind `json:"op"`
	Port    int    `json:"port"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

const scriptHeader = `param([string]$ResultPath)
$ErrorActionPreference = 'Stop'
$results = New-Object System.Collections.Generic.List[object]
function Add-Result($op, $port, $ok, $msg) {
  $results.Add([pscustomobject]@{ op = $op; port = $port; ok = $ok; message = $msg })
}
# Under 'Stop', PowerShell 5.1 turns native stderr into terminating errors,
# so the netsh helpers relax it and check $LASTEXITCODE instead.
function Invoke-Netsh([string[]]$a) {
  $ErrorActionPreference = 'Continue'
  $o = & netsh.exe @a 2>&1 | Out-String
  if ($LASTEXITCODE -ne 0) { throw $o.Trim() }
}
function Remove-Proxy($port) {
  $ErrorActionPreference = 'Continue'
  & netsh.exe interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=$port 2>&1 | Out-Null
}
try {
`

const scriptFooter = `} finally {
  ConvertTo-Json -Depth 4 -InputObject @{ results = $results.ToArray() } |
    Set-Content -Path $ResultPath -Encoding UTF8
}
`

// BuildScript returns a PowerShell script applying ops, for
// winexec.Elevator.RunScript. In mirrored networking mode WSL shares the
// host's IPs, so only Hyper-V firewall rules are needed, not portproxy.
func BuildScript(ops []Op, mirrored bool) (string, error) {
	var b strings.Builder
	b.WriteString(scriptHeader)
	for _, op := range ops {
		if op.Port < 1 || op.Port > 65535 {
			return "", fmt.Errorf("invalid port %d", op.Port)
		}
		var body string
		switch {
		case op.Kind == OpAdd && mirrored:
			body = addHyperV(op.Port)
		case op.Kind == OpAdd:
			if !op.ConnectAddress.Is4() {
				return "", fmt.Errorf("port %d: need an IPv4 WSL address, got %v", op.Port, op.ConnectAddress)
			}
			body = addNAT(op.Port, op.ConnectAddress)
		case op.Kind == OpRemove:
			body = remove(op.Port)
		default:
			return "", fmt.Errorf("unknown op %q", op.Kind)
		}
		fmt.Fprintf(&b, "  try {\n%s    Add-Result '%s' %d $true ''\n  } catch { Add-Result '%s' %d $false $_.Exception.Message }\n",
			body, op.Kind, op.Port, op.Kind, op.Port)
	}
	b.WriteString(scriptFooter)
	return b.String(), nil
}

func addNAT(port int, ip netip.Addr) string {
	return fmt.Sprintf(`    Remove-Proxy %[1]d
    Invoke-Netsh @('interface','portproxy','add','v4tov4','listenaddress=0.0.0.0','listenport=%[1]d','connectaddress=%[2]s','connectport=%[1]d')
    Get-NetFirewallRule -Name '%[3]s%[1]d' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    New-NetFirewallRule -Name '%[3]s%[1]d' -DisplayName 'WSL2 Serve-a-Port TCP %[1]d' -Group '%[4]s' -Direction Inbound -Protocol TCP -LocalPort %[1]d -Action Allow -Profile Any | Out-Null
`, port, ip, firewallRulePrefix, FirewallGroup)
}

func addHyperV(port int) string {
	return fmt.Sprintf(`    Get-NetFirewallHyperVRule -Name '%[2]s%[1]d' -ErrorAction SilentlyContinue | Remove-NetFirewallHyperVRule
    New-NetFirewallHyperVRule -Name '%[2]s%[1]d' -DisplayName 'WSL2 Serve-a-Port TCP %[1]d' -Direction Inbound -VMCreatorId '%[3]s' -Protocol TCP -LocalPorts %[1]d -Action Allow | Out-Null
`, port, hyperVRulePrefix, wslVMCreatorID)
}

// remove clears both NAT and mirrored-mode rules so switching modes never
// leaves strays behind.
func remove(port int) string {
	return fmt.Sprintf(`    Remove-Proxy %[1]d
    Get-NetFirewallRule -Name '%[2]s%[1]d' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    if (Get-Command Get-NetFirewallHyperVRule -ErrorAction SilentlyContinue) {
      Get-NetFirewallHyperVRule -Name '%[3]s%[1]d' -ErrorAction SilentlyContinue | Remove-NetFirewallHyperVRule
    }
`, port, firewallRulePrefix, hyperVRulePrefix)
}

// ParseResults decodes the JSON the script wrote. PowerShell 5.1 collapses
// one-element arrays into a bare object, so both shapes are accepted.
func ParseResults(b []byte) ([]Result, error) {
	var wrapper struct {
		Results json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing script results: %w", err)
	}
	raw := strings.TrimSpace(string(wrapper.Results))
	switch {
	case raw == "" || raw == "null":
		return nil, nil
	case strings.HasPrefix(raw, "{"):
		var r Result
		err := json.Unmarshal(wrapper.Results, &r)
		return []Result{r}, err
	default:
		var rs []Result
		err := json.Unmarshal(wrapper.Results, &rs)
		return rs, err
	}
}
