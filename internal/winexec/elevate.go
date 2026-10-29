package winexec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// ErrElevationDeclined means the user said no to the UAC prompt.
var ErrElevationDeclined = errors.New("administrator permission was declined")

// exitCancelled is ERROR_CANCELLED, which the launcher uses to signal a
// declined UAC prompt.
const exitCancelled = 1223

// Elevator runs PowerShell scripts as administrator with a single UAC prompt.
//
// An elevated process can't hand its stdout back to us, so the script is
// written to the Windows temp directory and receives a -ResultPath argument;
// the script writes its results there and RunScript reads them back.
type Elevator struct {
	PS PowerShell
	// ToWSLPath converts a Windows path to a WSL path. Defaults to wslpath -u.
	ToWSLPath func(ctx context.Context, winPath string) (string, error)

	once    sync.Once
	initErr error
	isAdmin bool
	winTemp string
	wslTemp string
}

// NewElevator returns an Elevator using the given PowerShell.
func NewElevator(ps PowerShell) *Elevator {
	e := &Elevator{PS: ps}
	e.ToWSLPath = func(ctx context.Context, p string) (string, error) {
		out, err := ps.Runner.Run(ctx, "wslpath", "-u", p)
		return Clean(out), err
	}
	return e
}

func (e *Elevator) init(ctx context.Context) error {
	e.once.Do(func() {
		if e.isAdmin, e.initErr = e.PS.IsAdmin(ctx); e.initErr != nil {
			e.initErr = fmt.Errorf("checking admin status: %w", e.initErr)
			return
		}
		if e.winTemp, e.initErr = e.PS.TempDir(ctx); e.initErr != nil {
			e.initErr = fmt.Errorf("finding Windows temp dir: %w", e.initErr)
			return
		}
		if e.wslTemp, e.initErr = e.ToWSLPath(ctx, e.winTemp); e.initErr != nil {
			e.initErr = fmt.Errorf("converting %s to a WSL path: %w", e.winTemp, e.initErr)
		}
	})
	return e.initErr
}

// IsAdmin reports whether scripts will run without a UAC prompt.
func (e *Elevator) IsAdmin(ctx context.Context) (bool, error) {
	if err := e.init(ctx); err != nil {
		return false, err
	}
	return e.isAdmin, nil
}

// RunScript runs script elevated and returns whatever it wrote to
// $ResultPath. The script must start with param([string]$ResultPath).
func (e *Elevator) RunScript(ctx context.Context, script string) ([]byte, error) {
	if err := e.init(ctx); err != nil {
		return nil, err
	}

	id, err := randomID()
	if err != nil {
		return nil, err
	}
	base := "serve-a-port-" + id
	winScript, winResult := e.winTemp+`\`+base+".ps1", e.winTemp+`\`+base+".json"
	wslScript, wslResult := e.wslTemp+"/"+base+".ps1", e.wslTemp+"/"+base+".json"

	// The BOM makes Windows PowerShell 5.1 read the file as UTF-8.
	if err := os.WriteFile(wslScript, append([]byte("\xEF\xBB\xBF"), script...), 0o600); err != nil {
		return nil, fmt.Errorf("writing script: %w", err)
	}
	defer os.Remove(wslScript)
	defer os.Remove(wslResult)

	if _, err := e.PS.Command(ctx, e.launcher(winScript, winResult)); err != nil {
		if exitErr, ok := errors.AsType[*ExitError](err); ok && exitErr.Code == exitCancelled {
			return nil, ErrElevationDeclined
		}
		return nil, fmt.Errorf("running elevated script: %w", err)
	}

	out, err := os.ReadFile(wslResult)
	if err != nil {
		return nil, fmt.Errorf("elevated script left no results: %w", err)
	}
	return []byte(Clean(out)), nil
}

// launcher returns the non-elevated PowerShell that starts the script, either
// directly (already admin) or through a UAC prompt.
func (e *Elevator) launcher(winScript, winResult string) string {
	args := fmt.Sprintf("@('-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',%s,'-ResultPath',%s)",
		Quote(`"`+winScript+`"`), Quote(`"`+winResult+`"`))
	if e.isAdmin {
		return fmt.Sprintf("$p = Start-Process -FilePath 'powershell.exe' -Wait -PassThru -NoNewWindow -ArgumentList %s\nexit $p.ExitCode", args)
	}
	return strings.Join([]string{
		"try {",
		"  $p = Start-Process -FilePath 'powershell.exe' -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList " + args,
		"  exit $p.ExitCode",
		"} catch {",
		"  $ex = $_.Exception",
		"  while ($ex) {",
		"    if ($ex -is [System.ComponentModel.Win32Exception] -and $ex.NativeErrorCode -eq 1223) { exit 1223 }",
		"    $ex = $ex.InnerException",
		"  }",
		"  if ($_.Exception.Message -match 'cancel') { exit 1223 }",
		"  [Console]::Error.WriteLine($_.Exception.Message)",
		"  exit 1",
		"}",
	}, "\n")
}

func randomID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
