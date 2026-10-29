// Command serve-a-port exposes WSL2 ports to the local network.
//
// With no arguments it starts the interactive UI. See usage for the
// subcommands, which do the same things non-interactively for scripts.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/forward"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/portproxy"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/ports"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/state"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/tui"
	"github.com/jamaalsawyerd/wsl2-serve-a-port/internal/wsl"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = ""

const usage = `serve-a-port: share WSL2 ports with every device on your network

Usage:
  serve-a-port                 interactive dashboard
  serve-a-port status          environment checks, IPs, forwards, listeners
  serve-a-port add <ports>     forward ports, e.g. 3000 or "3000, 8000-8010"
  serve-a-port rm <ports>      stop forwarding ports
  serve-a-port repair          re-point forwards after the WSL IP changes
  serve-a-port demo            try the dashboard with made-up data
  serve-a-port version

Changes need one Windows admin (UAC) prompt each.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "serve-a-port:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return runTUI(nil)
	}
	switch args[0] {
	case "demo":
		deps := tui.DemoDeps(900 * time.Millisecond)
		return runTUI(&deps)
	case "version", "--version", "-v":
		fmt.Println("serve-a-port", buildVersion())
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	env := wsl.NewDetector().Detect(ctx)
	if err := env.Err(); err != nil {
		printChecks(env)
		return err
	}
	store, err := state.DefaultStore()
	if err != nil {
		return err
	}
	m := forward.NewManager(env, store)

	switch cmd := args[0]; cmd {
	case "status":
		printChecks(env)
		snap, err := m.Snapshot(ctx)
		if err != nil {
			return err
		}
		printSnapshot(snap)
		return nil

	case "add", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: serve-a-port %s <ports>", cmd)
		}
		ps, err := ports.Parse(strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		snap, err := m.Snapshot(ctx)
		if err != nil {
			return err
		}
		fmt.Println("Waiting for the Windows admin prompt...")
		var results []portproxy.Result
		if cmd == "add" {
			results, err = m.Add(ctx, snap, ps, false)
		} else {
			results, err = m.Remove(ctx, ps)
		}
		printResults(results)
		return err

	case "repair":
		snap, err := m.Snapshot(ctx)
		if err != nil {
			return err
		}
		if len(forward.RepairPorts(snap)) == 0 {
			fmt.Println("Nothing to repair.")
			return nil
		}
		fmt.Println("Waiting for the Windows admin prompt...")
		results, err := m.Repair(ctx, snap)
		printResults(results)
		return err

	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
}

// buildVersion prefers the ldflags version, then the module version that
// `go install ...@vX` records.
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// runTUI starts the dashboard against the real system, or against deps if
// given.
func runTUI(deps *tui.Deps) error {
	if deps == nil {
		store, err := state.DefaultStore()
		if err != nil {
			return err
		}
		deps = &tui.Deps{
			Detect: wsl.NewDetector().Detect,
			Connect: func(env wsl.Report) (tui.Service, error) {
				return forward.NewManager(env, store), nil
			},
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := tea.NewProgram(tui.New(ctx, *deps), tea.WithContext(ctx)).Run()
	return err
}

func printChecks(env wsl.Report) {
	for _, c := range env.Checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
		}
		fmt.Printf("  %s %-16s %s\n", mark, c.Name, c.Detail)
	}
	fmt.Println()
}

func printSnapshot(s forward.Snapshot) {
	fmt.Printf("WSL      %s (%s)\n", s.WSL.IP, s.WSL.Interface)
	for i, w := range s.Windows {
		label := "Windows "
		if i > 0 {
			label = "        "
		}
		fmt.Printf("%s %-15s %s\n", label, w.IP, w.Alias)
	}
	for _, w := range s.Warnings {
		fmt.Println("warning:", w)
	}

	fmt.Println("\nForwards:")
	if len(s.Entries) == 0 {
		fmt.Println("  (none)")
	}
	lan := ""
	if len(s.Windows) > 0 {
		lan = s.Windows[0].IP.String()
	}
	for _, e := range s.Entries {
		proc := ""
		if e.Listener != nil && e.Listener.Process != "" {
			proc = e.Listener.Process
		}
		fmt.Printf("  %-6d %-11s %-10s http://%s:%d\n", e.Port, e.Status, proc, lan, e.Port)
	}

	fmt.Println("\nListening in WSL:")
	for _, l := range s.Listeners {
		reach := "reachable"
		if !l.Reachable {
			reach = "loopback only"
		}
		fmt.Printf("  %-6d %-14s %-12s %v\n", l.Port, reach, l.Process, l.Addrs)
	}
}

func printResults(rs []portproxy.Result) {
	for _, r := range rs {
		if r.OK {
			fmt.Printf("  ✓ %s %d\n", r.Op, r.Port)
		} else {
			fmt.Printf("  ✗ %s %d: %s\n", r.Op, r.Port, r.Message)
			if hint := portproxy.Hint(r.Message); hint != "" {
				fmt.Printf("    ↳ %s\n", hint)
			}
		}
	}
}
