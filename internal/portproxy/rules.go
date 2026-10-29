// Package portproxy reads Windows portproxy and firewall rules and builds the
// elevated PowerShell scripts that change them.
package portproxy

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/winexec"
)

// Rule is one netsh interface portproxy v4tov4 entry.
type Rule struct {
	ListenAddress  string
	ListenPort     int
	ConnectAddress string
	ConnectPort    int
}

// List returns the current v4tov4 portproxy rules. Reading them doesn't need
// admin rights.
func List(ctx context.Context, r winexec.Runner, netsh string) ([]Rule, error) {
	out, err := r.Run(ctx, netsh, "interface", "portproxy", "show", "v4tov4")
	if err != nil {
		return nil, fmt.Errorf("listing portproxy rules: %w", err)
	}
	return ParseRules(winexec.Clean(out)), nil
}

// ParseRules parses `netsh interface portproxy show v4tov4` output. The
// headers are localized, so it keys off the data rows' shape instead.
func ParseRules(out string) []Rule {
	var rules []Rule
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) != 4 {
			continue
		}
		lp, err1 := strconv.Atoi(f[1])
		cp, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil {
			continue
		}
		rules = append(rules, Rule{ListenAddress: f[0], ListenPort: lp, ConnectAddress: f[2], ConnectPort: cp})
	}
	return rules
}

// FirewallPorts returns the ports that have one of our inbound firewall
// rules, found by rule name.
func FirewallPorts(ctx context.Context, ps winexec.PowerShell) ([]int, error) {
	out, err := ps.Command(ctx, fmt.Sprintf(
		"Get-NetFirewallRule -Group %s -ErrorAction SilentlyContinue | ForEach-Object { $_.Name }",
		winexec.Quote(FirewallGroup)))
	if err != nil {
		return nil, fmt.Errorf("listing firewall rules: %w", err)
	}
	var ports []int
	for line := range strings.Lines(out) {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), firewallRulePrefix); ok {
			if n, err := strconv.Atoi(p); err == nil {
				ports = append(ports, n)
			}
		}
	}
	return ports, nil
}
