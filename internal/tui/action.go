package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/portproxy"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/ports"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/winexec"
)

// applyTimeout is generous because it includes the user finding and
// answering the UAC prompt.
const (
	applyTimeout = 5 * time.Minute
	maxListed    = 8
)

type actionKind int

const (
	actAdd actionKind = iota
	actRemove
	actRepair
)

// action is a pending change, shown on the confirm screen before it runs.
type action struct {
	kind  actionKind
	ports []int
	// takeOver replaces portproxy rules made by other tools (add only).
	takeOver bool
}

type applyDoneMsg struct {
	act     action
	results []portproxy.Result
	err     error
}

func (m Model) addAction(ps []int) action {
	act := action{kind: actAdd, ports: ps}
	for _, p := range ps {
		if e, ok := m.snap.Entry(p); ok && !e.Managed {
			act.takeOver = true
		}
	}
	return act
}

func (m Model) apply(act action) tea.Cmd {
	svc, snap := m.svc, m.snap
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, applyTimeout)
		defer cancel()
		var (
			results []portproxy.Result
			err     error
		)
		switch act.kind {
		case actAdd:
			results, err = svc.Add(ctx, snap, act.ports, act.takeOver)
		case actRemove:
			results, err = svc.Remove(ctx, act.ports)
		case actRepair:
			results, err = svc.Repair(ctx, snap)
		}
		return applyDoneMsg{act, results, err}
	}
}

// page frames a screen's content under the banner.
func (m Model) page(content string) string {
	t := m.theme
	return lipgloss.NewStyle().Padding(1, gutter).Render(lipgloss.JoinVertical(lipgloss.Left,
		m.title(), t.Tagline.Render("beam your WSL2 ports to every device on the network ✨"), "", content))
}

func (m Model) keyHint(pairs ...string) string {
	t := m.theme
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, t.Accent.Render(pairs[i])+" "+t.Dim.Render(pairs[i+1]))
	}
	return strings.Join(parts, t.Dim.Render(" · "))
}

func (m Model) confirmView() string {
	t := m.theme
	act := m.pending
	ip := m.snap.WSL.IP.String()
	spec := ports.Format(act.ports)

	var title string
	switch act.kind {
	case actAdd:
		title = "Open " + plural(len(act.ports), "port") + " to your network?"
	case actRemove:
		title = "Stop forwarding " + plural(len(act.ports), "port") + "?"
	case actRepair:
		title = "Repair " + plural(len(act.ports), "forward") + "?"
	}

	lines := []string{t.CardTitle.Render(title), ""}
	for i, p := range act.ports {
		if i == maxListed {
			lines = append(lines, t.Dim.Render(fmt.Sprintf("  …and %d more", len(act.ports)-maxListed)))
			break
		}
		lines = append(lines, m.confirmRow(act.kind, p))
	}

	lines = append(lines, "", t.Dim.Render("Runs as administrator:"))
	switch {
	case act.kind == actRemove:
		lines = append(lines,
			t.Body.Render("  netsh portproxy  ")+t.Dim.Render("delete "+spec),
			t.Body.Render("  firewall         ")+t.Dim.Render("delete rules for "+spec))
	case m.snap.Mirrored:
		lines = append(lines,
			t.Body.Render("  Hyper-V firewall ")+t.Dim.Render("allow inbound TCP "+spec))
	default:
		lines = append(lines,
			t.Body.Render("  netsh portproxy  ")+t.Dim.Render("0.0.0.0:"+spec+" → "+ip),
			t.Body.Render("  firewall         ")+t.Dim.Render("allow inbound TCP "+spec))
	}

	lines = append(lines, "", t.Dim.Render("Windows will ask for admin permission once."), "",
		m.keyHint("enter", "do it", "esc", "cancel"))

	width := min(maxFormWide, m.width-2*gutter)
	return m.page(t.Card.BorderForeground(t.Pink).Padding(1, 2).Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left, lines...)))
}

// confirmRow describes what will happen to one port, with any caveats.
func (m Model) confirmRow(kind actionKind, port int) string {
	t := m.theme
	e, known := m.snap.Entry(port)
	head := t.Accent.Render(fmt.Sprintf("  %-6d", port))

	switch kind {
	case actRemove:
		note := t.Dim.Render(m.shareURL(port))
		if known && !e.Managed {
			note = t.Warn.Render("made by another tool")
		}
		return head + note

	case actRepair:
		if known && e.Status == forward.StatusStale && e.Rule != nil {
			return head + t.Dim.Render(e.Rule.ConnectAddress+" → ") + t.Body.Render(m.snap.WSL.IP.String())
		}
		return head + t.Dim.Render("recreate missing rules")
	}

	row := head + t.Dim.Render("→ "+m.snap.WSL.IP.String()+":"+strconv.Itoa(port))
	l := m.listener(port)
	switch {
	case known && !e.Managed:
		row += "  " + t.Warn.Render("⚠ replaces another tool's rule")
	case known:
		row += "  " + t.Dim.Render("(already forwarded; re-applies)")
	case l == nil:
		row += "  " + t.Dim.Render("(nothing listening yet)")
	case !l.Reachable:
		row += "  " + t.Warn.Render("⚠ only listens on localhost")
	case l.Process != "":
		row += "  " + t.OK.Render("● "+l.Process)
	default:
		row += "  " + t.OK.Render("● listening")
	}
	return row
}

