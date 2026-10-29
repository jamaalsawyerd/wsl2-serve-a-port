package tui

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/netinfo"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/portproxy"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/wsl"
)

var (
	goodReport = wsl.Report{
		Distro: "Ubuntu",
		Checks: []wsl.Check{
			{Name: "WSL2 kernel", OK: true, Fatal: true, Detail: "6.18-microsoft-standard-WSL2"},
			{Name: "Windows interop", OK: true, Fatal: true, Detail: "enabled"},
		},
	}
	badReport = wsl.Report{
		Checks: []wsl.Check{
			{Name: "WSL2 kernel", OK: false, Fatal: true, Detail: "kernel 6.8.0-generic isn't a WSL kernel"},
			{Name: "Windows interop", OK: false, Fatal: true, Detail: "disabled"},
		},
	}
)

func rule(port int, ip string) *portproxy.Rule {
	return &portproxy.Rule{ListenAddress: "0.0.0.0", ListenPort: port, ConnectAddress: ip, ConnectPort: port}
}

// sampleSnapshot has one entry per status.
func sampleSnapshot() forward.Snapshot {
	wslIP := "172.17.213.47"
	node := &netinfo.Listener{Port: 3000, Reachable: true, Process: "node"}
	return forward.Snapshot{
		WSL: netinfo.WSLAddr{Interface: "eth0", IP: netip.MustParseAddr(wslIP)},
		Windows: []netinfo.WindowsAddr{
			{Alias: "Wi-Fi", IP: netip.MustParseAddr("192.168.1.50"), Hardware: true},
			{Alias: "ZeroTier One [0123456789abcdef]", IP: netip.MustParseAddr("172.22.184.45")},
		},
		Entries: []forward.Entry{
			{Port: 3000, Managed: true, Firewall: true, Rule: rule(3000, wslIP), Listener: node, Status: forward.StatusLive},
			{Port: 4000, Managed: true, Firewall: true, Rule: rule(4000, wslIP), Status: forward.StatusIdle},
			{Port: 5000, Managed: true, Firewall: true, Rule: rule(5000, wslIP), Listener: &netinfo.Listener{Port: 5000}, Status: forward.StatusLocalOnly},
			{Port: 5173, Managed: true, Firewall: true, Rule: rule(5173, "172.17.200.9"), Status: forward.StatusStale},
			{Port: 6000, Managed: true, Status: forward.StatusMissing},
			{Port: 8081, Rule: rule(8081, wslIP), Status: forward.StatusExternal},
		},
		Listeners: []netinfo.Listener{*node, {Port: 9229, Reachable: true, Process: "vite"}},
		Warnings:  []string{"listing firewall rules: access denied"},
		TakenAt:   time.Date(2026, 10, 28, 14, 2, 31, 0, time.UTC),
	}
}

type fakeService struct {
	snap  forward.Snapshot
	err   error
	calls int
}

func (f *fakeService) Snapshot(context.Context) (forward.Snapshot, error) {
	f.calls++
	return f.snap, f.err
}

func newModel(report wsl.Report, svc *fakeService, w, h int) Model {
	m := New(context.Background(), Deps{
		Detect:  func(context.Context) wsl.Report { return report },
		Connect: func(wsl.Report) (Service, error) { return svc, nil },
	})
	return step(m, tea.WindowSizeMsg{Width: w, Height: h})
}

