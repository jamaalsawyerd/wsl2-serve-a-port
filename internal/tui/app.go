// Package tui is the Bubble Tea interface for serve-a-port.
package tui

import (
	"context"
	"image/color"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/wsl"
)

// Service is the part of forward.Manager the UI uses.
type Service interface {
	Snapshot(ctx context.Context) (forward.Snapshot, error)
}

// Deps are the UI's hooks into the system, swappable in tests.
type Deps struct {
	Detect  func(context.Context) wsl.Report
	Connect func(wsl.Report) (Service, error)
}

type screen int

const (
	screenBoot screen = iota
	screenDashboard
)

type (
	detectedMsg struct{ report wsl.Report }
	revealMsg   struct{}
	snapshotMsg struct {
		snap forward.Snapshot
		err  error
	}
	enterDashboardMsg struct{}
	frameMsg          struct{}
	toastExpiredMsg   struct{ id int }
)

const (
	frameInterval   = 90 * time.Millisecond
	revealInterval  = 110 * time.Millisecond
	bootHold        = 650 * time.Millisecond
	toastDuration   = 3 * time.Second
	snapshotTimeout = 30 * time.Second
)

type toastKind int

const (
	toastInfo toastKind = iota
	toastSuccess
	toastError
)

// Model is the root Bubble Tea model.
type Model struct {
	ctx  context.Context
	deps Deps
	svc  Service

	screen  screen
	theme   Theme
	palette []color.Color
	frame   int
	width   int
	height  int
	keys    keyMap
	help    help.Model
	spinner spinner.Model

	// Boot.
	report    wsl.Report
	detected  bool
	revealed  int
	fatal     error
	gathering bool
	gatherErr error
	bootDone  bool

	// Dashboard.
	snap       forward.Snapshot
	cursor     int
	refreshing bool

	toast     string
	toastKind toastKind
	toastID   int
}

// New returns the root model. ctx bounds every system call it makes.
func New(ctx context.Context, deps Deps) Model {
	m := Model{
		ctx:    ctx,
		deps:   deps,
		keys:   newKeyMap(),
		help:   help.New(),
		width:  80,
		height: 24,
	}
	m.setTheme(NewTheme(true))
	return m
}

func (m *Model) setTheme(t Theme) {
	m.theme = t
	m.palette = gradientLoop(t)
	m.spinner = spinner.New(
		spinner.WithSpinner(spinner.Points),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(t.Pink)),
	)
	hs := help.DefaultStyles(t.IsDark)
	hs.ShortKey = lipgloss.NewStyle().Foreground(t.Pink).Bold(true)
	hs.ShortDesc = lipgloss.NewStyle().Foreground(t.Muted)
	hs.ShortSeparator = lipgloss.NewStyle().Foreground(t.Faint)
	hs.FullKey, hs.FullDesc, hs.FullSeparator = hs.ShortKey, hs.ShortDesc, hs.ShortSeparator
	m.help.Styles = hs
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.detect(), m.spinner.Tick, frameTick())
}

func frameTick() tea.Cmd {
	return tea.Tick(frameInterval, func(time.Time) tea.Msg { return frameMsg{} })
}

func revealTick() tea.Cmd {
	return tea.Tick(revealInterval, func(time.Time) tea.Msg { return revealMsg{} })
}

func (m Model) detect() tea.Cmd {
	return func() tea.Msg { return detectedMsg{m.deps.Detect(m.ctx)} }
}

func (m Model) fetch() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, snapshotTimeout)
		defer cancel()
		snap, err := svc.Snapshot(ctx)
		return snapshotMsg{snap, err}
	}
}

func (m *Model) showToast(kind toastKind, text string) tea.Cmd {
	m.toastID++
	m.toast, m.toastKind = text, kind
	id := m.toastID
	return tea.Tick(toastDuration, func(time.Time) tea.Msg { return toastExpiredMsg{id} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.setTheme(NewTheme(msg.IsDark()))
		return m, m.spinner.Tick

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width - 4)
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.screen == screenBoot {
			return m.updateBootKeys(msg)
		}
		return m.updateDashboardKeys(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case frameMsg:
		m.frame++
		return m, frameTick()

	case detectedMsg:
		m.report, m.detected = msg.report, true
		return m, revealTick()

	case revealMsg:
		m.revealed++
		if m.revealed < len(m.report.Checks) {
			return m, revealTick()
		}
		if err := m.report.Err(); err != nil {
			m.fatal = err
			return m, nil
		}
		svc, err := m.deps.Connect(m.report)
		if err != nil {
			m.fatal = err
			return m, nil
		}
		m.svc, m.gathering = svc, true
		return m, m.fetch()

	case snapshotMsg:
		m.refreshing = false
		if msg.err != nil {
			if m.screen == screenBoot {
				m.gatherErr = msg.err
				return m, nil
			}
			return m, m.showToast(toastError, msg.err.Error())
		}
		m.snap = msg.snap
		m.clampCursor()
		if m.screen == screenBoot {
			m.bootDone = true
			return m, tea.Tick(bootHold, func(time.Time) tea.Msg { return enterDashboardMsg{} })
		}
		return m, nil

	case enterDashboardMsg:
		m.screen = screenDashboard
		return m, nil

	case toastExpiredMsg:
		if msg.id == m.toastID {
			m.toast = ""
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateBootKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case msg.String() == "r" && m.gatherErr != nil:
		m.gatherErr = nil
		return m, m.fetch()
	}
	return m, nil
}

func (m Model) updateDashboardKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Up):
		m.cursor--
		m.clampCursor()
	case key.Matches(msg, m.keys.Down):
		m.cursor++
		m.clampCursor()
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
	case key.Matches(msg, m.keys.Refresh):
		if !m.refreshing {
			m.refreshing = true
			return m, m.fetch()
		}
	case key.Matches(msg, m.keys.Add, m.keys.Remove, m.keys.RemoveAll, m.keys.Repair):
		return m, m.showToast(toastInfo, "🚧 Changing forwards from the UI arrives in the next phase")
	}
	return m, nil
}

func (m *Model) clampCursor() {
	m.cursor = max(0, min(m.cursor, len(m.snap.Entries)-1))
}

func (m Model) View() tea.View {
	var content string
	switch m.screen {
	case screenBoot:
		content = m.bootView()
	case screenDashboard:
		content = m.dashboardView()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "serve-a-port"
	return v
}

// title renders the animated banner, or a compact title if it won't fit.
func (m Model) title() string {
	if m.width >= bannerWidth+4 {
		return renderBanner(m.theme, m.palette, m.frame)
	}
	return renderCompactTitle(m.theme, m.palette, m.frame)
}
