package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/isumin/hopmux/core/discover"
	"github.com/isumin/hopmux/core/sshconfig"
	"github.com/isumin/hopmux/core/tmuxctl"
)

const probeTimeout = 6 * time.Second

// outputCap keeps a runaway command from flooding the agent's context.
const outputCap = 64 * 1024

// toolSpecs describes the five tools. Descriptions are written for the agent:
// they say when to reach for each tool, not just what it does.
func toolSpecs() []map[string]any {
	obj := func(props map[string]any, required ...string) map[string]any {
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return []map[string]any{
		{
			"name": "list_hosts",
			"description": "Probe every SSH host in ~/.ssh/config and report reachability plus " +
				"per-GPU utilization/VRAM and a gpusFree count. A GPU counts as inUse when it " +
				"holds >=1.5GB VRAM (utilization % is an instantaneous sample — do not trust " +
				"util 0% alone). Use this first to see which servers have free GPUs before " +
				"placing work.",
			"inputSchema": obj(map[string]any{}),
		},
		{
			"name": "list_sessions",
			"description": "List Claude Code / Codex / tmux sessions across hosts (or one host), " +
				"each with its working directory, title, and age. Use to find where past work " +
				"lives before moving or resuming it.",
			"inputSchema": obj(map[string]any{
				"host": str("optional: limit to this host alias"),
			}),
		},
		{
			"name": "run_command",
			"description": "Run a shell command on a host over SSH (non-interactive, no TTY) and " +
				"return its stdout/stderr/exit code. Use for inspecting code and data, checking " +
				"environments, or running short jobs. For anything long-running, start it inside " +
				"tmux (see start_session) or with nohup instead of blocking here.",
			"inputSchema": obj(map[string]any{
				"host":        str("host alias from ~/.ssh/config"),
				"command":     str("shell command to run on the host"),
				"timeout_sec": map[string]any{"type": "integer", "description": "max seconds to wait (default 60, max 600)"},
			}, "host", "command"),
		},
		{
			"name": "copy_path",
			"description": "Copy a file or directory from one host to another with rsync running " +
				"ON the source host, straight to the destination (fast lab-network path; requires " +
				"the source host to be able to ssh to the destination). Set background=true for " +
				"large datasets — the copy then runs in a tmux session on the source host and this " +
				"returns immediately with the session name to poll via run_command.",
			"inputSchema": obj(map[string]any{
				"src_host":    str("source host alias"),
				"src_path":    str("path on the source host (directory contents copy rsync-style: trailing slash copies contents)"),
				"dst_host":    str("destination host alias"),
				"dst_path":    str("path on the destination host"),
				"background":  map[string]any{"type": "boolean", "description": "run in a detached tmux session on the source host and return immediately (default false)"},
				"timeout_sec": map[string]any{"type": "integer", "description": "foreground mode: max seconds to wait (default 600, max 3600)"},
			}, "src_host", "src_path", "dst_host", "dst_path"),
		},
		{
			"name": "start_session",
			"description": "Start a new claude / codex / shell session in a detached tmux session " +
				"on a host, in a given directory, optionally with an initial prompt. Returns the " +
				"tmux session name; the session also appears in the hopmux app so the user can " +
				"watch it. Check on it later with run_command: tmux capture-pane -p -t <name>.",
			"inputSchema": obj(map[string]any{
				"host":   str("host alias"),
				"agent":  map[string]any{"type": "string", "enum": []string{"claude", "codex", "shell"}, "description": "what to run"},
				"dir":    str("working directory on the host (default ~)"),
				"name":   str("optional tmux session name (default hopmux_<agent>_<time>)"),
				"prompt": str("optional initial prompt for claude/codex"),
			}, "host", "agent"),
		},
	}
}

// call dispatches a tools/call. The second return marks a tool-level error
// (shown to the agent as a failed call, not a protocol error).
func (s *server) call(name string, raw json.RawMessage) (string, bool) {
	switch name {
	case "list_hosts":
		return s.listHosts()
	case "list_sessions":
		var a struct {
			Host string `json:"host"`
		}
		_ = json.Unmarshal(raw, &a)
		return s.listSessions(a.Host)
	case "run_command":
		var a struct {
			Host       string `json:"host"`
			Command    string `json:"command"`
			TimeoutSec int    `json:"timeout_sec"`
		}
		if err := json.Unmarshal(raw, &a); err != nil || a.Host == "" || a.Command == "" {
			return "run_command needs host and command", true
		}
		return s.runCommand(a.Host, a.Command, clampSec(a.TimeoutSec, 60, 600))
	case "copy_path":
		var a struct {
			SrcHost    string `json:"src_host"`
			SrcPath    string `json:"src_path"`
			DstHost    string `json:"dst_host"`
			DstPath    string `json:"dst_path"`
			Background bool   `json:"background"`
			TimeoutSec int    `json:"timeout_sec"`
		}
		if err := json.Unmarshal(raw, &a); err != nil ||
			a.SrcHost == "" || a.SrcPath == "" || a.DstHost == "" || a.DstPath == "" {
			return "copy_path needs src_host, src_path, dst_host, dst_path", true
		}
		return s.copyPath(a.SrcHost, a.SrcPath, a.DstHost, a.DstPath, a.Background,
			clampSec(a.TimeoutSec, 600, 3600))
	case "start_session":
		var a struct {
			Host   string `json:"host"`
			Agent  string `json:"agent"`
			Dir    string `json:"dir"`
			Name   string `json:"name"`
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(raw, &a); err != nil || a.Host == "" || a.Agent == "" {
			return "start_session needs host and agent", true
		}
		return s.startSession(a)
	default:
		return "unknown tool: " + name, true
	}
}

func clampSec(v, def, max int) time.Duration {
	if v <= 0 {
		v = def
	}
	if v > max {
		v = max
	}
	return time.Duration(v) * time.Second
}

func (s *server) entryFor(alias string) (sshconfig.Entry, bool) {
	for _, e := range s.entries {
		if e.Alias == alias {
			return e, true
		}
	}
	return sshconfig.Entry{}, false
}

// ---- tools ----

func (s *server) listHosts() (string, bool) {
	be := discover.NewSSH(probeTimeout, s.entries)
	hosts := be.Discover(s.hosts, nil)
	out := make([]map[string]any, 0, len(hosts))
	for _, h := range hosts {
		row := map[string]any{
			"host":      h.Name,
			"reachable": h.Reachable,
		}
		if h.AuthRequired {
			row["needsInteractiveLogin"] = true
		}
		if h.Err != "" {
			row["error"] = h.Err
		}
		if h.Hostname != "" {
			row["hostname"] = h.Hostname
		}
		if len(h.GPUs) > 0 {
			gpus := make([]map[string]any, 0, len(h.GPUs))
			free := 0
			for _, g := range h.GPUs {
				if !g.InUse() {
					free++
				}
				gpus = append(gpus, map[string]any{
					"index": g.Index, "name": g.Name,
					"utilPct": g.Util, "memUsedMiB": g.MemUsed, "memTotalMiB": g.MemTotal,
					"inUse": g.InUse(),
				})
			}
			row["gpus"] = gpus
			row["gpusFree"] = free
			row["gpusTotal"] = len(h.GPUs)
		}
		if h.Reachable {
			row["tmuxSessions"] = len(h.Tmux)
			row["agentSessions"] = len(h.Agents)
		}
		out = append(out, row)
	}
	return marshal(out)
}

func (s *server) listSessions(host string) (string, bool) {
	targets := s.hosts
	if host != "" {
		if _, ok := s.entryFor(host); !ok {
			return "unknown host: " + host, true
		}
		targets = []string{host}
	}
	be := discover.NewSSH(probeTimeout, s.entries)
	hosts := be.Discover(targets, nil)
	type row struct {
		Host  string `json:"host"`
		Kind  string `json:"kind"` // claude | codex | tmux
		Name  string `json:"name,omitempty"`
		SID   string `json:"sid,omitempty"`
		CWD   string `json:"cwd,omitempty"`
		Title string `json:"title,omitempty"`
		AgeH  int64  `json:"ageHours,omitempty"`
	}
	var rows []row
	for _, h := range hosts {
		for _, t := range h.Tmux {
			rows = append(rows, row{Host: h.Name, Kind: "tmux", Name: t.Name})
		}
		for _, a := range h.Agents {
			var age int64
			if h.Now > 0 && a.MTime > 0 {
				age = (h.Now - a.MTime) / 3600
			}
			rows = append(rows, row{Host: h.Name, Kind: string(a.Agent),
				SID: a.SID, CWD: a.CWD, Title: a.Title, AgeH: age})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].AgeH < rows[j].AgeH })
	return marshal(rows)
}

func (s *server) runCommand(host, command string, timeout time.Duration) (string, bool) {
	if _, ok := s.entryFor(host); !ok {
		return "unknown host: " + host, true
	}
	stdout, stderr, code, err := discover.RunCommand(host, command, timeout)
	if err != nil {
		return "ssh failed: " + err.Error(), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "exit code: %d\n", code)
	if stdout != "" {
		b.WriteString("--- stdout ---\n" + truncate(stdout) + "\n")
	}
	if stderr != "" {
		b.WriteString("--- stderr ---\n" + truncate(stderr) + "\n")
	}
	return b.String(), code != 0
}

// copyPath runs rsync ON the source host, pushing straight to the destination
// endpoint (resolved from the local ssh config) — the fast lab-network path.
func (s *server) copyPath(srcHost, srcPath, dstHost, dstPath string, background bool, timeout time.Duration) (string, bool) {
	if _, ok := s.entryFor(srcHost); !ok {
		return "unknown src_host: " + srcHost, true
	}
	dst, ok := s.entryFor(dstHost)
	if !ok {
		return "unknown dst_host: " + dstHost, true
	}
	hn := dst.HostName
	if hn == "" {
		hn = dst.Alias
	}
	port := dst.Port
	if port == "" {
		port = "22"
	}
	spec := hn + ":" + shq(dstPath)
	if dst.User != "" {
		spec = dst.User + "@" + spec
	}
	rsync := "rsync -az -e 'ssh -p " + port + " -o BatchMode=yes -o StrictHostKeyChecking=accept-new' " +
		shq(srcPath) + " " + spec

	if background {
		name := fmt.Sprintf("hopmux_copy_%d", time.Now().Unix())
		cmd := "tmux new-session -d -s " + shq(name) + " " + shq(rsync+"; echo hopmux_copy_exit=$?") +
			" && echo started"
		stdout, stderr, code, err := discover.RunCommand(srcHost, cmd, 30*time.Second)
		if err != nil || code != 0 {
			return "failed to start background copy: " + strings.TrimSpace(stdout+stderr), true
		}
		return "copy running in tmux session " + name + " on " + srcHost +
			"\ncheck with run_command: tmux capture-pane -p -t " + name +
			" (finished when it prints hopmux_copy_exit=0; the session then lingers until closed)", false
	}

	stdout, stderr, code, err := discover.RunCommand(srcHost, rsync, timeout)
	if err != nil {
		return "copy failed: " + err.Error() +
			"\nfor large transfers retry with background=true", true
	}
	if code != 0 {
		return "rsync exit code " + fmt.Sprint(code) + "\n" + truncate(stderr) +
			"\nhint: this runs on " + srcHost + " and sshes to " + dstHost +
			" directly — if auth failed, the source host has no key for the destination" +
			" (test with run_command on " + srcHost + ": ssh -o BatchMode=yes " + spec2host(spec) + " true)", true
	}
	return "copied " + srcHost + ":" + srcPath + " -> " + dstHost + ":" + dstPath + "\n" + truncate(stdout), false
}

func (s *server) startSession(a struct {
	Host   string `json:"host"`
	Agent  string `json:"agent"`
	Dir    string `json:"dir"`
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
}) (string, bool) {
	if _, ok := s.entryFor(a.Host); !ok {
		return "unknown host: " + a.Host, true
	}
	agent := a.Agent
	if agent == "shell" {
		agent = ""
	}
	name := a.Name
	if name == "" {
		name = fmt.Sprintf("hopmux_%s_%s", a.Agent, time.Now().Format("150405"))
	}
	cmd := tmuxctl.NewDetachedSession(name, a.Dir, agent, a.Prompt)
	stdout, stderr, code, err := discover.RunCommand(a.Host, cmd, 30*time.Second)
	if err != nil {
		return "ssh failed: " + err.Error(), true
	}
	if code != 0 {
		return "failed to start session (exit " + fmt.Sprint(code) + "):\n" + truncate(stdout+stderr), true
	}
	return "started tmux session " + name + " on " + a.Host + " (dir " + orTilde(a.Dir) + ")" +
		"\ninspect with run_command: tmux capture-pane -p -t " + name, false
}

// ---- small helpers ----

func marshal(v any) (string, bool) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "encode: " + err.Error(), true
	}
	return string(b), false
}

func truncate(s string) string {
	if len(s) <= outputCap {
		return strings.TrimRight(s, "\n")
	}
	return s[:outputCap] + "\n[truncated at 64KB]"
}

// shq single-quotes a string for a POSIX shell.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// spec2host strips the :path part of a user@host:'path' rsync spec.
func spec2host(spec string) string {
	if i := strings.LastIndex(spec, ":"); i > 0 {
		return spec[:i]
	}
	return spec
}

func orTilde(dir string) string {
	if dir == "" {
		return "~"
	}
	return dir
}
