package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/winexec"
)

var cmdSliceType = reflect.TypeFor[[]tea.Cmd]()

// send delivers each message and then settles the model: commands that
// return promptly have their messages fed back in, the way the runtime
// would. Timer-driven commands (ticks, cursor blinks) are dropped.
func send(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, cmd := m.Update(msg)
		m = settle(t, next.(Model), cmd)
	}
	return m
}

func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 1000 {
			t.Fatal("model never settled")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg, ok := runQuick(c)
		if !ok || msg == nil {
			continue
		}
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().ConvertibleTo(cmdSliceType) {
			queue = append(queue, v.Convert(cmdSliceType).Interface().([]tea.Cmd)...)
			continue
		}
		switch msg.(type) {
		case frameMsg, spinner.TickMsg, toastExpiredMsg, tea.QuitMsg,
			celebrateMsg, relistenTickMsg, fullRefreshMsg:
			continue
		}
		next, c2 := m.Update(msg)
		m = next.(Model)
		queue = append(queue, c2)
	}
	return m
}

func runQuick(c tea.Cmd) (tea.Msg, bool) {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- c() }()
	select {
	case msg := <-ch:
		return msg, true
	case <-time.After(30 * time.Millisecond):
		return nil, false
	}
}

func typed(s string) []tea.Msg {
	var out []tea.Msg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

// noCandidates is a snapshot where every listener is already forwarded, so
// the add form goes straight to typing ports.
func noCandidates() forward.Snapshot {
	s := sampleSnapshot()
	s.Listeners = s.Listeners[:1]
	return s
}

func dashboard(t *testing.T, svc *fakeService) Model {
	t.Helper()
	m := boot(t, newModel(goodReport, svc, 110, 40))
	if m.screen != screenDashboard {
		t.Fatal("didn't reach dashboard")
	}
	return m
}

func TestAddTypedRange(t *testing.T) {
	svc := &fakeService{snap: noCandidates()}
	m := send(t, dashboard(t, svc), press("a"))
	if m.screen != screenAdd || !strings.Contains(plain(m), "Which ports?") {
		t.Fatalf("expected the port input:\n%s", plain(m))
	}

	m = send(t, m, append(typed("8000-8002"), press("enter"))...)
	if m.screen != screenConfirm {
		t.Fatalf("screen = %v, want confirm:\n%s", m.screen, plain(m))
	}
	out := plain(m)
	for _, want := range []string{"Open 3 ports to your network?", "0.0.0.0:8000-8002 → 172.17.213.47", "allow inbound TCP 8000-8002", "(nothing listening yet)"} {
		if !strings.Contains(out, want) {
			t.Errorf("confirm screen missing %q:\n%s", want, out)
		}
	}

	before := svc.calls
	m = send(t, m, press("enter"))
	if m.screen != screenResult {
		t.Fatalf("screen = %v, want result", m.screen)
	}
	if svc.calls != before+1 {
		t.Errorf("expected one refresh after applying, got %d", svc.calls-before)
	}
	if !slices.Equal(svc.added, []int{8000, 8001, 8002}) || svc.takeOver {
		t.Errorf("Add(%v, takeOver=%v)", svc.added, svc.takeOver)
	}
	if out := plain(m); !strings.Contains(out, "✦ 3 ports open to the network!") || !strings.Contains(out, "http://192.168.1.50:8001") {
		t.Errorf("result screen:\n%s", out)
	}
	m = send(t, m, press("enter"))
	if m.screen != screenDashboard {
		t.Errorf("expected dashboard after result (screen %v)", m.screen)
	}
}

func TestAddValidation(t *testing.T) {
	m := send(t, dashboard(t, &fakeService{snap: noCandidates()}), press("a"))
	m = send(t, m, append(typed("70000"), press("enter"))...)
	if m.screen != screenAdd || !strings.Contains(plain(m), "out of range") {
		t.Errorf("invalid port should keep the form open with an error:\n%s", plain(m))
	}
	m = send(t, m, press("esc"))
	if m.screen != screenDashboard {
		t.Error("esc didn't cancel the form")
	}
}

func TestAddPickFromListeners(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := send(t, dashboard(t, svc), press("a"))
	if out := plain(m); !strings.Contains(out, "Pick from running servers") || !strings.Contains(out, "Found 1 server") || strings.Contains(out, "filter") {
		t.Fatalf("expected mode select:\n%s", out)
	}
	m = send(t, m, press("enter"))
	if out := plain(m); !strings.Contains(out, "9229") || !strings.Contains(out, "vite") {
		t.Fatalf("expected vite in multi-select:\n%s", out)
	}
	m = send(t, m, press("enter"))
	if m.screen != screenAdd || !strings.Contains(plain(m), "pick at least one") {
		t.Fatalf("empty pick should be rejected:\n%s", plain(m))
	}
	m = send(t, m, press("space"), press("enter"))
	if m.screen != screenConfirm || !slices.Equal(m.pending.ports, []int{9229}) {
		t.Fatalf("pending = %+v on screen %v", m.pending, m.screen)
	}
	if !strings.Contains(plain(m), "● vite") {
		t.Errorf("confirm row should show the process:\n%s", plain(m))
	}
}

func TestAddTakeOverExternal(t *testing.T) {
	svc := &fakeService{snap: noCandidates()}
	m := send(t, dashboard(t, svc), press("a"))
	m = send(t, m, append(typed("8081"), press("enter"))...)
	if !strings.Contains(plain(m), "replaces another tool's rule") {
		t.Errorf("no take-over warning:\n%s", plain(m))
	}
	send(t, m, press("enter"))
	if !svc.takeOver {
		t.Error("Add wasn't told to take over the external rule")
	}
}

func TestRemoveSelected(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := send(t, dashboard(t, svc), press("down"), press("down"), press("down"), press("d"))
	if m.screen != screenConfirm || !strings.Contains(plain(m), "Stop forwarding 1 port?") {
		t.Fatalf("expected remove confirm:\n%s", plain(m))
	}
	m = send(t, m, press("enter"))
	if !slices.Equal(svc.removed, []int{5173}) || !strings.Contains(plain(m), "✓ 1 port closed") {
		t.Errorf("removed %v:\n%s", svc.removed, plain(m))
	}
}

func TestRemoveAllSkipsExternal(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := send(t, dashboard(t, svc), press("D"))
	if !slices.Equal(m.pending.ports, []int{3000, 4000, 5000, 5173, 6000}) {
		t.Errorf("remove-all ports = %v", m.pending.ports)
	}
	m = send(t, m, press("esc"))
	if m.screen != screenDashboard || svc.removed != nil {
		t.Error("esc should cancel without removing")
	}
}

func TestRepair(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := send(t, dashboard(t, svc), press("r"))
	out := plain(m)
	if !strings.Contains(out, "Repair 2 forwards?") || !strings.Contains(out, "172.17.200.9 → 172.17.213.47") || !strings.Contains(out, "recreate missing rules") {
		t.Fatalf("repair confirm:\n%s", out)
	}
	m = send(t, m, press("y"))
	if !slices.Equal(svc.repaired, []int{5173, 6000}) || !strings.Contains(plain(m), "✓ 2 forwards repaired") {
		t.Errorf("repaired %v:\n%s", svc.repaired, plain(m))
	}
}

func TestRepairHealthy(t *testing.T) {
	snap := sampleSnapshot()
	snap.Entries = snap.Entries[:1]
	m := send(t, dashboard(t, &fakeService{snap: snap}), press("r"))
	if m.screen != screenDashboard || !strings.Contains(plain(m), "Everything's healthy") {
		t.Errorf("expected healthy toast:\n%s", plain(m))
	}
}

func TestApplyDeclined(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot(), applyErr: winexec.ErrElevationDeclined}
	m := send(t, dashboard(t, svc), press("d"), press("enter"))
	if out := plain(m); !strings.Contains(out, "Admin permission declined") || !strings.Contains(out, "Nothing was changed") {
		t.Errorf("declined result:\n%s", out)
	}
}

func TestApplyPartialFailure(t *testing.T) {
	svc := &fakeService{snap: noCandidates(), fail: map[int]string{8001: "The process cannot access the file"}}
	m := send(t, dashboard(t, svc), press("a"))
	m = send(t, m, append(typed("8000 8001"), press("enter"), press("enter"))...)
	out := plain(m)
	if !strings.Contains(out, "⚠ 1 of 2 worked") || !strings.Contains(out, "cannot access the file") {
		t.Errorf("partial result:\n%s", out)
	}
}

func TestApplyingScreen(t *testing.T) {
	m := dashboard(t, &fakeService{snap: sampleSnapshot()})
	m = step(m, press("d"))
	m = step(m, press("enter")) // don't run the apply command
	if m.screen != screenApplying || !strings.Contains(plain(m), "Approve the admin prompt") {
		t.Errorf("applying screen:\n%s", plain(m))
	}
}
