package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
)

// Theme is the synthwave palette plus the styles built from it. It's rebuilt
// once the terminal reports whether its background is dark.
type Theme struct {
	IsDark bool

	Pink, Purple, Cyan, Green, Yellow, Orange, Red color.Color
	Text, Muted, Faint                             color.Color

	Tagline   lipgloss.Style
	Card      lipgloss.Style
	CardTitle lipgloss.Style
	BigValue  lipgloss.Style
	Body      lipgloss.Style
	Dim       lipgloss.Style
	Accent    lipgloss.Style
	URL       lipgloss.Style
	Header    lipgloss.Style
	Cell      lipgloss.Style
	Selected  lipgloss.Style
	Border    lipgloss.Style
	OK        lipgloss.Style
	Bad       lipgloss.Style
	Warn      lipgloss.Style
	ErrorBox  lipgloss.Style
}

// NewTheme builds the palette for a dark or light terminal.
func NewTheme(isDark bool) Theme {
	ld := lipgloss.LightDark(isDark)
	c := lipgloss.Color
	t := Theme{
		IsDark: isDark,
		Pink:   ld(c("#D6006F"), c("#FF2E97")),
		Purple: ld(c("#7B2FBE"), c("#B967FF")),
		Cyan:   ld(c("#00819F"), c("#01CDFE")),
		Green:  ld(c("#00875A"), c("#05FFA1")),
		Yellow: ld(c("#9A7B00"), c("#FFFB96")),
		Orange: ld(c("#C25E00"), c("#FF9E3D")),
		Red:    ld(c("#C8143C"), c("#FF5C7A")),
		Text:   ld(c("#1E1B2E"), c("#ECEAF6")),
		Muted:  ld(c("#6B6785"), c("#8D88AD")),
		Faint:  ld(c("#C9C4DE"), c("#3B3458")),
	}

	s := lipgloss.NewStyle
	t.Tagline = s().Foreground(t.Muted).Italic(true)
	t.Card = s().Border(lipgloss.RoundedBorder()).BorderForeground(t.Purple).Padding(0, 2)
	t.CardTitle = s().Foreground(t.Pink).Bold(true)
	t.BigValue = s().Foreground(t.Cyan).Bold(true)
	t.Body = s().Foreground(t.Text)
	t.Dim = s().Foreground(t.Muted)
	t.Accent = s().Foreground(t.Pink).Bold(true)
	t.URL = s().Foreground(t.Cyan).Underline(true)
	t.Header = s().Foreground(t.Purple).Bold(true).Padding(0, 1)
	t.Cell = s().Foreground(t.Text).Padding(0, 1)
	t.Selected = s().Foreground(t.Text).Background(t.Faint).Bold(true).Padding(0, 1)
	t.Border = s().Foreground(t.Purple)
	t.OK = s().Foreground(t.Green).Bold(true)
	t.Bad = s().Foreground(t.Red).Bold(true)
	t.Warn = s().Foreground(t.Orange)
	t.ErrorBox = s().Border(lipgloss.RoundedBorder()).BorderForeground(t.Red).Padding(1, 3)
	return t
}

// Badge returns the icon, label, and color for a forward's status.
func (t Theme) Badge(s forward.Status) (icon, label string, fg color.Color) {
	switch s {
	case forward.StatusLive:
		return "●", "live", t.Green
	case forward.StatusIdle:
		return "○", "idle", t.Muted
	case forward.StatusLocalOnly:
		return "◐", "localhost", t.Orange
	case forward.StatusStale:
		return "↻", "stale", t.Yellow
	case forward.StatusMissing:
		return "✗", "missing", t.Red
	case forward.StatusExternal:
		return "◆", "external", t.Purple
	default:
		return "?", string(s), t.Muted
	}
}
