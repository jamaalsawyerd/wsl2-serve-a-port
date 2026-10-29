package tui

import (
	"image/color"
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"
)

const (
	celebrateFPS   = 30
	confettiHeight = 5
	confettiCount  = 70
	// titleSlide is how far right the success title starts before springing
	// into place.
	titleSlide = 24.0
	// celebrateMaxFrames stops the animation even if something never
	// settles.
	celebrateMaxFrames = 4 * celebrateFPS
)

var confettiGlyphs = []string{"✦", "✧", "•", "*", "+", "·", "˚"}

type celebrateMsg struct{ id int }

type particle struct {
	x, y   float64
	vx, vy float64
	glyph  string
	color  color.Color
}

// celebration is a confetti burst plus a title that springs in from the
// right. The zero value is inactive.
type celebration struct {
	id        int
	frames    int
	particles []particle
	spring    harmonica.Spring
	offset    float64
	velocity  float64
}

func (c celebration) active() bool { return c.frames > 0 }

func celebrateTick(id int) tea.Cmd {
	return tea.Tick(time.Second/celebrateFPS, func(time.Time) tea.Msg { return celebrateMsg{id} })
}

// startCelebration launches a burst from the bottom middle of a band width
// cells wide.
func (m *Model) startCelebration(width int) tea.Cmd {
	t := m.theme
	colors := []color.Color{t.Pink, t.Purple, t.Cyan, t.Green, t.Yellow, t.Orange}
	ps := make([]particle, confettiCount)
	for i := range ps {
		ps[i] = particle{
			x:     float64(width) / 2,
			y:     confettiHeight,
			vx:    (rand.Float64()*2 - 1) * float64(width) / 2.2,
			vy:    -(6 + rand.Float64()*10),
			glyph: confettiGlyphs[rand.IntN(len(confettiGlyphs))],
			color: colors[rand.IntN(len(colors))],
		}
	}
	id := m.celebrate.id + 1
	m.celebrate = celebration{
		id:        id,
		frames:    1,
		particles: ps,
		spring:    harmonica.NewSpring(harmonica.FPS(celebrateFPS), 7.0, 0.35),
		offset:    titleSlide,
	}
	return celebrateTick(id)
}

// step advances the animation one frame and reports whether it's still
// running.
func (c celebration) step() (celebration, bool) {
	const dt, gravity = 1.0 / celebrateFPS, 18.0
	c.frames++
	c.offset, c.velocity = c.spring.Update(c.offset, c.velocity, 0)

	alive := c.particles[:0:0]
	for _, p := range c.particles {
		p.vy += gravity * dt
		p.x += p.vx * dt
		p.y += p.vy * dt
		if p.y < confettiHeight+1 {
			alive = append(alive, p)
		}
	}
	c.particles = alive

	settled := len(alive) == 0 && abs(c.offset) < 0.5 && abs(c.velocity) < 0.5
	if settled || c.frames > celebrateMaxFrames {
		return celebration{id: c.id}, false
	}
	return c, true
}

// confettiView renders the particles into a band width cells wide.
func (c celebration) confettiView(width int) string {
	grid := make([][]string, confettiHeight)
	for y := range grid {
		grid[y] = make([]string, width)
		for x := range grid[y] {
			grid[y][x] = " "
		}
	}
	for _, p := range c.particles {
		x, y := int(p.x), int(p.y)
		if x >= 0 && x < width && y >= 0 && y < confettiHeight {
			grid[y][x] = lipgloss.NewStyle().Foreground(p.color).Render(p.glyph)
		}
	}
	rows := make([]string, confettiHeight)
	for y, row := range grid {
		rows[y] = strings.Join(row, "")
	}
	return strings.Join(rows, "\n")
}

// titleOffset is how many cells to indent the springing title. The spring
// overshoots past zero; mirroring it makes the title bounce off the left
// edge.
func (c celebration) titleOffset() int {
	if !c.active() {
		return 0
	}
	return int(abs(c.offset) + 0.5)
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
