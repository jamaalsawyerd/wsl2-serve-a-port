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

// prelude makes scripts behave predictably when run from WSL:
//   - UTF-8 output, so non-ASCII adapter names survive the trip back;
//   - no progress records, which otherwise arrive on stderr as CLIXML;
//   - terminating errors print a plain message and exit 1, and anything
//     else exits 0, even if the last command "failed" quietly under
//     -ErrorAction SilentlyContinue.
const prelude = `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$ProgressPreference = 'SilentlyContinue'
$ErrorActionPreference = 'Stop'
trap { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }
`

// Command runs script without elevation and returns its cleaned stdout.
func (p PowerShell) Command(ctx context.Context, script string) (string, error) {
	out, err := p.Runner.Run(ctx, p.Exe,
		"-NoProfile", "-NonInteractive",
		"-EncodedCommand", EncodeCommand(prelude+script+"\nexit 0\n"))
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
