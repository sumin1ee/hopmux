// Package model holds the shared data types that the discovery layer produces
// and the UI consumes.
package model

import (
	"path"
	"sort"
	"strings"
)

// Agent identifies which coding agent a saved session belongs to.
type Agent string

const (
	Claude Agent = "claude"
	Codex  Agent = "codex"
)

// AgentSession is a resumable Claude Code or Codex CLI session found on a host.
type AgentSession struct {
	Agent Agent
	SID   string // session id used for --resume <sid> / resume <sid>
	CWD   string // working directory the session lives in (the resume path)
	MTime int64  // last-modified epoch seconds
	Title string // first human prompt (best-effort), for at-a-glance meaning
	Host  string
}

// Project returns the last path element of CWD, e.g. /home/dev/api -> api.
func (a AgentSession) Project() string {
	p := strings.TrimRight(a.CWD, "/")
	if p == "" {
		return "?"
	}
	if b := path.Base(p); b != "" {
		return b
	}
	return p
}

// TmuxSession is a live tmux session already running on a host.
type TmuxSession struct {
	Name     string
	Windows  string
	Attached bool
	Created  string
	Host     string
}

// GPU is one NVIDIA GPU's snapshot (from nvidia-smi), if the host has any.
type GPU struct {
	Index    int
	Util     int // utilization %
	MemUsed  int // MiB
	MemTotal int // MiB
	Name     string
}

// MemPct is memory used as a percentage of total.
func (g GPU) MemPct() int {
	if g.MemTotal <= 0 {
		return 0
	}
	return g.MemUsed * 100 / g.MemTotal
}

// GPUBusyMemMiB is the VRAM threshold above which a GPU counts as in use.
// Utilization % is an instantaneous sample (a training job can read 0% between
// kernels), but a real job always holds VRAM — 1.5GB filters out desktop/idle
// allocations while catching any actual workload.
const GPUBusyMemMiB = 1536

// InUse reports whether this GPU is occupied, judged by VRAM held.
func (g GPU) InUse() bool { return g.MemUsed >= GPUBusyMemMiB }

// Host is one entry from ~/.ssh/config plus whatever we discovered on it.
type Host struct {
	Name      string
	Reachable bool
	Scanned   bool // whether a probe has completed (reachable or not)
	// AuthRequired means the host answered but needs interactive auth
	// (password/passphrase). It's reachable and openable — you just can't list
	// its sessions non-interactively. Not a failure.
	AuthRequired bool
	Err          string
	Tmux      []TmuxSession
	Agents    []AgentSession
	GPUs      []GPU
	Hostname  string // remote uname nodename, if probed
	Now       int64  // remote clock at probe time (for relative times)

	// Alias is the concrete host we actually connect through when several ssh
	// config aliases share one endpoint (IP dedup). Empty means "self".
	Alias string
}

// SessionCount is tmux + agent sessions.
func (h Host) SessionCount() int { return len(h.Tmux) + len(h.Agents) }

// RecentAgents returns this host's agent sessions newest-first.
func (h Host) RecentAgents() []AgentSession {
	out := append([]AgentSession(nil), h.Agents...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].MTime > out[j].MTime })
	return out
}
