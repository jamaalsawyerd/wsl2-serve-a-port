package forward

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/netinfo"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/portproxy"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/ports"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/state"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/winexec"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/wsl"
)

// Backend is everything the Manager needs from the OS. System() returns the
// real one; tests use a fake.
type Backend interface {
	WSLIP() (netinfo.WSLAddr, error)
	WindowsIPs(ctx context.Context) ([]netinfo.WindowsAddr, error)
	Listeners(wslIP netip.Addr) ([]netinfo.Listener, error)
	Rules(ctx context.Context) ([]portproxy.Rule, error)
	FirewallPorts(ctx context.Context, mirrored bool) ([]int, error)
	RunElevated(ctx context.Context, script string) ([]byte, error)
}

type systemBackend struct {
	runner   winexec.Runner
	ps       winexec.PowerShell
	netsh    string
	elevator *winexec.Elevator
}

// System returns a Backend that talks to the real WSL and Windows.
func System(env wsl.Report) Backend {
	r := winexec.ExecRunner{}
	ps := winexec.PowerShell{Exe: env.PowerShell, Runner: r}
	return &systemBackend{runner: r, ps: ps, netsh: env.Netsh, elevator: winexec.NewElevator(ps)}
}

func (b *systemBackend) WSLIP() (netinfo.WSLAddr, error) { return netinfo.WSLIP() }
func (b *systemBackend) WindowsIPs(ctx context.Context) ([]netinfo.WindowsAddr, error) {
	return netinfo.WindowsIPs(ctx, b.ps)
}
func (b *systemBackend) Listeners(ip netip.Addr) ([]netinfo.Listener, error) {
	return netinfo.Listeners(ip)
}
func (b *systemBackend) Rules(ctx context.Context) ([]portproxy.Rule, error) {
	return portproxy.List(ctx, b.runner, b.netsh)
}
func (b *systemBackend) FirewallPorts(ctx context.Context, mirrored bool) ([]int, error) {
	return portproxy.FirewallPorts(ctx, b.ps, mirrored)
}
func (b *systemBackend) RunElevated(ctx context.Context, script string) ([]byte, error) {
	return b.elevator.RunScript(ctx, script)
}

// Snapshot is everything the dashboard shows.
type Snapshot struct {
	WSL       netinfo.WSLAddr
	Windows   []netinfo.WindowsAddr
	Entries   []Entry
	Listeners []netinfo.Listener
	Mirrored  bool
	// Warnings are non-fatal problems, e.g. Windows IPs couldn't be read.
	Warnings []string
	TakenAt  time.Time
}

// Entry returns the entry for port.
func (s Snapshot) Entry(port int) (Entry, bool) {
	i := slices.IndexFunc(s.Entries, func(e Entry) bool { return e.Port == port })
	if i < 0 {
		return Entry{}, false
	}
	return s.Entries[i], true
}

// Manager reads and changes forwards.
type Manager struct {
	Backend  Backend
	Store    state.Store
	Mirrored bool
	Now      func() time.Time
}

// NewManager returns a Manager for the detected environment.
func NewManager(env wsl.Report, store state.Store) *Manager {
	return &Manager{
		Backend:  System(env),
		Store:    store,
		Mirrored: env.NetworkingMode == wsl.ModeMirrored,
		Now:      time.Now,
	}
}

// Snapshot gathers the current state. The slow Windows calls (each spawns
// PowerShell or netsh) run in parallel.
func (m *Manager) Snapshot(ctx context.Context) (Snapshot, error) {
	snap := Snapshot{Mirrored: m.Mirrored, TakenAt: m.Now()}

	wslAddr, err := m.Backend.WSLIP()
	if err != nil {
		return snap, fmt.Errorf("finding the WSL IP: %w", err)
	}
	snap.WSL = wslAddr

	st, err := m.Store.Load()
	if err != nil {
		return snap, err
	}

	var (
		wg                     sync.WaitGroup
		rules                  []portproxy.Rule
		firewall               []int
		rulesErr, fwErr, ipErr error
	)
	wg.Go(func() { snap.Windows, ipErr = m.Backend.WindowsIPs(ctx) })
	wg.Go(func() { firewall, fwErr = m.Backend.FirewallPorts(ctx, m.Mirrored) })
	if !m.Mirrored {
		wg.Go(func() { rules, rulesErr = m.Backend.Rules(ctx) })
	}
	listeners, lErr := m.Backend.Listeners(wslAddr.IP)
	wg.Wait()

	if rulesErr != nil {
		return snap, rulesErr
	}
	if ipErr != nil {
		snap.Warnings = append(snap.Warnings, ipErr.Error())
	}
	if fwErr != nil {
		snap.Warnings = append(snap.Warnings, fwErr.Error())
	}
	if lErr != nil {
		snap.Warnings = append(snap.Warnings, "reading listening ports: "+lErr.Error())
	}

	snap.Listeners = listeners
	snap.Entries = buildEntries(st, rules, firewall, listeners, wslAddr.IP, m.Mirrored)
	return snap, nil
}

