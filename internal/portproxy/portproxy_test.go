package portproxy

import (
	"context"
	"flag"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/winexec"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestParseRules(t *testing.T) {
	out := "\r\nListen on ipv4:             Connect to ipv4:\r\n\r\n" +
		"Address         Port        Address         Port\r\n" +
		"--------------- ----------  --------------- ----------\r\n" +
		"0.0.0.0         8081        172.17.213.47   8081\r\n" +
		"0.0.0.0         3000        172.17.200.1    3001\r\n\r\n"
	got := ParseRules(winexec.Clean([]byte(out)))
	want := []Rule{
		{"0.0.0.0", 8081, "172.17.213.47", 8081},
		{"0.0.0.0", 3000, "172.17.200.1", 3001},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got := ParseRules(""); len(got) != 0 {
		t.Errorf("empty output gave %+v", got)
	}
}

func TestParseServiceRunning(t *testing.T) {
	running := "SERVICE_NAME: iphlpsvc\n        TYPE               : 30  WIN32\n        STATE              : 4  RUNNING\n                                (STOPPABLE, NOT_PAUSABLE, IGNORES_SHUTDOWN)\n"
	if ok, err := parseServiceRunning(running); !ok || err != nil {
		t.Errorf("running: %v, %v", ok, err)
	}
	stopped := "SERVICE_NAME: iphlpsvc\n        TYP                : 30  WIN32\n        STATUS             : 1  STOPPED\n"
	if ok, err := parseServiceRunning(stopped); ok || err != nil {
		t.Errorf("stopped (localized label): %v, %v", ok, err)
	}
	if _, err := parseServiceRunning("[SC] OpenService FAILED 1060"); err == nil {
		t.Error("expected error for unknown output")
	}
}

func TestHint(t *testing.T) {
	for msg, want := range map[string]string{
		"Access is denied.": "Group Policy",
		"The term 'New-NetFirewallHyperVRule' is not recognized as the name of a cmdlet": "Hyper-V firewall",
		"The requested operation requires elevation.":                                    "administrator",
		"The process cannot access the file":                                             "",
	} {
		if got := Hint(msg); (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("Hint(%q) = %q, want it to mention %q", msg, got, want)
		}
	}
}

func TestParseFirewallPorts(t *testing.T) {
	out := "wsl2-serve-a-port-tcp-3000\nsomething-else\nwsl2-serve-a-port-tcp-x\nwsl2-serve-a-port-tcp-5173\n"
	got := parseFirewallPorts(out, firewallRulePrefix)
	if len(got) != 2 || got[0] != 3000 || got[1] != 5173 {
		t.Errorf("parseFirewallPorts = %v", got)
	}
}

var wslIP = netip.MustParseAddr("172.17.213.47")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch; run go test -update and review the diff\n--- got ---\n%s", name, got)
	}
}

var scriptCases = []struct {
	name     string
	ops      []Op
	mirrored bool
}{
	{"nat_add_remove", []Op{{Kind: OpAdd, Port: 3000, ConnectAddress: wslIP}, {Kind: OpRemove, Port: 8081}}, false},
	{"mirrored_add", []Op{{Kind: OpAdd, Port: 5173}}, true},
}

func TestBuildScriptGolden(t *testing.T) {
	for _, tc := range scriptCases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := BuildScript(tc.ops, tc.mirrored)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, tc.name, s)
		})
	}
}

func TestBuildScriptErrors(t *testing.T) {
	bad := [][]Op{
		{{Kind: OpAdd, Port: 0, ConnectAddress: wslIP}},
		{{Kind: OpAdd, Port: 3000}}, // NAT add without an IP
		{{Kind: OpAdd, Port: 3000, ConnectAddress: netip.MustParseAddr("::1")}},
		{{Kind: "explode", Port: 3000}},
	}
	for _, ops := range bad {
		if _, err := BuildScript(ops, false); err == nil {
			t.Errorf("BuildScript(%+v) succeeded, want error", ops)
		}
	}
}

// TestBuildScriptParses runs the generated scripts through PowerShell's own
// parser when powershell.exe is reachable (i.e. under WSL), catching syntax
// errors the golden files can't.
func TestBuildScriptParses(t *testing.T) {
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("powershell.exe not available")
	}
	for _, tc := range scriptCases {
		s, err := BuildScript(tc.ops, tc.mirrored)
		if err != nil {
			t.Fatal(err)
		}
		check := "$errs = $null\n" +
			"[void][System.Management.Automation.Language.Parser]::ParseInput(" + winexec.Quote(s) + ", [ref]$null, [ref]$errs)\n" +
			"$errs | ForEach-Object { $_.Message + ' (line ' + $_.Extent.StartLineNumber + ')' }"
		out, err := winexec.PowerShell{Exe: ps, Runner: winexec.ExecRunner{}}.Command(context.Background(), check)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if out != "" {
			t.Errorf("%s has PowerShell syntax errors:\n%s", tc.name, out)
		}
	}
}

func TestParseResults(t *testing.T) {
	many := `{"results":[{"op":"add","port":3000,"ok":true,"message":""},{"op":"remove","port":8081,"ok":false,"message":"boom"}]}`
	rs, err := ParseResults([]byte(many))
	if err != nil || len(rs) != 2 || rs[1].Message != "boom" || rs[0].Op != OpAdd {
		t.Errorf("many: %+v, %v", rs, err)
	}

	one := `{"results":{"op":"add","port":3000,"ok":true,"message":""}}`
	rs, err = ParseResults([]byte(one))
	if err != nil || len(rs) != 1 || rs[0].Port != 3000 {
		t.Errorf("one: %+v, %v", rs, err)
	}

	for _, empty := range []string{`{"results":[]}`, `{"results":null}`, `{}`} {
		if rs, err := ParseResults([]byte(empty)); err != nil || len(rs) != 0 {
			t.Errorf("%s: %+v, %v", empty, rs, err)
		}
	}
	if _, err := ParseResults([]byte("nope")); err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(FirewallGroup, "Serve-a-Port") {
		t.Error("firewall group renamed; existing users' rules would be orphaned")
	}
}
