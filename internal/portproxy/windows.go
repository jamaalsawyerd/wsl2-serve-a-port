package portproxy

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/winexec"
)

// serviceState matches the state line of `sc.exe query`, e.g.
// "STATE : 4  RUNNING". The label may be localized; the state names aren't.
var serviceState = regexp.MustCompile(`:\s*\d+\s+(RUNNING|STOPPED|START_PENDING|STOP_PENDING|PAUSED|PAUSE_PENDING|CONTINUE_PENDING)\b`)

// IPHelperRunning reports whether the Windows IP Helper service (iphlpsvc)
// is running. portproxy rules silently pass no traffic without it. sc.exe
// answers in milliseconds, unlike PowerShell.
func IPHelperRunning(ctx context.Context, r winexec.Runner, sc string) (bool, error) {
	out, err := r.Run(ctx, sc, "query", "iphlpsvc")
	if err != nil {
		return false, fmt.Errorf("checking the IP Helper service: %w", err)
	}
	return parseServiceRunning(winexec.Clean(out))
}

func parseServiceRunning(out string) (bool, error) {
	m := serviceState.FindStringSubmatch(out)
	if m == nil {
		return false, fmt.Errorf("unexpected sc.exe output: %q", out)
	}
	return m[1] == "RUNNING", nil
}

// Hint explains a Windows error message from the elevated script in plain
// words, or returns "" when there's nothing useful to add.
func Hint(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "access is denied") || strings.Contains(lower, "0x80070005"):
		return "Windows refused the change. A Group Policy may block firewall edits on this PC."
	case strings.Contains(lower, "hypervrule") && strings.Contains(lower, "not recognized"):
		return "This Windows build lacks Hyper-V firewall support; update Windows 11 and WSL to use mirrored mode."
	case strings.Contains(lower, "requires elevation"):
		return "The script didn't get administrator rights."
	case strings.Contains(lower, "group policy") || strings.Contains(lower, "gpo"):
		return "A Group Policy is overriding local firewall rules."
	}
	return ""
}
