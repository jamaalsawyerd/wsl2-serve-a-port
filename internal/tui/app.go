// Package tui is the Bubble Tea interface for serve-a-port.
package tui

import (
	"context"
	"fmt"
	"image/color"
	"slices"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/netinfo"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/portproxy"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/ports"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/wsl"
)

// Service is the part of forward.Manager the UI uses.
type Service interface {
	Snapshot(ctx context.Context) (forward.Snapshot, error)
	Add(ctx context.Context, snap forward.Snapshot, ports []int, takeOver bool) ([]portproxy.Result, error)
	Remove(ctx context.Context, ports []int) ([]portproxy.Result, error)
	Repair(ctx context.Context, snap forward.Snapshot) ([]portproxy.Result, error)
	Relisten(snap forward.Snapshot) (forward.Snapshot, error)
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
	screenAdd
	screenConfirm
	screenApplying
	screenResult
	screenQR
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
	relistenTickMsg   struct{}
	fullRefreshMsg    struct{}
	liveMsg           struct {
		snap forward.Snapshot
		gen  int
		err  error
	}
)

const (
	frameInterval   = 90 * time.Millisecond
	revealInterval  = 110 * time.Millisecond
	bootHold        = 650 * time.Millisecond
	toastDuration   = 3 * time.Second
	snapshotTimeout = 30 * time.Second
	// relistenInterval polls the cheap WSL-side state; fullRefreshInterval
	// re-asks Windows, which costs a few PowerShell launches.
	relistenInterval    = 3 * time.Second
	fullRefreshInterval = time.Minute
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
	// snapGen counts full snapshots, so a background relisten started
	// before one can't overwrite it.
	snapGen int

	toast     string
	toastKind toastKind
	toastID   int

	// Add form, confirm, apply, and result.
	form     *huh.Form
	add      *addForm
	pending  action
	results  []portproxy.Result
	applyErr error

	// Fun.
	qrPort    int
	qrHost    int
	celebrate celebration
	party     bool
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

func (m Model) relisten() tea.Cmd {
	svc, snap, gen := m.svc, m.snap, m.snapGen
	return func() tea.Msg {
		s, err := svc.Relisten(snap)
		return liveMsg{s, gen, err}
	}
}

func relistenTick() tea.Cmd {
	return tea.Tick(relistenInterval, func(time.Time) tea.Msg { return relistenTickMsg{} })
}

func fullRefreshTick() tea.Cmd {
	return tea.Tick(fullRefreshInterval, func(time.Time) tea.Msg { return fullRefreshMsg{} })
}

// watching reports whether the current screen shows live status.
func (m Model) watching() bool {
	return m.svc != nil && (m.screen == screenDashboard || m.screen == screenQR)
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
		switch m.screen {
		case screenBoot:
			return m.updateBootKeys(msg)
		case screenDashboard:
			return m.updateDashboardKeys(msg)
		case screenAdd:
			if msg.String() == "esc" {
				m.screen, m.form = screenDashboard, nil
				return m, nil
			}
			return m.updateForm(msg)
		case screenConfirm:
			switch msg.String() {
			case "enter", "y":
				m.screen = screenApplying
				return m, m.apply(m.pending)
			case "esc", "n", "q":
				m.screen = screenDashboard
			}
		case screenResult:
			switch msg.String() {
			case "enter", "esc", "q", " ":
				m.screen, m.party = screenDashboard, false
			}
		case screenQR:
			return m.updateQRKeys(msg)
		}
		return m, nil

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
		m.snapGen++
		m.clampCursor()
		if m.screen == screenBoot {
			m.bootDone = true
			return m, tea.Tick(bootHold, func(time.Time) tea.Msg { return enterDashboardMsg{} })
		}
		return m, nil

	case enterDashboardMsg:
		m.screen = screenDashboard
		return m, tea.Batch(relistenTick(), fullRefreshTick())

	case relistenTickMsg:
		if m.watching() && !m.refreshing {
			return m, tea.Batch(m.relisten(), relistenTick())
		}
		return m, relistenTick()

	case fullRefreshMsg:
		if m.watching() && !m.refreshing {
			m.refreshing = true
			return m, tea.Batch(m.fetch(), fullRefreshTick())
		}
		return m, fullRefreshTick()

	case liveMsg:
		// Polling errors are transient (or will surface on the next full
		// refresh), and results based on an older snapshot are stale.
		if msg.err != nil || msg.gen != m.snapGen || m.refreshing {
			return m, nil
		}
		note := liveChanges(m.snap, msg.snap)
		m.snap = msg.snap
		m.clampCursor()
		if note != "" {
			return m, m.showToast(toastSuccess, note)
		}
		return m, nil

	case celebrateMsg:
		if msg.id != m.celebrate.id || !m.celebrate.active() {
			return m, nil
		}
		var running bool
		if m.celebrate, running = m.celebrate.step(); running {
			return m, celebrateTick(msg.id)
		}
		return m, nil

	case toastExpiredMsg:
		if msg.id == m.toastID {
			m.toast = ""
		}
		return m, nil

	case applyDoneMsg:
		m.screen = screenResult
		m.results, m.applyErr = msg.results, msg.err
		m.refreshing = true
		cmds := []tea.Cmd{m.fetch()}
		if msg.act.kind != actRemove && msg.err == nil && slices.ContainsFunc(msg.results, func(r portproxy.Result) bool { return r.OK }) {
			m.party = true
			cmds = append(cmds, m.startCelebration(m.resultWidth()))
		}
		return m, tea.Batch(cmds...)
	}

	// Anything else is internal to the form (cursor blinks, field changes).
	if m.screen == screenAdd {
		return m.updateForm(msg)
	}
	return m, nil
}

