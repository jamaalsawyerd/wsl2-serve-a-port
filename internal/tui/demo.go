package tui

import (
	"cmp"
	"context"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/netinfo"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/portproxy"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/wsl"
)

// DemoDeps runs the UI against made-up data, touching nothing on the
// system. It powers `serve-a-port demo` and the README screenshots. delay
// simulates the time Windows takes to answer.
func DemoDeps(delay time.Duration) Deps {
	svc := newDemoService(delay)
	return Deps{
		Detect: func(context.Context) wsl.Report {
			time.Sleep(delay / 2)
			return demoReport
		},
		Connect: func(wsl.Report) (Service, error) { return svc, nil },
	}
}

var demoReport = wsl.Report{
	Distro:         "Ubuntu-24.04",
	Kernel:         "6.6.87.2-microsoft-standard-WSL2",
	NetworkingMode: wsl.ModeNAT,
	Checks: []wsl.Check{
		{Name: "WSL2 kernel", OK: true, Fatal: true, Detail: "6.6.87.2-microsoft-standard-WSL2"},
		{Name: "Windows interop", OK: true, Fatal: true, Detail: "enabled"},
		{Name: "PowerShell", OK: true, Fatal: true, Detail: "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"},
		{Name: "netsh", OK: true, Fatal: true, Detail: "/mnt/c/Windows/system32/netsh.exe"},
		{Name: "Networking mode", OK: true, Detail: "nat"},
	},
}

// demoService fakes forward.Manager over in-memory state.
type demoService struct {
	delay time.Duration

	mu        sync.Mutex
	wsl       netip.Addr
	windows   []netinfo.WindowsAddr
	listeners []netinfo.Listener
	// forwards maps port to the WSL IP its rule points at.
	forwards map[int]string
	external map[int]bool
}

func newDemoService(delay time.Duration) *demoService {
	return &demoService{
		delay: delay,
		wsl:   netip.MustParseAddr("172.28.112.5"),
		windows: []netinfo.WindowsAddr{
			{Alias: "Wi-Fi", Description: "Intel Wi-Fi 6E AX211", IP: netip.MustParseAddr("192.168.1.42"), Metric: 35, Hardware: true},
			{Alias: "Tailscale", Description: "Tailscale Tunnel", IP: netip.MustParseAddr("100.101.102.103"), Metric: 5},
		},
		listeners: []netinfo.Listener{
			{Port: 3000, Reachable: true, Process: "node", Addrs: []netip.Addr{netip.IPv6Unspecified()}},
			{Port: 5173, Reachable: true, Process: "vite", Addrs: []netip.Addr{netip.IPv4Unspecified()}},
			{Port: 5432, Process: "postgres", Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
			{Port: 6006, Reachable: true, Process: "storybook", Addrs: []netip.Addr{netip.IPv4Unspecified()}},
			{Port: 8080, Process: "python3", Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		},
		forwards: map[int]string{
			3000: "172.28.112.5",
			5173: "172.28.112.5",
			8000: "172.28.112.5",
			8080: "172.28.112.5",
			4321: "172.28.96.17", // from before WSL last restarted
		},
		external: map[int]bool{},
	}
}

func (d *demoService) snapshot() forward.Snapshot {
	snap := forward.Snapshot{
		WSL:       netinfo.WSLAddr{Interface: "eth0", IP: d.wsl},
		Windows:   slices.Clone(d.windows),
		Listeners: slices.Clone(d.listeners),
		TakenAt:   time.Now(),
	}
	for port, target := range d.forwards {
		e := forward.Entry{
			Port:     port,
			Managed:  !d.external[port],
			Firewall: true,
			Rule:     &portproxy.Rule{ListenAddress: "0.0.0.0", ListenPort: port, ConnectAddress: target, ConnectPort: port},
		}
		if i := slices.IndexFunc(snap.Listeners, func(l netinfo.Listener) bool { return l.Port == port }); i >= 0 {
			e.Listener = &snap.Listeners[i]
		}
		switch {
		case !e.Managed:
			e.Status = forward.StatusExternal
		case target != d.wsl.String():
			e.Status = forward.StatusStale
		case e.Listener == nil:
			e.Status = forward.StatusIdle
		case !e.Listener.Reachable:
			e.Status = forward.StatusLocalOnly
		default:
			e.Status = forward.StatusLive
		}
		snap.Entries = append(snap.Entries, e)
	}
	slices.SortFunc(snap.Entries, func(a, b forward.Entry) int { return cmp.Compare(a.Port, b.Port) })
	return snap
}

func (d *demoService) Snapshot(context.Context) (forward.Snapshot, error) {
	time.Sleep(d.delay)
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.snapshot(), nil
}

func (d *demoService) Relisten(forward.Snapshot) (forward.Snapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.snapshot(), nil
}

func (d *demoService) change(op portproxy.OpKind, ps []int, apply func(int)) []portproxy.Result {
	time.Sleep(d.delay)
	d.mu.Lock()
	defer d.mu.Unlock()
	results := make([]portproxy.Result, len(ps))
	for i, p := range ps {
		apply(p)
		results[i] = portproxy.Result{Op: op, Port: p, OK: true}
	}
	return results
}

func (d *demoService) Add(_ context.Context, _ forward.Snapshot, ps []int, _ bool) ([]portproxy.Result, error) {
	return d.change(portproxy.OpAdd, ps, func(p int) {
		d.forwards[p] = d.wsl.String()
		delete(d.external, p)
	}), nil
}

func (d *demoService) Remove(_ context.Context, ps []int) ([]portproxy.Result, error) {
	return d.change(portproxy.OpRemove, ps, func(p int) {
		delete(d.forwards, p)
		delete(d.external, p)
	}), nil
}

func (d *demoService) Repair(_ context.Context, snap forward.Snapshot) ([]portproxy.Result, error) {
	return d.change(portproxy.OpAdd, forward.RepairPorts(snap), func(p int) {
		d.forwards[p] = d.wsl.String()
	}), nil
}
