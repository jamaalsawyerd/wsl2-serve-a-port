package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"rsc.io/qr"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/netinfo"
)

func TestRenderQRGeometry(t *testing.T) {
	url := "http://192.168.1.50:3000"
	out, err := renderQR(url)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := qr.Encode(url, qr.M)
	side := code.Size + 2*qrQuiet
	lines := strings.Split(ansi.Strip(out), "\n")
	if len(lines) != (side+1)/2 {
		t.Errorf("got %d rows, want %d", len(lines), (side+1)/2)
	}
	for i, l := range lines {
		if l != strings.Repeat("▀", side) {
			t.Fatalf("row %d isn't %d half-blocks: %q", i, side, l)
		}
	}
	if w, h, _ := qrSize(url); w != side || h != len(lines) {
		t.Errorf("qrSize = %dx%d, want %dx%d", w, h, side, len(lines))
	}
}

func TestQRScreen(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := send(t, dashboard(t, svc), press("enter"))
	if m.screen != screenQR {
		t.Fatalf("enter didn't open the QR screen")
	}
	out := plain(m)
	for _, want := range []string{"Scan to open :3000", "▀▀▀", "http://192.168.1.50:3000", "via Wi-Fi  (1/2)", "● live · node", "←/→ network"} {
		if !strings.Contains(out, want) {
			t.Errorf("QR screen missing %q:\n%s", want, out)
		}
	}

	m = send(t, m, press("l"))
	if out := plain(m); !strings.Contains(out, "http://172.22.184.45:3000") || !strings.Contains(out, "(2/2)") {
		t.Errorf("didn't switch network:\n%s", out)
	}
	m = send(t, m, press("l"))
	if m.qrHost != 0 {
		t.Errorf("host index didn't wrap: %d", m.qrHost)
	}

	m = send(t, m, press("c"))
	if !strings.Contains(plain(m), "📋 Copied http://192.168.1.50:3000") {
		t.Errorf("no copy toast:\n%s", plain(m))
	}
	m = send(t, m, press("esc"))
	if m.screen != screenDashboard {
		t.Error("esc didn't close the QR screen")
	}
}

func TestQRScreenTooSmall(t *testing.T) {
	m := boot(t, newModel(goodReport, &fakeService{snap: sampleSnapshot()}, 80, 20))
	m = send(t, m, press("enter"))
	out := plain(m)
	if !strings.Contains(out, "Make the terminal bigger") || !strings.Contains(out, "http://192.168.1.50:3000") {
		t.Errorf("small terminal should still show the URL:\n%s", out)
	}
}

func TestCopyFromDashboard(t *testing.T) {
	m := send(t, dashboard(t, &fakeService{snap: sampleSnapshot()}), press("down"), press("c"))
	if !strings.Contains(plain(m), "📋 Copied http://192.168.1.50:4000") {
		t.Errorf("no copy toast:\n%s", plain(m))
	}
}

func withStatus(s forward.Snapshot, port int, st forward.Status) forward.Snapshot {
	s.Entries = slices.Clone(s.Entries)
	for i := range s.Entries {
		if s.Entries[i].Port == port {
			s.Entries[i].Status = st
		}
	}
	return s
}

func TestLiveChanges(t *testing.T) {
	base := sampleSnapshot()
	tests := []struct {
		name string
		cur  forward.Snapshot
		want string
	}{
		{"nothing", base, ""},
		{"one up", withStatus(base, 4000, forward.StatusLive), "✨ :4000 is live!"},
		{"two up", withStatus(withStatus(base, 4000, forward.StatusLive), 5000, forward.StatusLive), "✨ 4000, 5000 are live!"},
		{"down", withStatus(base, 3000, forward.StatusIdle), "💤 :3000 stopped listening"},
		{"stale wins", withStatus(withStatus(base, 4000, forward.StatusLive), 3000, forward.StatusStale), "↻ The WSL IP changed. Press r to repair."},
		{"external ignored", withStatus(base, 8081, forward.StatusLive), ""},
	}
	for _, tt := range tests {
		if got := liveChanges(base, tt.cur); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRelistenUpdatesDashboard(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	svc.relisten = func(s forward.Snapshot) forward.Snapshot {
		s = withStatus(s, 4000, forward.StatusLive)
		s.Entries[1].Listener = &netinfo.Listener{Port: 4000, Reachable: true, Process: "python3"}
		return s
	}
	m := dashboard(t, svc)

	// A result computed against an older snapshot is dropped.
	old := m.relisten()().(liveMsg)
	old.gen--
	if m2 := send(t, m, old); strings.Contains(plain(m2), "python3") {
		t.Error("stale relisten result was applied")
	}

	m = send(t, m, relistenTickMsg{})
	out := plain(m)
	if !strings.Contains(out, "✨ :4000 is live!") || !strings.Contains(out, "python3") {
		t.Errorf("relisten didn't update the dashboard:\n%s", out)
	}
}

func TestCelebration(t *testing.T) {
	svc := &fakeService{snap: noCandidates()}
	m := send(t, dashboard(t, svc), press("a"))
	m = send(t, m, append(typed("8000"), press("enter"), press("enter"))...)
	if !m.party || !m.celebrate.active() {
		t.Fatal("successful add should celebrate")
	}
	if lines := strings.Count(plain(m), "\n"); lines == 0 {
		t.Fatal("empty result view")
	}

	c, frames := m.celebrate, 0
	for running := true; running; frames++ {
		c, running = c.step()
		if frames > celebrateMaxFrames+1 {
			t.Fatal("celebration never ended")
		}
	}
	if c.active() || c.titleOffset() != 0 {
		t.Errorf("celebration didn't reset: %+v", c)
	}

	m = send(t, m, press("enter"), press("d"), press("enter"))
	if m.party {
		t.Error("removing ports shouldn't celebrate")
	}
}
