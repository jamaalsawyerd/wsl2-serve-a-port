package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"rsc.io/qr"
)

// qrQuiet is the blank margin around the code, in modules. The spec asks for
// four, but two scans reliably and saves precious terminal rows.
const qrQuiet = 2

var (
	qrDark  = lipgloss.Color("#000000")
	qrLight = lipgloss.Color("#FFFFFF")
)

// renderQR draws text as a QR code. Each character cell holds two modules
// stacked vertically ("▀" with the top module as foreground and the bottom
// as background), so the code comes out roughly square. Colors are always
// dark-on-light so phone cameras read it whatever the terminal theme.
func renderQR(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	color := func(black bool) lipgloss.Style {
		if black {
			return lipgloss.NewStyle().Foreground(qrDark)
		}
		return lipgloss.NewStyle().Foreground(qrLight)
	}

	var b strings.Builder
	for y := -qrQuiet; y < code.Size+qrQuiet; y += 2 {
		if y > -qrQuiet {
			b.WriteByte('\n')
		}
		// Emit runs of identical cells as one styled string to keep the
		// escape-code overhead down.
		x := -qrQuiet
		for x < code.Size+qrQuiet {
			top, bottom := code.Black(x, y), code.Black(x, y+1)
			run := 1
			for x+run < code.Size+qrQuiet && code.Black(x+run, y) == top && code.Black(x+run, y+1) == bottom {
				run++
			}
			bg := qrLight
			if bottom {
				bg = qrDark
			}
			b.WriteString(color(top).Background(bg).Render(strings.Repeat("▀", run)))
			x += run
		}
	}
	return b.String(), nil
}

// qrSize returns the rendered code's width and height in cells.
func qrSize(text string) (w, h int, err error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return 0, 0, err
	}
	side := code.Size + 2*qrQuiet
	return side, (side + 1) / 2, nil
}

func (m Model) qrView() string {
	t := m.theme
	hosts := m.hosts()
	url := m.urlFor(m.qrPort, m.qrHost)

	header := t.CardTitle.Render(fmt.Sprintf("Scan to open :%d", m.qrPort))
	var status string
	if e, ok := m.snap.Entry(m.qrPort); ok {
		icon, label, fg := t.Badge(e.Status)
		status = lipgloss.NewStyle().Foreground(fg).Render(icon + " " + label)
		if name := processName(e); name != "—" && name != "?" {
			status += t.Dim.Render(" · " + name)
		}
	}

	network := t.Dim.Render("no Windows network found")
	if len(hosts) > 0 {
		network = t.Dim.Render("via " + hosts[m.qrHost].Alias)
		if len(hosts) > 1 {
			network += t.Dim.Render(fmt.Sprintf("  (%d/%d)", m.qrHost+1, len(hosts)))
		}
	}

	code := t.Warn.Render("Make the terminal bigger to show the QR code.")
	if w, h, err := qrSize(url); err == nil && w+8 <= m.width && h+12 <= m.height {
		if rendered, err := renderQR(url); err == nil {
			code = rendered
		}
	}

	hints := []string{"c", "copy URL"}
	if len(hosts) > 1 {
		hints = append(hints, "←/→", "network")
	}
	hints = append(hints, "esc", "back")

	lines := []string{header, "", code, "", t.URL.Render(url), network}
	if status != "" {
		lines = append(lines, status)
	}
	if m.toast != "" {
		lines = append(lines, "", m.toastView())
	}
	lines = append(lines, "", m.keyHint(hints...))

	card := t.Card.BorderForeground(t.Cyan).Padding(1, 3).Render(
		lipgloss.JoinVertical(lipgloss.Center, lines...))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}