// step applies msg and returns the updated model, discarding any command.
func step(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

// boot drives the model from start to the dashboard (or as far as it gets).
func boot(t *testing.T, m Model) Model {
	t.Helper()
	m = step(m, m.detect()())
	var cmd tea.Cmd
	for range m.report.Checks {
		var next tea.Model
		next, cmd = m.Update(revealMsg{})
		m = next.(Model)
	}
	if m.fatal != nil {
		return m
	}
	m = step(m, cmd())
	if m.gatherErr != nil {
		return m
	}
	return step(m, enterDashboardMsg{})
}

func plain(m Model) string { return ansi.Strip(m.View().Content) }

func press(s string) tea.KeyPressMsg {
	if s == "down" {
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func TestBootToDashboard(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := boot(t, newModel(goodReport, svc, 110, 40))
	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want dashboard", m.screen)
	}
	v := m.View()
	if !v.AltScreen || v.WindowTitle != "serve-a-port" {
		t.Errorf("view flags: alt=%v title=%q", v.AltScreen, v.WindowTitle)
	}
	out := plain(m)
	for _, want := range []string{
		"172.17.213.47", "eth0 · Ubuntu", "192.168.1.50", "Wi-Fi", "+ 172.22.184.45",
		"NAT mode · 6 forwards · updated 14:02:31",
		"● live", "○ idle", "◐ localhost", "↻ stale", "✗ missing", "◆ external",
		"http://192.168.1.50:3000", "node",
		"✨ Live!", // detail for the selected (first) row
		"💡 vite is listening on :9229",
		"⚠ listing firewall rules",
		"q quit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "node is listening") {
		t.Error("hint shown for a port that's already forwarded")
	}
}

func TestDashboardNavigation(t *testing.T) {
	m := boot(t, newModel(goodReport, &fakeService{snap: sampleSnapshot()}, 110, 40))
	for range 3 {
		m = step(m, press("down"))
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d, want 3", m.cursor)
	}
	if out := plain(m); !strings.Contains(out, "old WSL IP (172.17.200.9)") || !strings.Contains(out, "▸ 5173") {
		t.Errorf("stale row not selected/explained:\n%s", out)
	}
	for range 10 {
		m = step(m, press("j"))
	}
	if m.cursor != 5 {
		t.Errorf("cursor = %d, want clamped to 5", m.cursor)
	}
	if out := plain(m); !strings.Contains(out, "Created outside serve-a-port, forwarding to 172.17.213.47:8081") {
		t.Errorf("external detail missing:\n%s", out)
	}
}

func TestDashboardScrollsWhenShort(t *testing.T) {
	m := boot(t, newModel(goodReport, &fakeService{snap: sampleSnapshot()}, 110, 24))
	for range 5 {
		m = step(m, press("down"))
	}
	out := plain(m)
	if !strings.Contains(out, "▸ 8081") {
		t.Errorf("selected row scrolled out of view:\n%s", out)
	}
	if !strings.Contains(out, "of 6") {
		t.Errorf("no scroll indicator:\n%s", out)
	}
}

func TestRefreshAndToast(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := boot(t, newModel(goodReport, svc, 110, 40))

	next, cmd := m.Update(press("R"))
	m = next.(Model)
	if !m.refreshing || cmd == nil {
		t.Fatal("R didn't start a refresh")
	}
	svc.err = errors.New("netsh exploded")
	m = step(m, cmd())
	if m.refreshing || !strings.Contains(plain(m), "✗ netsh exploded") {
		t.Errorf("refresh error not shown:\n%s", plain(m))
	}
	if m.screen != screenDashboard {
		t.Error("refresh error kicked us off the dashboard")
	}

	m = step(m, press("a"))
	if !strings.Contains(plain(m), "next phase") {
		t.Error("placeholder toast missing for add")
	}
	m = step(m, toastExpiredMsg{id: m.toastID - 1})
	if m.toast == "" {
		t.Error("stale toast expiry cleared the current toast")
	}
	m = step(m, toastExpiredMsg{id: m.toastID})
	if m.toast != "" {
		t.Error("toast didn't expire")
	}
}

func TestBootFatal(t *testing.T) {
	svc := &fakeService{}
	m := boot(t, newModel(badReport, svc, 100, 30))
	if m.fatal == nil || m.screen != screenBoot {
		t.Fatal("expected to stay on boot with a fatal error")
	}
	out := plain(m)
	if !strings.Contains(out, "serve-a-port needs WSL2") || !strings.Contains(out, "isn't a WSL kernel") {
		t.Errorf("fatal box missing details:\n%s", out)
	}
	if svc.calls != 0 {
		t.Error("snapshot taken despite failed checks")
	}
}

func TestBootSnapshotErrorRetry(t *testing.T) {
	svc := &fakeService{err: errors.New("no eth0")}
	m := boot(t, newModel(goodReport, svc, 100, 30))
	if m.gatherErr == nil || !strings.Contains(plain(m), "press r to retry") {
		t.Fatalf("expected retry prompt:\n%s", plain(m))
	}
	svc.err, svc.snap = nil, sampleSnapshot()
	next, cmd := m.Update(press("r"))
	m = step(next.(Model), cmd())
	if !m.bootDone {
		t.Error("retry didn't finish boot")
	}
}

func TestNarrowTerminal(t *testing.T) {
	for _, width := range []int{36, 50, 64} {
		m := boot(t, newModel(goodReport, &fakeService{snap: sampleSnapshot()}, width, 40))
		out := plain(m)
		for i, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: line %d is %d wide: %q", width, i, w, line)
			}
		}
		if !strings.Contains(out, "▸ 3000") {
			t.Errorf("width %d: selected row missing:\n%s", width, out)
		}
	}
	m := boot(t, newModel(goodReport, &fakeService{snap: sampleSnapshot()}, 36, 40))
	if out := plain(m); strings.Contains(out, "╔═╗") || !strings.Contains(out, "serve-a-port") {
		t.Errorf("36 columns should use the compact title:\n%s", out)
	}
}

func TestEmptyState(t *testing.T) {
	snap := sampleSnapshot()
	snap.Entries = nil
	m := boot(t, newModel(goodReport, &fakeService{snap: snap}, 100, 30))
	if out := plain(m); !strings.Contains(out, "No ports forwarded yet") || !strings.Contains(out, "node is listening on :3000") {
		t.Errorf("empty state:\n%s", out)
	}
}

func TestLightTheme(t *testing.T) {
	m := newModel(goodReport, &fakeService{}, 80, 24)
	m = step(m, tea.BackgroundColorMsg{Color: lightBG{}})
	if m.theme.IsDark {
		t.Error("theme didn't switch to light")
	}
}

type lightBG struct{}

func (lightBG) RGBA() (r, g, b, a uint32) { return 0xffff, 0xffff, 0xffff, 0xffff }
