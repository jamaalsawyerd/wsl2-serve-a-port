package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
)

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestQuitWithoutForwards(t *testing.T) {
	snap := sampleSnapshot()
	snap.Entries = []forward.Entry{snap.Entries[5]} // only the external one
	m := dashboard(t, &fakeService{snap: snap})
	if _, cmd := m.Update(press("q")); !isQuit(cmd) {
		t.Error("q with no serve-a-port forwards should quit right away")
	}
}

func TestQuitPromptKeep(t *testing.T) {
	m := send(t, dashboard(t, &fakeService{snap: sampleSnapshot()}), press("q"))
	if m.screen != screenQuit {
		t.Fatalf("screen = %v, want quit prompt", m.screen)
	}
	out := plain(m)
	if !strings.Contains(out, "5 ports will stay open") || !strings.Contains(out, "3000, 4000, 5000, 5173, 6000") {
		t.Errorf("quit prompt:\n%s", out)
	}
	if _, cmd := m.Update(press("enter")); !isQuit(cmd) {
		t.Error("enter should keep forwards and quit")
	}
	if m2 := send(t, m, press("esc")); m2.screen != screenDashboard {
		t.Error("esc should go back to the dashboard")
	}
}

func TestQuitPromptTearDown(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := send(t, dashboard(t, svc), press("q"))
	next, cmd := m.Update(press("t"))
	m = next.(Model)
	if m.screen != screenApplying {
		t.Fatalf("screen = %v, want applying", m.screen)
	}
	_, cmd = m.Update(cmd())
	if !isQuit(cmd) {
		t.Error("successful tear-down should quit")
	}
	if !slices.Equal(svc.removed, []int{3000, 4000, 5000, 5173, 6000}) {
		t.Errorf("removed %v; the external 8081 rule must be left alone", svc.removed)
	}

	// A failed tear-down stays open to explain itself.
	svc = &fakeService{snap: sampleSnapshot(), fail: map[int]string{4000: "Access is denied."}}
	m = send(t, dashboard(t, svc), press("q"), press("t"))
	if m.screen != screenResult {
		t.Fatalf("screen = %v, want result after failed tear-down", m.screen)
	}
	if out := plain(m); !strings.Contains(out, "Access is denied.") || !strings.Contains(out, "↳ Windows refused the change") {
		t.Errorf("failure and hint not shown:\n%s", out)
	}
}
