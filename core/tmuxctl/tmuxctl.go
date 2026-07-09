// Package tmuxctl builds the remote shell commands that drive tmux on a host.
//
// hopmux does not embed a terminal. It parks the sessions it opens inside one
// tmux session per host (named "hopmux") and shows them as panes side by side —
// the sidebar is the only navigation axis, matching the cmux feel.
//
// Reuse model ("if already open, jump to it, don't duplicate"): every pane hopmux
// opens is tagged with the agent's session id via a tmux pane option. Opening a
// session first looks for a pane already carrying that id; if found it just
// selects it, otherwise it splits a new pane and runs the resume command there.
package tmuxctl

import (
	"fmt"
	"strconv"

	"github.com/isumin/hopmux/core/model"
)

// Session is the tmux session hopmux manages on each host.
const Session = "hopmux"

// tag is the tmux user-option key we stamp on each pane so we can find it again.
const tag = "@hopmux_sid"

func q(s string) string { return "'" + escSingle(s) + "'" }

func escSingle(s string) string {
	out := make([]byte, 0, len(s)+2)
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\\', '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// locale forces a UTF-8 locale on the remote so agent TUIs render wide chars
// (Korean/CJK) instead of underscores. GUI-app-spawned ssh doesn't forward the
// local LANG, and many boxes default to POSIX; C.utf8 is near-universal.
const locale = "export LC_ALL=C.utf8 LANG=C.utf8 2>/dev/null || " +
	"export LC_ALL=en_US.UTF-8 LANG=en_US.UTF-8; "

// ResumeCommand re-enters a Claude/Codex session in its own directory.
func ResumeCommand(a model.AgentSession) string {
	cwd := a.CWD
	if cwd == "" {
		cwd = "~"
	}
	cd := locale + "cd " + q(cwd) + " 2>/dev/null; "
	switch a.Agent {
	case model.Claude:
		return cd + "claude --resume " + q(a.SID)
	case model.Codex:
		if a.SID != "" {
			return cd + "codex resume " + q(a.SID)
		}
		return cd + "codex"
	default:
		return cd + "$SHELL"
	}
}

// AttachExisting attaches to a pre-existing tmux session by name (a live tmux the
// user already had — hopmux creates nothing here).
func AttachExisting(name string) string {
	return "tmux attach -t " + q(name)
}

// DirectResume runs an agent session directly (no tmux wrapper) — a fresh
// process every time in the right directory with a UTF-8 locale.
func DirectResume(a model.AgentSession) string {
	return ResumeCommand(a)
}

// agentSessionName is a stable, unique tmux session name for one agent session.
func agentSessionName(a model.AgentSession) string {
	sid := a.SID
	if len(sid) > 12 {
		sid = sid[:12]
	}
	return "hopmux_" + string(a.Agent) + "_" + sid
}

// AgentTmux runs an agent session inside its OWN tmux session (attach-or-create).
// This gives real tmux splits (Ctrl-B %/") and persistence: detaching leaves it
// running, reopening re-attaches the same session. Each agent session gets its
// own tmux session (named by sid), so there's no window juggling or stale-pane
// reuse — the earlier bug.
func AgentTmux(a model.AgentSession) string {
	name := agentSessionName(a)
	inner := ResumeCommand(a)
	// new-session -A: attach if it exists, else create running the resume command.
	return locale + "tmux new-session -A -s " + q(name) + " " + q(inner)
}

// hasSession is a shell test for the managed session.
func hasSession() string {
	return "tmux has-session -t " + q(Session) + " 2>/dev/null"
}

// findPaneBySID emits a shell command that prints the pane id tagged with sid
// (empty if none). Uses list-panes across the hopmux session.
func findPaneBySID(sid string) string {
	// for each pane, print "<pane_id> <tag value>"; grep the matching sid
	return fmt.Sprintf(
		`tmux list-panes -s -t %s -F '#{pane_id} #{%s}' 2>/dev/null | `+
			`awk -v s=%s '$2==s{print $1; exit}'`,
		q(Session), tag[1:], q(sid))
}

// OpenAgent opens (or re-focuses) an agent session as a pane in the hopmux
// session, then attaches. Implements the reuse model.
func OpenAgent(a model.AgentSession) string {
	inner := ResumeCommand(a)
	sid := q(a.SID)
	// Create the session on first use with this agent as the first pane, tagged.
	create := "tmux new-session -d -s " + q(Session) + " " + q(inner) +
		" && tmux set-option -p -t " + q(Session) + " " + tag + " " + sid
	// If the session exists: reuse the pane if present, else split a new tagged one.
	existing := "P=$(" + findPaneBySID(a.SID) + "); " +
		"if [ -n \"$P\" ]; then tmux select-pane -t \"$P\"; " +
		"else tmux split-window -h -t " + q(Session) + " " + q(inner) +
		" && tmux set-option -p " + tag + " " + sid + "; fi"
	body := "if " + hasSession() + "; then " + existing + "; else " + create + "; fi"
	return body + "; tmux attach -t " + q(Session)
}

// Split splits the current pane and runs the agent session in the new pane.
// vertical=false → side-by-side (-h); true → stacked (-v).
func Split(a model.AgentSession, vertical bool) string {
	flag := "-h"
	if vertical {
		flag = "-v"
	}
	inner := ResumeCommand(a)
	sid := q(a.SID)
	do := "tmux split-window " + flag + " -t " + q(Session) + " " + q(inner) +
		" && tmux set-option -p " + tag + " " + sid
	create := "tmux new-session -d -s " + q(Session) + " " + q(inner) +
		" && tmux set-option -p -t " + q(Session) + " " + tag + " " + sid
	body := "if " + hasSession() + "; then " + do + "; else " + create + "; fi"
	return body + "; tmux attach -t " + q(Session)
}

// NewShell opens a plain shell pane in the hopmux session and attaches.
func NewShell() string {
	do := "tmux split-window -h -t " + q(Session)
	create := "tmux new-session -d -s " + q(Session)
	body := "if " + hasSession() + "; then " + do + "; else " + create + "; fi"
	return body + "; tmux attach -t " + q(Session)
}

// NewSession starts a brand-new session in the given directory as a hopmux pane,
// then attaches. agent is "claude", "codex", or "" (plain shell). dir may be ""
// (falls back to home) or contain ~ (expanded by the remote shell).
func NewSession(dir, agent string) string {
	if dir == "" {
		dir = "~"
	}
	// Let the remote shell expand ~; only quote when it's an absolute/literal path.
	cd := locale + "cd " + shellPath(dir) + " && "
	var cmd string
	switch agent {
	case "claude":
		cmd = cd + "claude"
	case "codex":
		cmd = cd + "codex"
	default:
		cmd = cd + "exec ${SHELL:-sh} -l"
	}
	do := "tmux split-window -h -t " + q(Session) + " " + q(cmd)
	create := "tmux new-session -d -s " + q(Session) + " " + q(cmd)
	body := "if " + hasSession() + "; then " + do + "; else " + create + "; fi"
	return body + "; tmux attach -t " + q(Session)
}

// shellPath quotes a path for the remote shell but leaves a leading ~ unquoted so
// the shell still expands it (quoting "~/x" would stop ~ expansion).
func shellPath(p string) string {
	if p == "~" {
		return "~"
	}
	if len(p) >= 2 && p[0] == '~' && p[1] == '/' {
		return "~/" + escSingle(p[2:]) // ~/'rest'
	}
	return q(p)
}

// ClosePane kills the currently active pane of the hopmux session.
func ClosePane() string {
	return "tmux kill-pane -t " + q(Session) + " 2>/dev/null || " +
		"echo 'hopmux: no pane to close'; sleep 0.4"
}

// AttachSession attaches to (creating if needed) the managed session.
func AttachSession() string {
	return "tmux new-session -A -s " + q(Session)
}

// PaneLimitReached is a client-side helper: past this many panes, splitting gets
// impractical and we should warn instead. Exposed so the UI can decide.
const PaneLimit = 4

func atoiSafe(s string) int { n, _ := strconv.Atoi(s); return n }