func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	f, cmd := m.form.Update(msg)
	m.form = f.(*huh.Form)
	switch m.form.State {
	case huh.StateAborted:
		m.screen, m.form = screenDashboard, nil
		return m, nil
	case huh.StateCompleted:
		m.form = nil
		ps, err := m.add.ports()
		if err != nil {
			m.screen = screenDashboard
			return m, m.showToast(toastError, err.Error())
		}
		m.pending, m.screen = m.addAction(ps), screenConfirm
		return m, nil
	}
	return m, cmd
}

func (m Model) updateQRKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := max(1, len(m.hosts()))
	switch msg.String() {
	case "esc", "enter", "q":
		m.screen = screenDashboard
	case "right", "l", "tab":
		m.qrHost = (m.qrHost + 1) % n
	case "left", "h", "shift+tab":
		m.qrHost = (m.qrHost + n - 1) % n
	case "c":
		if len(m.hosts()) > 0 {
			return m.copyURL(m.urlFor(m.qrPort, m.qrHost))
		}
	}
	return m, nil
}

// copyURL puts url on the clipboard via OSC 52, which Windows Terminal
// supports, so it lands on the Windows clipboard.
func (m Model) copyURL(url string) (tea.Model, tea.Cmd) {
	toast := m.showToast(toastSuccess, "📋 Copied "+url)
	return m, tea.Batch(tea.SetClipboard(url), toast)
}

// liveChanges describes forwards that went live or stopped since old, for a
// toast. It returns "" when nothing interesting changed.
func liveChanges(old, cur forward.Snapshot) string {
	var up, down, stale []int
	for _, e := range cur.Entries {
		prev, ok := old.Entry(e.Port)
		if !ok || !e.Managed || prev.Status == e.Status {
			continue
		}
		switch {
		case e.Status == forward.StatusLive:
			up = append(up, e.Port)
		case e.Status == forward.StatusStale:
			stale = append(stale, e.Port)
		case prev.Status == forward.StatusLive:
			down = append(down, e.Port)
		}
	}
	switch {
	case len(stale) > 0:
		return "↻ The WSL IP changed. Press r to repair."
	case len(up) == 1:
		return fmt.Sprintf("✨ :%d is live!", up[0])
	case len(up) > 1:
		return "✨ " + ports.Format(up) + " are live!"
	case len(down) == 1:
		return fmt.Sprintf("💤 :%d stopped listening", down[0])
	case len(down) > 1:
		return "💤 " + ports.Format(down) + " stopped listening"
	}
	return ""
}

func (m Model) startAdd() (tea.Model, tea.Cmd) {
	m.form = m.newAddForm()
	m.screen = screenAdd
	return m, m.form.Init()
}

func (m Model) confirm(act action) (tea.Model, tea.Cmd) {
	m.pending, m.screen = act, screenConfirm
	return m, nil
}

func (m Model) listener(port int) *netinfo.Listener {
	for i := range m.snap.Listeners {
		if m.snap.Listeners[i].Port == port {
			return &m.snap.Listeners[i]
		}
	}
	return nil
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
	case key.Matches(msg, m.keys.Add):
		return m.startAdd()
	case key.Matches(msg, m.keys.Share):
		if len(m.snap.Entries) == 0 {
			return m, nil
		}
		m.qrPort, m.qrHost, m.screen = m.snap.Entries[m.cursor].Port, 0, screenQR
		return m, nil
	case key.Matches(msg, m.keys.Copy):
		if len(m.snap.Entries) == 0 || len(m.hosts()) == 0 {
			return m, nil
		}
		return m.copyURL(m.shareURL(m.snap.Entries[m.cursor].Port))
	case key.Matches(msg, m.keys.Remove):
		if len(m.snap.Entries) == 0 {
			return m, m.showToast(toastInfo, "Nothing to remove yet")
		}
		return m.confirm(action{kind: actRemove, ports: []int{m.snap.Entries[m.cursor].Port}})
	case key.Matches(msg, m.keys.RemoveAll):
		var managed []int
		for _, e := range m.snap.Entries {
			if e.Managed {
				managed = append(managed, e.Port)
			}
		}
		if len(managed) == 0 {
			return m, m.showToast(toastInfo, "No serve-a-port forwards to remove")
		}
		return m.confirm(action{kind: actRemove, ports: managed})
	case key.Matches(msg, m.keys.Repair):
		broken := forward.RepairPorts(m.snap)
		if len(broken) == 0 {
			return m, m.showToast(toastSuccess, "✨ Everything's healthy, nothing to repair")
		}
		return m.confirm(action{kind: actRepair, ports: broken})
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
	case screenAdd:
		content = m.page(m.form.View() + "\n\n" + m.keyHint("esc", "cancel"))
	case screenConfirm:
		content = m.confirmView()
	case screenApplying:
		content = m.applyingView()
	case screenResult:
		content = m.resultView()
	case screenQR:
		content = m.qrView()
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
