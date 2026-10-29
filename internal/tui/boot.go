package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	checkNameWidth = 18
	// The boot body has a fixed size so centering it doesn't make it jump
	// around as checklist lines appear.
	bootWidth  = 78
	bootHeight = 22
)

func (m Model) bootView() string {
	t := m.theme
	lines := []string{m.title(), t.Tagline.Render("warming up the port-a-tron…"), ""}

	if !m.detected {
		lines = append(lines, m.spinner.View()+" "+t.Body.Render("Sniffing the environment"))
	}
	width := min(bootWidth, m.width-4)
	detailWidth := max(10, width-checkNameWidth-4)
	for i, c := range m.report.Checks {
		switch {
		case i < m.revealed:
			mark := t.OK.Render("✓")
			if !c.OK {
				mark = t.Bad.Render("✗")
			}
			lines = append(lines, fmt.Sprintf("%s %s %s", mark,
				t.Body.Width(checkNameWidth).Render(c.Name),
				t.Dim.Render(ansi.Truncate(c.Detail, detailWidth, "…"))))
		case i == m.revealed:
			lines = append(lines, m.spinner.View()+" "+t.Body.Render(c.Name))
		}
	}

	switch {
	case m.fatal != nil:
		lines = append(lines, "", m.fatalBox())
	case m.gatherErr != nil:
		lines = append(lines,
			t.Bad.Render("✗")+" "+t.Body.Width(checkNameWidth).Render("Network info")+" "+
				t.Dim.Render(ansi.Truncate(m.gatherErr.Error(), detailWidth, "…")),
			"", t.Dim.Render("press ")+t.Accent.Render("r")+t.Dim.Render(" to retry · ")+
				t.Accent.Render("q")+t.Dim.Render(" to quit"))
	case m.bootDone:
		lines = append(lines, t.OK.Render("✓")+" "+t.Body.Width(checkNameWidth).Render("Network info")+" "+
			t.Dim.Render("mapped! 🚀"))
	case m.gathering:
		lines = append(lines, m.spinner.View()+" "+t.Body.Render("Asking Windows for its IPs"))
	}

	body := lipgloss.NewStyle().Width(width).Height(min(bootHeight, m.height)).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) fatalBox() string {
	t := m.theme
	msg := m.fatal.Error()
	if i := strings.Index(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		t.Bad.Render("serve-a-port needs WSL2 🐧"),
		"",
		t.Body.Width(min(60, max(20, m.width-12))).Render(msg),
		"",
		t.Dim.Render("press ")+t.Accent.Render("q")+t.Dim.Render(" to quit"),
	)
	return t.ErrorBox.Render(content)
}
