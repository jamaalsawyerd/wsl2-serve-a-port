package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// glyphs is a three-row box-drawing font in the style of figlet's "Calvin S".
var glyphs = map[rune][3]string{
	'S': {"╔═╗", "╚═╗", "╚═╝"},
	'E': {"╔═╗", "║╣ ", "╚═╝"},
	'R': {"╦═╗", "╠╦╝", "╩╚═"},
	'V': {"╦  ╦", "╚╗╔╝", " ╚╝ "},
	'A': {"╔═╗", "╠═╣", "╩ ╩"},
	'P': {"╔═╗", "╠═╝", "╩  "},
	'O': {"╔═╗", "║ ║", "╚═╝"},
	'T': {"╔╦╗", " ║ ", " ╩ "},
	'-': {"  ", "──", "  "},
	' ': {"  ", "  ", "  "},
}

const bannerText = "SERVE-A-PORT"

// bannerRows lays out text in the glyph font, one string per row.
func bannerRows(text string) [3][]rune {
	var rows [3][]rune
	for _, r := range text {
		g, ok := glyphs[r]
		if !ok {
			g = glyphs[' ']
		}
		for i := range rows {
			rows[i] = append(rows[i], []rune(g[i])...)
		}
	}
	return rows
}

// bannerWidth is the banner's width in cells.
var bannerWidth = len(bannerRows(bannerText)[0])

// gradientCycle is the length of the looping banner gradient, in columns.
const gradientCycle = 48

// gradientLoop returns a palette that blends pink → purple → cyan and back,
// so shifting it by one column per frame loops seamlessly.
func gradientLoop(t Theme) []color.Color {
	return lipgloss.Blend1D(gradientCycle, t.Pink, t.Purple, t.Cyan, t.Purple, t.Pink)
}

// renderBanner draws the title with the gradient shifted by frame.
func renderBanner(t Theme, palette []color.Color, frame int) string {
	rows := bannerRows(bannerText)
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		for col, r := range row {
			if r == ' ' {
				b.WriteRune(r)
				continue
			}
			c := palette[(col+frame)%len(palette)]
			b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(r)))
		}
	}
	return b.String()
}

// renderCompactTitle is the one-line title for narrow terminals.
func renderCompactTitle(t Theme, palette []color.Color, frame int) string {
	var b strings.Builder
	for i, r := range "serve-a-port" {
		c := palette[(i*3+frame)%len(palette)]
		b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(r)))
	}
	return b.String()
}