func (m Model) quitView() string {
	t := m.theme
	managed := m.managedPorts()
	width := m.resultWidth()
	lines := []string{
		t.CardTitle.Render("Heading out? 👋"),
		"",
		t.Body.Width(width - 6).Render(plural(len(managed), "port") + " will stay open to your network:"),
		t.Accent.Render("  " + ansi.Truncate(ports.Format(managed), width-10, "…")),
		"",
		t.Dim.Width(width - 6).Render("Forwards survive reboots. If the WSL IP changes, run serve-a-port again and press r."),
		"",
		m.keyHint("enter", "keep & quit", "t", "tear down & quit", "esc", "stay"),
	}
	return m.page(t.Card.Padding(1, 2).Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...)))
}

func (m Model) applyingView() string {
	t := m.theme
	content := lipgloss.JoinVertical(lipgloss.Left,
		m.spinner.View()+" "+t.Body.Bold(true).Render("Waiting for Windows…"),
		"",
		t.Dim.Width(min(maxFormWide, m.width-2*gutter)).Render(
			"Approve the admin prompt (UAC) to continue. If you don't see it, check your taskbar."),
	)
	return m.page(content)
}

func (m Model) resultWidth() int { return min(maxFormWide, m.width-2*gutter) }

func (m Model) resultView() string {
	t := m.theme
	width := m.resultWidth()
	var lines []string

	switch {
	case errors.Is(m.applyErr, winexec.ErrElevationDeclined):
		lines = append(lines, t.Warn.Bold(true).Render("😶 Admin permission declined"), "",
			t.Body.Render("Nothing was changed."))
	case m.applyErr != nil:
		lines = append(lines, t.Bad.Render("✗ That didn't work"), "",
			t.Body.Width(width-6).Render(m.applyErr.Error()))
	default:
		ok := 0
		for _, r := range m.results {
			if r.OK {
				ok++
			}
		}
		switch {
		case ok == len(m.results):
			lines = append(lines, strings.Repeat(" ", m.celebrate.titleOffset())+t.OK.Render(m.successTitle()))
		case ok == 0:
			lines = append(lines, t.Bad.Render("✗ Nothing changed"))
		default:
			lines = append(lines, t.Warn.Bold(true).Render(fmt.Sprintf("⚠ %d of %d worked", ok, len(m.results))))
		}
		lines = append(lines, "")
		for i, r := range m.results {
			if i == maxListed {
				lines = append(lines, t.Dim.Render(fmt.Sprintf("  …and %d more", len(m.results)-maxListed)))
				break
			}
			lines = append(lines, m.resultRow(r, width-6))
		}
	}

	lines = append(lines, "", m.keyHint("enter", "back to dashboard"))
	card := t.Card.Padding(1, 2).Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	if m.party {
		// The band stays (blank) after the confetti lands so the card
		// doesn't jump.
		card = m.celebrate.confettiView(width) + "\n" + card
	}
	return m.page(card)
}

func (m Model) successTitle() string {
	n := len(m.results)
	switch m.pending.kind {
	case actAdd:
		return "🎉 " + plural(n, "port") + " open to the network!"
	case actRemove:
		return "👋 " + plural(n, "port") + " closed"
	default:
		return "🔧 " + plural(n, "forward") + " repaired"
	}
}

func (m Model) resultRow(r portproxy.Result, width int) string {
	t := m.theme
	head := fmt.Sprintf("%-6d", r.Port)
	if !r.OK {
		row := t.Bad.Render("✗ ") + t.Body.Render(head) + t.Dim.Render(ansi.Truncate(r.Message, max(10, width-10), "…"))
		if hint := portproxy.Hint(r.Message); hint != "" {
			row += "\n" + t.Warn.Width(width).Render("  ↳ "+hint)
		}
		return row
	}
	if r.Op == portproxy.OpAdd {
		return t.OK.Render("✓ ") + t.Body.Render(head) + t.URL.Render(m.shareURL(r.Port))
	}
	return t.OK.Render("✓ ") + t.Body.Render(head) + t.Dim.Render("removed")
}
