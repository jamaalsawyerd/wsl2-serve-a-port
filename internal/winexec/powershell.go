package winexec

import (
	"context"
	"strings"
)

// PowerShell runs Windows PowerShell scripts.
type PowerShell struct {
	Exe    string
	Runner Runner
}

// utf8Prelude makes PowerShell write UTF-8 so non-ASCII adapter names survive
// the trip back into WSL.
const utf8Prelude = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n"

// Command runs script without elevation and returns its cleaned stdout.
func (p PowerShell) Command(ctx context.Context, script string) (string, error) {
	out, err := p.Runner.Run(ctx, p.Exe,
		"-NoProfile", "-NonInteractive",
		"-EncodedCommand", EncodeCommand(utf8Prelude+script))
	return Clean(out), err
}

// IsAdmin reports whether PowerShell is already running elevated, which
// happens when WSL itself was launched from an admin terminal.
func (p PowerShell) IsAdmin(ctx context.Context) (bool, error) {
	out, err := p.Command(ctx, "([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent())"+
		".IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)")
	if err != nil {
		return false, err
	}
	return strings.EqualFold(out, "True"), nil
}

// TempDir returns the Windows user's temp directory, e.g.
// C:\Users\me\AppData\Local\Temp.
func (p PowerShell) TempDir(ctx context.Context) (string, error) {
	return p.Command(ctx, "[System.IO.Path]::GetTempPath().TrimEnd('\\')")
}
