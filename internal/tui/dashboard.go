package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/netinfo"
)

const (
	gutter = 2
	// tableChrome is the table's border and header lines.
	tableChrome = 4
	maxHints    = 3
)

func (m Model) dashboardView() string {
	t := m.theme
	inner := m.width - 2*gutter

	top := []string{m.title(), t.Tagline.Width(inner).Render("beam your WSL2 ports to every device on the network ✨"), ""}
	top = append(top, m.cardsView(inner), m.statusLine(), "")

	var bottom []string
	if d := m.detailView(); d != "" {
		bottom = append(bottom, d)
	}
	for _, h := range m.hintLines() {
		bottom = append(bottom, ansi.Truncate(h, inner, "…"))
	}
	for _, w := range m.snap.Warnings {
		bottom = append(bottom, t.Warn.Render(ansi.Truncate("⚠ "+w, inner, "…")))
	}
	if m.toast != "" {
		bottom = append(bottom, m.toastView())
	}
	// help.View doesn't always respect its width, so clamp it here.
	bottom = append(bottom, "", ansi.Truncate(m.help.View(m.keys), inner, "…"))

	used := lipgloss.Height(strings.Join(top, "\n")) + lipgloss.Height(strings.Join(bottom, "\n"))
	rows := max(3, m.height-used-tableChrome)

	body := lipgloss.JoinVertical(lipgloss.Left,
		append(append(top, m.forwardsView(inner, rows)), bottom...)...)
	return lipgloss.NewStyle().Padding(1, gutter).Render(body)
}

func (m Model) cardsView(width int) string {
	t := m.theme
	s := m.snap

	wslLines := []string{
		t.CardTitle.Render("❯_ WSL2"),
		t.BigValue.Render(s.WSL.IP.String()),
		t.Dim.Render(strings.Join(nonEmpty(s.WSL.Interface, m.report.Distro), " · ")),
	}

	winLines := []string{t.CardTitle.Render("⊞ Windows LAN")}
	if len(s.Windows) == 0 {
		winLines = append(winLines, t.Bad.Render("unknown"), t.Dim.Render("couldn't read adapters"))
	} else {
		primary := s.Windows[0]
		// Card borders and padding take 6 columns.
		lineWidth := max(10, min(40, width-6))
		winLines = append(winLines,
			t.BigValue.Render(primary.IP.String()),
			t.Dim.Render(ansi.Truncate(primary.Alias, lineWidth, "…")))
		for _, w := range s.Windows[1:] {
			winLines = append(winLines, t.Dim.Render(ansi.Truncate("+ "+w.IP.String()+"  "+w.Alias, lineWidth, "…")))
		}
	}

	wslCard := t.Card.Render(lipgloss.JoinVertical(lipgloss.Left, wslLines...))
	winCard := t.Card.Render(lipgloss.JoinVertical(lipgloss.Left, winLines...))
	if lipgloss.Width(wslCard)+lipgloss.Width(winCard)+2 <= width {
		return lipgloss.JoinHorizontal(lipgloss.Top, wslCard, "  ", winCard)
	}
	return lipgloss.JoinVertical(lipgloss.Left, wslCard, winCard)
}

func (m Model) statusLine() string {
	t := m.theme
	mode := "NAT mode"
	if m.snap.Mirrored {
		mode = "mirrored mode"
	}
	n := len(m.snap.Entries)
	parts := []string{mode, plural(n, "forward"), "updated " + m.snap.TakenAt.Format("15:04:05")}
	line := t.Dim.Render(ansi.Truncate(strings.Join(parts, " · "), m.width-2*gutter-4, "…"))
	if m.refreshing {
		line += " " + m.spinner.View()
	}
	return line
}

func (m Model) forwardsView(width, maxRows int) string {
	t := m.theme
	entries := m.snap.Entries
	if len(entries) == 0 {
		msg := lipgloss.JoinVertical(lipgloss.Center,
			t.Body.Render("No ports forwarded yet."),
			t.Dim.Render("Press ")+t.Accent.Render("a")+t.Dim.Render(" to open one."))
		return t.Card.BorderForeground(t.Faint).Padding(1, 4).Render(msg)
	}

	// Scroll just enough to keep the cursor on screen.
	offset := max(0, m.cursor-maxRows+1)
	visible := entries[offset:min(len(entries), offset+maxRows)]

	tbl := m.forwardsTable(visible, offset, true)
	if lipgloss.Width(tbl.String()) > width {
		// Drop the least useful column first, then truncate cells.
		tbl = m.forwardsTable(visible, offset, false)
		if lipgloss.Width(tbl.String()) > width {
			tbl = tbl.Width(width).Wrap(false)
		}
	}

	out := tbl.String()
	if len(entries) > maxRows {
		out += "\n" + t.Dim.Render(fmt.Sprintf("  %d–%d of %d", offset+1, offset+len(visible), len(entries)))
	}
	return out
}

type column int

const (
	colPort column = iota
	colStatus
	colProcess
	colURL
)

