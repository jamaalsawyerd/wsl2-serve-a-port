package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/winexec"
)

// addPorts opens the add form and submits spec, leaving the model on the
// confirm screen.
func addPorts(spec string) []tea.Msg {
	msgs := []tea.Msg{press("a")}
	msgs = append(msgs, typed(spec)...)
	return append(msgs, press("enter"))
}

// TestBorderedLinesAreSingleWidth guards against emoji inside boxes.
// Terminals disagree on emoji widths (Windows Terminal draws them two cells
// wide, xterm.js one), and any disagreement shifts the right border.
func TestBorderedLinesAreSingleWidth(t *testing.T) {
	empty := sampleSnapshot()
	empty.Entries = nil
	fake := func() *fakeService { return &fakeService{snap: sampleSnapshot()} }
	typing := func() *fakeService { return &fakeService{snap: noCandidates()} }
	declined := &fakeService{snap: sampleSnapshot(), applyErr: winexec.ErrElevationDeclined}

	screens := map[string]Model{
		"dashboard": dashboard(t, fake()),
		"empty":     dashboard(t, &fakeService{snap: empty}),
		"confirm":   send(t, dashboard(t, typing()), addPorts("8081 5000")...),
		"added":     send(t, dashboard(t, typing()), append(addPorts("8000"), press("enter"))...),
		"removed":   send(t, dashboard(t, fake()), press("d"), press("enter")),
		"repaired":  send(t, dashboard(t, fake()), press("r"), press("enter")),
		"declined":  send(t, dashboard(t, declined), press("d"), press("enter")),
		"qr":        send(t, dashboard(t, fake()), press("enter")),
		"quit":      send(t, dashboard(t, fake()), press("q")),
		"fatal":     boot(t, newModel(badReport, &fakeService{}, 100, 30)),
	}
	for name, m := range screens {
		for i, line := range strings.Split(plain(m), "\n") {
			if !strings.ContainsAny(line, "│╮╯") {
				continue
			}
			if w, n := ansi.StringWidth(line), utf8.RuneCountInString(line); w != n {
				t.Errorf("%s line %d has wide characters inside a box (%d cells, %d runes): %q", name, i, w, n, line)
			}
		}
	}
}
