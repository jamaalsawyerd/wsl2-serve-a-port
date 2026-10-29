package winexec

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestPowerShellLive exercises the prelude against real PowerShell when
// running under WSL.
func TestPowerShellLive(t *testing.T) {
	exe, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("powershell.exe not available")
	}
	ps := PowerShell{Exe: exe, Runner: ExecRunner{}}
	ctx := context.Background()

	out, err := ps.Command(ctx, "Get-Item 'C:\\definitely-missing' -ErrorAction SilentlyContinue; 'héllo'")
	if err != nil || out != "héllo" {
		t.Errorf("quiet failure: out=%q err=%v, want héllo and no error", out, err)
	}

	_, err = ps.Command(ctx, "throw 'kaboom'")
	exitErr, ok := err.(*ExitError)
	if !ok || exitErr.Code != 1 || exitErr.Stderr != "kaboom" {
		t.Errorf("throw: err = %#v, want exit 1 with plain message", err)
	}

	tmp, err := ps.TempDir(ctx)
	if err != nil || !strings.Contains(tmp, `\`) {
		t.Errorf("TempDir = %q, %v", tmp, err)
	}
}
