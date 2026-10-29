// Package forward ties the system pieces together: it builds a snapshot of
// what's forwarded and applies add/remove/repair changes.
package forward

import (
	"cmp"
	"net/netip"
	"slices"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/netinfo"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/portproxy"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/state"
)

// Status summarizes one forwarded port.
type Status string

const (
	// StatusLive: forwarded, and something reachable is listening in WSL.
	StatusLive Status = "live"
	// StatusIdle: forwarded, but nothing is listening yet.
	StatusIdle Status = "idle"
	// StatusLocalOnly: forwarded, but the server is bound to 127.0.0.1 so
	// the forward can't reach it.
	StatusLocalOnly Status = "local-only"
	// StatusStale: the portproxy rule points at an old WSL IP.
	StatusStale Status = "stale"
	// StatusMissing: we created it, but the Windows rules are gone.
	StatusMissing Status = "missing"
	// StatusExternal: a portproxy rule this app didn't create.
	StatusExternal Status = "external"
)

// Entry is one row of the dashboard.
type Entry struct {
	Port    int
	Managed bool
	// Rule is the Windows portproxy rule, if any (always nil in mirrored mode).
	Rule *portproxy.Rule
	// Firewall is true when our inbound firewall rule exists.
	Firewall bool
	Listener *netinfo.Listener
	Status   Status
}

// NeedsRepair reports whether Repair would fix this entry.
func (e Entry) NeedsRepair() bool {
	return e.Managed && (e.Status == StatusStale || e.Status == StatusMissing)
}

// buildEntries reconciles saved state with what Windows and WSL report.
func buildEntries(st *state.State, rules []portproxy.Rule, firewall []int,
	listeners []netinfo.Listener, wslIP netip.Addr, mirrored bool) []Entry {

	ruleByPort := make(map[int]*portproxy.Rule)
	for i := range rules {
		ruleByPort[rules[i].ListenPort] = &rules[i]
	}
	listenerByPort := make(map[int]*netinfo.Listener)
	for i := range listeners {
		listenerByPort[listeners[i].Port] = &listeners[i]
	}

	ports := st.Ports()
	if !mirrored {
		for p := range ruleByPort {
			if !slices.Contains(ports, p) {
				ports = append(ports, p)
			}
		}
	}
	slices.SortFunc(ports, cmp.Compare)

	entries := make([]Entry, 0, len(ports))
	for _, p := range ports {
		_, managed := st.Get(p)
		e := Entry{
			Port:     p,
			Managed:  managed,
			Rule:     ruleByPort[p],
			Firewall: slices.Contains(firewall, p),
			Listener: listenerByPort[p],
		}
		if mirrored {
			e.Rule = nil
		}
		e.Status = entryStatus(e, wslIP, mirrored)
		entries = append(entries, e)
	}
	return entries
}

func entryStatus(e Entry, wslIP netip.Addr, mirrored bool) Status {
	switch {
	case !e.Managed:
		return StatusExternal
	case !e.Firewall || (!mirrored && e.Rule == nil):
		return StatusMissing
	case !mirrored && e.Rule.ConnectAddress != wslIP.String():
		return StatusStale
	case e.Listener == nil:
		return StatusIdle
	case !e.Listener.Reachable:
		return StatusLocalOnly
	default:
		return StatusLive
	}
}
