// Package wsl detects whether the process is running inside WSL2 and gathers
// the facts the rest of the app needs about the environment.
package wsl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// NetworkingMode is the WSL networking mode reported by wslinfo.
type NetworkingMode string

const (
	ModeNAT      NetworkingMode = "nat"
	ModeMirrored NetworkingMode = "mirrored"
	ModeUnknown  NetworkingMode = "unknown"
)

// Default Windows locations, used when the .exe files aren't on PATH
// (for example when interop's appendWindowsPath is disabled).
const (
	defaultPowerShell = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
	defaultNetsh      = "/mnt/c/Windows/System32/netsh.exe"
)

// ErrNotWSL2 is returned by Report.Err when a fatal check failed.
var ErrNotWSL2 = errors.New("not running inside WSL2")

// Check is one line of the environment checklist.
type Check struct {
	Name   string
	OK     bool
	Detail string
	// Fatal checks must pass for the app to work at all.
	Fatal bool
}

// Report is the result of Detect.
type Report struct {
	Checks         []Check
	Distro         string
	Kernel         string
	NetworkingMode NetworkingMode
	PowerShell     string
	Netsh          string
}

// Err returns ErrNotWSL2 (wrapped with the first failing fatal check) if the
// environment can't be used, or nil.
func (r Report) Err() error {
	for _, c := range r.Checks {
		if c.Fatal && !c.OK {
			return fmt.Errorf("%w: %s: %s", ErrNotWSL2, c.Name, c.Detail)
		}
	}
	return nil
}

// Detector holds the OS hooks used by Detect so tests can fake them.
type Detector struct {
	ReadFile func(string) ([]byte, error)
	Getenv   func(string) string
	Glob     func(string) ([]string, error)
	LookPath func(string) (string, error)
	Stat     func(string) (os.FileInfo, error)
	// Output runs a command and returns its stdout.
	Output func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewDetector returns a Detector wired to the real OS.
func NewDetector() *Detector {
	return &Detector{
		ReadFile: os.ReadFile,
		Getenv:   os.Getenv,
		Glob:     filepath.Glob,
		LookPath: exec.LookPath,
		Stat:     os.Stat,
		Output: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
	}
}

// Detect runs every environment check. It never returns early so the UI can
// show the full checklist even when something fails.
func (d *Detector) Detect(ctx context.Context) Report {
	var r Report
	r.Distro = d.Getenv("WSL_DISTRO_NAME")

	kernel := ""
	if b, err := d.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		kernel = strings.TrimSpace(string(b))
	}
	r.Kernel = kernel
	r.Checks = append(r.Checks, d.checkWSL2(kernel))
	r.Checks = append(r.Checks, d.checkInterop())

	r.PowerShell = d.findExe("powershell.exe", defaultPowerShell)
	r.Checks = append(r.Checks, exeCheck("PowerShell", r.PowerShell))
	r.Netsh = d.findExe("netsh.exe", defaultNetsh)
	r.Checks = append(r.Checks, exeCheck("netsh", r.Netsh))

	r.NetworkingMode = d.networkingMode(ctx)
	modeCheck := Check{Name: "Networking mode", OK: true, Detail: string(r.NetworkingMode)}
	if r.NetworkingMode == ModeUnknown {
		modeCheck.Detail = "unknown (assuming NAT)"
	}
	r.Checks = append(r.Checks, modeCheck)

	return r
}

func (d *Detector) checkWSL2(kernel string) Check {
	c := Check{Name: "WSL2 kernel", Fatal: true}
	lower := strings.ToLower(kernel)
	switch {
	case strings.Contains(lower, "microsoft-standard") || strings.Contains(lower, "wsl2"):
		c.OK, c.Detail = true, kernel
	case strings.Contains(lower, "microsoft"):
		// WSL1 kernels report e.g. "4.4.0-19041-Microsoft".
		c.Detail = "this looks like WSL1; run `wsl --set-version <distro> 2`"
	case d.Getenv("WSL_INTEROP") != "":
		// Custom kernels drop the "microsoft" suffix, but WSL_INTEROP is
		// only set by WSL2.
		c.OK, c.Detail = true, kernel+" (custom kernel)"
	default:
		c.Detail = "kernel " + kernel + " isn't a WSL kernel"
	}
	return c
}

func (d *Detector) checkInterop() Check {
	c := Check{Name: "Windows interop", Fatal: true}
	matches, _ := d.Glob("/proc/sys/fs/binfmt_misc/WSLInterop*")
	if len(matches) > 0 {
		c.OK, c.Detail = true, "enabled"
		return c
	}
	c.Detail = "disabled; enable [interop] in /etc/wsl.conf"
	return c
}

func (d *Detector) findExe(name, fallback string) string {
	if p, err := d.LookPath(name); err == nil {
		return p
	}
	if _, err := d.Stat(fallback); err == nil {
		return fallback
	}
	return ""
}

func exeCheck(name, path string) Check {
	c := Check{Name: name, Fatal: true, OK: path != "", Detail: path}
	if path == "" {
		c.Detail = "not found on PATH or under /mnt/c"
	}
	return c
}

func (d *Detector) networkingMode(ctx context.Context) NetworkingMode {
	out, err := d.Output(ctx, "wslinfo", "--networking-mode")
	if err != nil {
		return ModeUnknown
	}
	switch NetworkingMode(strings.ToLower(strings.TrimSpace(string(out)))) {
	case ModeNAT:
		return ModeNAT
	case ModeMirrored:
		return ModeMirrored
	default:
		return ModeUnknown
	}
}
