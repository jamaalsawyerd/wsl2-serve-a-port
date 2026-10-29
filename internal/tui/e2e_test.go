package tui

import (
	"bytes"
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
)

// TestEndToEnd drives the real Bubble Tea program against the demo service:
// boot, add a running server from the picker, then quit keeping forwards.
func TestEndToEnd(t *testing.T) {
	tm := teatest.NewTestModel(t, New(context.Background(), DemoDeps(10*time.Millisecond)),
		teatest.WithInitialTermSize(110, 45))

	waitFor := func(s string) {
		t.Helper()
		teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return bytes.Contains(b, []byte(s)) },
			teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
	}

	waitFor("Ubuntu-24.04") // dashboard card
	tm.Type("a")
	waitFor("Pick from running servers")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitFor("storybook")
	tm.Type("x")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitFor("Open 1 port to your network?")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitFor("1 port open to the network!")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	tm.Type("q")
	waitFor("Heading out?")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})

	final := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(Model)
	e, ok := final.snap.Entry(6006)
	if !ok || !e.Managed || e.Status != forward.StatusLive {
		t.Errorf("6006 after adding: %+v (found %v)", e, ok)
	}
}