// ConflictError means some ports already have portproxy rules this app
// didn't create. Retry Add with takeOver to replace them.
type ConflictError struct {
	Ports []int
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("port %s already forwarded by something else", ports.Format(e.Ports))
}

// Add forwards ports to the WSL IP in snap.
func (m *Manager) Add(ctx context.Context, snap Snapshot, ps []int, takeOver bool) ([]portproxy.Result, error) {
	if !takeOver {
		var conflicts []int
		for _, p := range ps {
			if e, ok := snap.Entry(p); ok && !e.Managed {
				conflicts = append(conflicts, p)
			}
		}
		if len(conflicts) > 0 {
			return nil, &ConflictError{Ports: conflicts}
		}
	}
	return m.apply(ctx, m.addOps(ps, snap.WSL.IP))
}

// Remove deletes the forwards for ports, including ones this app didn't
// create.
func (m *Manager) Remove(ctx context.Context, ps []int) ([]portproxy.Result, error) {
	ops := make([]portproxy.Op, len(ps))
	for i, p := range ps {
		ops[i] = portproxy.Op{Kind: portproxy.OpRemove, Port: p}
	}
	return m.apply(ctx, ops)
}

// RepairPorts returns the managed ports that are stale or missing.
func RepairPorts(snap Snapshot) []int {
	var out []int
	for _, e := range snap.Entries {
		if e.NeedsRepair() {
			out = append(out, e.Port)
		}
	}
	return out
}

// Repair re-applies every stale or missing managed forward. It returns no
// results and no error when there's nothing to fix.
func (m *Manager) Repair(ctx context.Context, snap Snapshot) ([]portproxy.Result, error) {
	ps := RepairPorts(snap)
	if len(ps) == 0 {
		return nil, nil
	}
	return m.apply(ctx, m.addOps(ps, snap.WSL.IP))
}

func (m *Manager) addOps(ps []int, ip netip.Addr) []portproxy.Op {
	ops := make([]portproxy.Op, len(ps))
	for i, p := range ps {
		ops[i] = portproxy.Op{Kind: portproxy.OpAdd, Port: p, ConnectAddress: ip}
	}
	return ops
}

// apply runs ops in one elevated script and records the successes.
func (m *Manager) apply(ctx context.Context, ops []portproxy.Op) ([]portproxy.Result, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	script, err := portproxy.BuildScript(ops, m.Mirrored)
	if err != nil {
		return nil, err
	}
	out, err := m.Backend.RunElevated(ctx, script)
	if err != nil {
		return nil, err
	}
	results, err := portproxy.ParseResults(out)
	if err != nil {
		return nil, err
	}
	results = fillMissing(ops, results)

	st, err := m.Store.Load()
	if err != nil {
		return results, err
	}
	now := m.Now()
	for i, r := range results {
		if !r.OK {
			continue
		}
		switch r.Op {
		case portproxy.OpAdd:
			st.Upsert(state.Forward{
				Port:           r.Port,
				ConnectAddress: ops[i].ConnectAddress.String(),
				Mirrored:       m.Mirrored,
				CreatedAt:      now,
			})
		case portproxy.OpRemove:
			st.Remove(r.Port)
		}
	}
	return results, m.Store.Save(st)
}

// fillMissing returns one result per op, in op order, marking any op the
// script didn't report on (e.g. it was killed partway) as failed.
func fillMissing(ops []portproxy.Op, results []portproxy.Result) []portproxy.Result {
	type key struct {
		kind portproxy.OpKind
		port int
	}
	got := make(map[key]portproxy.Result, len(results))
	for _, r := range results {
		got[key{r.Op, r.Port}] = r
	}
	out := make([]portproxy.Result, len(ops))
	for i, op := range ops {
		r, ok := got[key{op.Kind, op.Port}]
		if !ok {
			r = portproxy.Result{Op: op.Kind, Port: op.Port, Message: "no result reported"}
		}
		out[i] = r
	}
	return out
}
