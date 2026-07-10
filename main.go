// Command hopmux is a cmux-style TUI that indexes your SSH servers' tmux and
// Claude Code / Codex sessions and lets you jump back into any of them.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/isumin/hopmux/core/discover"
	"github.com/isumin/hopmux/core/sshconfig"
	"github.com/isumin/hopmux/internal/mcp"
	"github.com/isumin/hopmux/internal/ui"
)

const version = "0.2.0"

func main() {
	// `hopmux mcp` — serve the engine as MCP tools for an orchestrating agent
	// (e.g. `claude mcp add hopmux -- hopmux mcp`). Checked before flag parsing
	// so the subcommand isn't mistaken for a positional arg.
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if err := mcp.Run(version); err != nil {
			fmt.Fprintln(os.Stderr, "hopmux mcp:", err)
			os.Exit(1)
		}
		return
	}
	var (
		demo       = flag.Bool("demo", false, "run against built-in mock data (no servers needed)")
		only       = flag.String("only", "", "comma-separated subset of hosts")
		timeoutSec = flag.Int("timeout", 6, "per-host SSH connect timeout (seconds)")
		configPath = flag.String("config", "~/.ssh/config", "path to ssh config")
		showVer    = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("hopmux", version)
		return
	}

	entries, err := sshconfig.Parse(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hopmux: reading ssh config:", err)
		os.Exit(1)
	}
	hosts := make([]string, 0, len(entries))
	for _, e := range entries {
		hosts = append(hosts, e.Alias)
	}
	if *only != "" {
		want := map[string]bool{}
		for _, s := range strings.Split(*only, ",") {
			if s = strings.TrimSpace(s); s != "" {
				want[s] = true
			}
		}
		filtered := hosts[:0]
		for _, h := range hosts {
			if want[h] {
				filtered = append(filtered, h)
			}
		}
		hosts = filtered
	}

	// In demo mode we don't need a config at all — synthesize a host list.
	if *demo && len(hosts) == 0 {
		hosts = []string{"ml-train-01", "prod-api", "research-box", "gpu-node-2",
			"staging", "db-primary", "edge-01", "old-box"}
	}
	if len(hosts) == 0 {
		fmt.Fprintf(os.Stderr, "hopmux: no hosts in %s\n", *configPath)
		os.Exit(1)
	}

	var backend discover.Backend
	if *demo {
		backend = discover.NewMock(1)
	} else {
		backend = discover.NewSSH(time.Duration(*timeoutSec)*time.Second, entries)
	}

	root := ui.New(backend, hosts)
	if *demo {
		root = root.WithSimulate()
	}
	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "hopmux:", err)
		os.Exit(1)
	}
}
