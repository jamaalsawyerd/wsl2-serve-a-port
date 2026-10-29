package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/netinfo"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/portproxy"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/state"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/winexec"
)

var (
	wslIP = netip.MustParseAddr("172.17.213.47")
	oldIP = "172.17.200.9"
	now   = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
)

func TestBuildEntries(t *testing.T) {
	st := &state.State{}
	for _, p := range []int{3000, 4000, 5000, 5173, 6000, 7000} {
		st.Upsert(state.Forward{Port: p})
	}
	rule := func(port int, ip string) portproxy.Rule {
		return portproxy.Rule{ListenAddress: "0.0.0.0", ListenPort: port, ConnectAddress: ip, ConnectPort: port}
	}
	rules := []portproxy.Rule{
		rule(3000, wslIP.String()),
		rule(4000, wslIP.String()),
		rule(5000, wslIP.String()),
		rule(5173, oldIP),
		rule(7000, wslIP.String()),
		rule(8081, wslIP.String()), // not ours
	}
	firewall := []int{3000, 4000, 5000, 5173, 6000}
	listeners := []netinfo.Listener{
		{Port: 3000, Reachable: true},
		{Port: 5000, Reachable: false},
	}

	got := buildEntries(st, rules, firewall, listeners, wslIP, false)
	want := map[int]Status{
		3000: StatusLive,
		4000: StatusIdle,
		5000: StatusLocalOnly,
		5173: StatusStale,
		6000: StatusMissing, // no portproxy rule
		7000: StatusMissing, // no firewall rule
		8081: StatusExternal,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i, e := range got {
		if i > 0 && got[i-1].Port >= e.Port {
			t.Errorf("entries not sorted: %d after %d", e.Port, got[i-1].Port)
		}
		if want[e.Port] != e.Status {
			t.Errorf("port %d: status %s, want %s", e.Port, e.Status, want[e.Port])
		}
	}

	snap := Snapshot{Entries: got}
	if r := RepairPorts(snap); !slices.Equal(r, []int{5173, 6000, 7000}) {
		t.Errorf("RepairPorts = %v", r)
	}
}

func TestBuildEntriesMirrored(t *testing.T) {
	st := &state.State{}
	st.Upsert(state.Forward{Port: 3000, Mirrored: true})
	st.Upsert(state.Forward{Port: 4000, Mirrored: true})
	got := buildEntries(st, nil, []int{3000}, nil, wslIP, true)
	if len(got) != 2 || got[0].Status != StatusIdle || got[1].Status != StatusMissing {
		t.Errorf("mirrored entries = %+v", got)
	}
}

// fakeBackend records elevated scripts and answers with a canned result per
// op, failing the ports in fail.
type fakeBackend struct {
	rules    []portproxy.Rule
	firewall []int
	fail     map[int]bool
	declined bool
	scripts  []string

	ip        netip.Addr // defaults to wslIP
	listeners []netinfo.Listener
	helperOff bool
}

func (f *fakeBackend) IPHelperRunning(context.Context) (bool, error) { return !f.helperOff, nil }

func (f *fakeBackend) WSLIP() (netinfo.WSLAddr, error) {
	if f.ip.IsValid() {
		return netinfo.WSLAddr{Interface: "eth0", IP: f.ip}, nil
	}
	return netinfo.WSLAddr{Interface: "eth0", IP: wslIP}, nil
}
func (f *fakeBackend) WindowsIPs(context.Context) ([]netinfo.WindowsAddr, error) {
	return nil, errors.New("powershell sad")
}
func (f *fakeBackend) Listeners(netip.Addr) ([]netinfo.Listener, error) { return f.listeners, nil }
func (f *fakeBackend) Rules(context.Context) ([]portproxy.Rule, error)  { return f.rules, nil }
func (f *fakeBackend) FirewallPorts(context.Context, bool) ([]int, error) {
	return f.firewall, nil
}

var resultLine = regexp.MustCompile(`Add-Result '(\w+)' (\d+) \$true`)

func (f *fakeBackend) RunElevated(_ context.Context, script string) ([]byte, error) {
	f.scripts = append(f.scripts, script)
	if f.declined {
		return nil, winexec.ErrElevationDeclined
	}
	var results []portproxy.Result
	for _, m := range resultLine.FindAllStringSubmatch(script, -1) {
		port, _ := strconv.Atoi(m[2])
		r := portproxy.Result{Op: portproxy.OpKind(m[1]), Port: port, OK: !f.fail[port]}
		if !r.OK {
			r.Message = "nope"
		}
		results = append(results, r)
	}
	return json.Marshal(map[string]any{"results": results})
}

func newTestManager(t *testing.T, b *fakeBackend) *Manager {
	return &Manager{
		Backend: b,
		Store:   state.Store{Path: filepath.Join(t.TempDir(), "state.json")},
		Now:     func() time.Time { return now },
	}
}

func TestAddRemoveRepair(t *testing.T) {
	ctx := context.Background()
	b := &fakeBackend{
		rules: []portproxy.Rule{{ListenAddress: "0.0.0.0", ListenPort: 8081, ConnectAddress: wslIP.String(), ConnectPort: 8081}},
		fail:  map[int]bool{3001: true},
	}
	m := newTestManager(t, b)

	snap, err := m.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Warnings) != 1 || !strings.Contains(snap.Warnings[0], "powershell sad") {
		t.Errorf("warnings = %v", snap.Warnings)
	}

	// 8081 belongs to someone else.
	_, err = m.Add(ctx, snap, []int{3000, 8081}, false)
	if ce, ok := errors.AsType[*ConflictError](err); !ok || !slices.Equal(ce.Ports, []int{8081}) {
		t.Fatalf("Add over external rule: err = %v, want ConflictError{8081}", err)
	}
	if len(b.scripts) != 0 {
		t.Fatal("conflict should be caught before elevating")
	}

	results, err := m.Add(ctx, snap, []int{3000, 3001}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !results[0].OK || results[1].OK {
		t.Errorf("results = %+v", results)
	}
	st, _ := m.Store.Load()
	if !slices.Equal(st.Ports(), []int{3000}) {
		t.Errorf("state after add = %v, want only the successful port", st.Ports())
	}
	if f, _ := st.Get(3000); f.ConnectAddress != wslIP.String() || !f.CreatedAt.Equal(now) {
		t.Errorf("forward = %+v", f)
	}

	// Pretend Windows now has a stale rule for 3000 and no firewall rule.
	b.rules = append(b.rules, portproxy.Rule{ListenAddress: "0.0.0.0", ListenPort: 3000, ConnectAddress: oldIP, ConnectPort: 3000})
	snap, _ = m.Snapshot(ctx)
	if e, _ := snap.Entry(3000); e.Status != StatusMissing {
		t.Errorf("3000 status = %s, want missing (no firewall)", e.Status)
	}
	b.scripts = nil
	if _, err := m.Repair(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if len(b.scripts) != 1 || !strings.Contains(b.scripts[0], "connectaddress="+wslIP.String()) {
		t.Errorf("repair script didn't re-point 3000 at the current IP")
	}

	if _, err := m.Remove(ctx, []int{3000}); err != nil {
		t.Fatal(err)
	}
	st, _ = m.Store.Load()
	if len(st.Ports()) != 0 {
		t.Errorf("state after remove = %v", st.Ports())
	}
}

func TestIPHelperStopped(t *testing.T) {
	m := newTestManager(t, &fakeBackend{helperOff: true})
	snap, err := m.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snap.IPHelperStopped || !strings.Contains(strings.Join(snap.Warnings, "\n"), "IP Helper") {
		t.Errorf("expected IP Helper warning: %+v", snap.Warnings)
	}

	m.Mirrored = true
	if snap, _ = m.Snapshot(context.Background()); snap.IPHelperStopped {
		t.Error("mirrored mode doesn't use portproxy, so IP Helper doesn't matter")
	}
}

func TestDeclinedLeavesStateAlone(t *testing.T) {
	b := &fakeBackend{declined: true}
	m := newTestManager(t, b)
	snap, _ := m.Snapshot(context.Background())
	if _, err := m.Add(context.Background(), snap, []int{3000}, false); !errors.Is(err, winexec.ErrElevationDeclined) {
		t.Fatalf("err = %v, want ErrElevationDeclined", err)
	}
	st, _ := m.Store.Load()
	if len(st.Ports()) != 0 {
		t.Errorf("state changed after declined prompt: %v", st.Ports())
	}
}

func TestFillMissing(t *testing.T) {
	ops := []portproxy.Op{{Kind: portproxy.OpAdd, Port: 1}, {Kind: portproxy.OpAdd, Port: 2}}
	got := fillMissing(ops, []portproxy.Result{{Op: portproxy.OpAdd, Port: 2, OK: true}})
	if got[0].OK || got[0].Message == "" || !got[1].OK {
		t.Errorf("fillMissing = %+v", got)
	}
}

func TestRelisten(t *testing.T) {
	ctx := context.Background()
	b := &fakeBackend{firewall: []int{3000}}
	m := newTestManager(t, b)
	if _, err := m.Add(ctx, Snapshot{WSL: netinfo.WSLAddr{IP: wslIP}}, []int{3000}, false); err != nil {
		t.Fatal(err)
	}
	b.rules = []portproxy.Rule{{ListenAddress: "0.0.0.0", ListenPort: 3000, ConnectAddress: wslIP.String(), ConnectPort: 3000}}
	snap, err := m.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := snap.Entry(3000); e.Status != StatusIdle {
		t.Fatalf("before: %s, want idle", e.Status)
	}

	// A server starts.
	b.listeners = []netinfo.Listener{{Port: 3000, Reachable: true, Process: "node"}}
	b.scripts = nil
	live, err := m.Relisten(snap)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := live.Entry(3000); e.Status != StatusLive || e.Listener.Process != "node" {
		t.Errorf("after start: %+v", e)
	}
	if e, _ := snap.Entry(3000); e.Status != StatusIdle {
		t.Error("Relisten mutated the input snapshot")
	}

	// WSL restarts with a new IP.
	b.ip = netip.MustParseAddr("172.17.99.1")
	moved, _ := m.Relisten(live)
	if e, _ := moved.Entry(3000); e.Status != StatusStale || moved.WSL.IP != b.ip {
		t.Errorf("after IP change: %s, WSL %v", e.Status, moved.WSL.IP)
	}
}