func (m Model) forwardsTable(visible []forward.Entry, offset int, withProcess bool) *table.Table {
	t := m.theme
	cols := []column{colPort, colStatus, colProcess, colURL}
	if !withProcess {
		cols = slices.DeleteFunc(cols, func(c column) bool { return c == colProcess })
	}
	headers := map[column]string{colPort: "  PORT", colStatus: "STATUS", colProcess: "PROCESS", colURL: "SHARE URL"}

	var hdr []string
	for _, c := range cols {
		hdr = append(hdr, headers[c])
	}
	rows := make([][]string, len(visible))
	for i, e := range visible {
		for _, c := range cols {
			rows[i] = append(rows[i], m.cell(e, c, offset+i == m.cursor))
		}
	}

	return table.New().
		Headers(hdr...).
		Rows(rows...).
		Border(lipgloss.RoundedBorder()).
		BorderStyle(t.Border).
		BorderColumn(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return t.Header
			}
			style := t.Cell
			if offset+row == m.cursor {
				style = t.Selected
			}
			switch cols[col] {
			case colStatus:
				_, _, fg := t.Badge(visible[row].Status)
				style = style.Foreground(fg)
			case colProcess:
				style = style.Foreground(t.Muted)
			case colURL:
				style = style.Foreground(t.Cyan)
			}
			return style
		})
}

func (m Model) cell(e forward.Entry, c column, selected bool) string {
	switch c {
	case colPort:
		if selected {
			return "▸ " + strconv.Itoa(e.Port)
		}
		return "  " + strconv.Itoa(e.Port)
	case colStatus:
		icon, label, _ := m.theme.Badge(e.Status)
		return icon + " " + label
	case colProcess:
		return processName(e)
	default:
		return m.shareURL(e.Port)
	}
}

// detailView explains the selected entry's status in plain words.
func (m Model) detailView() string {
	if len(m.snap.Entries) == 0 {
		return ""
	}
	t := m.theme
	e := m.snap.Entries[m.cursor]
	port := strconv.Itoa(e.Port)
	var msg string
	switch e.Status {
	case forward.StatusLive:
		msg = "✨ Live! Anyone on your network can open " + t.URL.Render(m.shareURL(e.Port))
	case forward.StatusIdle:
		msg = "Forwarded, but nothing in WSL is listening on :" + port + " yet."
	case forward.StatusLocalOnly:
		msg = "Your server on :" + port + " only listens on localhost. Restart it bound to 0.0.0.0 (e.g. --host 0.0.0.0)."
	case forward.StatusStale:
		msg = "Points at an old WSL IP (" + e.Rule.ConnectAddress + "). Press " + t.Accent.Render("r") + t.Body.Render(" to repair.")
	case forward.StatusMissing:
		msg = "The Windows rules for :" + port + " have gone missing. Press " + t.Accent.Render("r") + t.Body.Render(" to repair.")
	case forward.StatusExternal:
		target := "?"
		if e.Rule != nil {
			target = fmt.Sprintf("%s:%d", e.Rule.ConnectAddress, e.Rule.ConnectPort)
		}
		msg = "Created outside serve-a-port, forwarding to " + target + "."
	}
	return t.Body.Width(m.width - 2*gutter).Render(msg)
}

// hintLines suggests forwarding reachable WSL servers that aren't forwarded.
func (m Model) hintLines() []string {
	t := m.theme
	var out []string
	for _, l := range m.snap.Listeners {
		if !l.Reachable || slices.ContainsFunc(m.snap.Entries, func(e forward.Entry) bool { return e.Port == l.Port }) {
			continue
		}
		who := "Something"
		if l.Process != "" {
			who = l.Process
		}
		out = append(out, t.Dim.Render(fmt.Sprintf("💡 %s is listening on :%d. Press ", who, l.Port))+
			t.Accent.Render("a")+t.Dim.Render(" to share it."))
		if len(out) == maxHints {
			break
		}
	}
	return out
}

func (m Model) toastView() string {
	t := m.theme
	switch m.toastKind {
	case toastSuccess:
		return t.OK.Render(m.toast)
	case toastError:
		return t.Bad.Render("✗ " + m.toast)
	default:
		return t.Accent.Render(m.toast)
	}
}

// hosts are the Windows addresses other devices can reach, best first. In
// mirrored mode WSL shares these addresses, so they work there too.
func (m Model) hosts() []netinfo.WindowsAddr { return m.snap.Windows }

// urlFor is the share URL for port via the host'th Windows address.
func (m Model) urlFor(port, host int) string {
	hosts := m.hosts()
	if len(hosts) == 0 {
		return "—"
	}
	return fmt.Sprintf("http://%s:%d", hosts[host%len(hosts)].IP, port)
}

func (m Model) shareURL(port int) string { return m.urlFor(port, 0) }

func processName(e forward.Entry) string {
	switch {
	case e.Listener == nil:
		return "—"
	case e.Listener.Process == "":
		return "?"
	default:
		return e.Listener.Process
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func nonEmpty(ss ...string) []string {
	return slices.DeleteFunc(ss, func(s string) bool { return s == "" })
}
