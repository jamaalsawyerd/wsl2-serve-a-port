package winexec

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"
)

func decode(t *testing.T, enc string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

func TestEncodeCommandRoundTrip(t *testing.T) {
	in := "Write-Output 'héllo \"wörld\"' 🚀"
	if got := decode(t, EncodeCommand(in)); got != in {
		t.Errorf("round trip = %q, want %q", got, in)
	}
}

func TestQuote(t *testing.T) {
	if got := Quote(`it's C:\x`); got != `'it''s C:\x'` {
		t.Errorf("Quote = %s", got)
	}
}

func TestClean(t *testing.T) {
	if got := Clean([]byte("\xEF\xBB\xBF  hi\r\nthere\r\n")); got != "hi\nthere" {
		t.Errorf("Clean = %q", got)
	}
}

// fakePS answers the scripts Elevator sends to PowerShell.
type fakePS struct {
	t       *testing.T
	admin   bool
	tempDir string // WSL side of C:\Temp
	launch  func(script string) ([]byte, error)
	scripts []string
}

func (f *fakePS) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "wslpath" {
		return []byte(f.tempDir + "\n"), nil
	}
	script := decode(f.t, args[len(args)-1])
	f.scripts = append(f.scripts, script)
	switch {
	case strings.Contains(script, "IsInRole"):
		if f.admin {
			return []byte("True\r\n"), nil
		}
		return []byte("False\r\n"), nil
	case strings.Contains(script, "GetTempPath"):
		return []byte(`C:\Temp` + "\r\n"), nil
	default:
		return f.launch(script)
	}
}

var resultArg = regexp.MustCompile(`'-ResultPath','"C:\\Temp\\([^"]+)"'`)

func TestRunScriptSuccess(t *testing.T) {
	dir := t.TempDir()
	f := &fakePS{t: t, tempDir: dir}
	f.launch = func(script string) ([]byte, error) {
		if !strings.Contains(script, "-Verb RunAs") {
			t.Errorf("non-admin launcher should use RunAs:\n%s", script)
		}
		m := resultArg.FindStringSubmatch(script)
		if m == nil {
			t.Fatalf("no -ResultPath in launcher:\n%s", script)
		}
		ps1 := filepath.Join(dir, strings.TrimSuffix(m[1], ".json")+".ps1")
		body, err := os.ReadFile(ps1)
		if err != nil || !strings.HasSuffix(string(body), "the-script") {
			t.Errorf("script file %s = %q, %v", ps1, body, err)
		}
		return nil, os.WriteFile(filepath.Join(dir, m[1]), []byte("\xEF\xBB\xBF{\"ok\":true}\r\n"), 0o600)
	}
	e := NewElevator(PowerShell{Exe: "powershell.exe", Runner: f})

	out, err := e.RunScript(context.Background(), "the-script")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("result = %q", out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestRunScriptAdminSkipsUAC(t *testing.T) {
	dir := t.TempDir()
	f := &fakePS{t: t, admin: true, tempDir: dir}
	f.launch = func(script string) ([]byte, error) {
		if strings.Contains(script, "RunAs") {
			t.Errorf("admin launcher should not prompt:\n%s", script)
		}
		m := resultArg.FindStringSubmatch(script)
		return nil, os.WriteFile(filepath.Join(dir, m[1]), []byte("{}"), 0o600)
	}
	e := NewElevator(PowerShell{Exe: "powershell.exe", Runner: f})
	if _, err := e.RunScript(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
}

func TestRunScriptDeclined(t *testing.T) {
	f := &fakePS{t: t, tempDir: t.TempDir()}
	f.launch = func(string) ([]byte, error) {
		return nil, &ExitError{Name: "powershell.exe", Code: 1223}
	}
	e := NewElevator(PowerShell{Exe: "powershell.exe", Runner: f})
	if _, err := e.RunScript(context.Background(), "x"); !errors.Is(err, ErrElevationDeclined) {
		t.Errorf("err = %v, want ErrElevationDeclined", err)
	}
}

func TestRunScriptNoResult(t *testing.T) {
	f := &fakePS{t: t, tempDir: t.TempDir()}
	f.launch = func(string) ([]byte, error) { return nil, nil }
	e := NewElevator(PowerShell{Exe: "powershell.exe", Runner: f})
	if _, err := e.RunScript(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "no results") {
		t.Errorf("err = %v, want missing-results error", err)
	}
}
