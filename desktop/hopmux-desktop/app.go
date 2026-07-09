package main

import (
	"context"
	"os"
	"sync"
	"time"

	pty "github.com/aymanbagabas/go-pty"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/isumin/hopmux/core/discover"
	"github.com/isumin/hopmux/core/model"
	"github.com/isumin/hopmux/core/sshconfig"
	"github.com/isumin/hopmux/core/tmuxctl"
)

// App is the Wails backend for the native two-panel hopmux window.
//
// The left panel (server + session list) is native HTML driven by Scan()/the
// "host:update" event. The right panel is an xterm.js terminal; OpenSession()
// spawns the chosen ssh/tmux command in a PTY and streams it to that terminal —
// so a session renders *inside the right panel* while the sidebar stays put.
type App struct {
	ctx      context.Context
	entries  []sshconfig.Entry
	hostList []string

	mu       sync.Mutex
	sessions map[string]*ptySession // tab id -> live PTY
	seq      int
}

// ptySession is one open terminal tab.
type ptySession struct {
	id   string
	ptmx pty.Pty
	cmd  *pty.Cmd

	// The frontend subscribes to "pty:data:<id>" only AFTER OpenSession returns
	// the id, but the PTY starts producing output immediately (Windows ssh.exe
	// dumps its whole initial screen at once). Buffer everything until the
	// frontend calls AttachTab(id); then flush and stream live. Without this the
	// entire first screen is emitted before anyone is listening — a blank
	// terminal on Windows.
	bufMu    sync.Mutex
	attached bool
	backlog  []byte
}

func NewApp() *App { return &App{sessions: map[string]*ptySession{}} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.entries, _ = sshconfig.Parse("~/.ssh/config")
	for _, e := range a.entries {
		a.hostList = append(a.hostList, e.Alias)
	}
}

func (a *App) domReady(ctx context.Context) {
	// kick off the first scan as soon as the UI is ready
	go a.Scan()
}

// ---- data API (called from the frontend) ----

// HostNames returns the config-ordered aliases (so the sidebar can render
// placeholders immediately, before any scan completes).
func (a *App) HostNames() []string { return a.hostList }

// Platform returns the host OS ("windows", "darwin", "linux") so the frontend
// can tell xterm.js it's driving a Windows ConPTY — without that hint xterm
// mis-parses ssh.exe's ConPTY control sequences and renders a blank screen.
func (a *App) Platform() string { return runtime.Environment(a.ctx).Platform }

// Scan probes every host over SSH, emitting a "host:update" event per host as it
// finishes and a "scan:done" event at the end.
func (a *App) Scan() {
	be := discover.NewSSH(6*time.Second, a.entries)
	be.Discover(a.hostList, func(h model.Host) {
		runtime.EventsEmit(a.ctx, "host:update", h)
	})
	runtime.EventsEmit(a.ctx, "scan:done", nil)
}

// OpenReq describes what to open in the right-hand terminal.
type OpenReq struct {
	Host  string `json:"host"`
	Kind  string `json:"kind"`  // "tmux" | "agent" | "login" | "newSession"
	Name  string `json:"name"`  // tmux session name
	Agent string `json:"agent"` // "claude" | "codex" | "" (shell) for agent/newSession
	SID   string `json:"sid"`   // agent session id
	CWD   string `json:"cwd"`   // agent session dir / new-session dir
}

// OpenSession opens the chosen session as a NEW terminal tab and returns its id.
// The frontend uses the id to route input/resize/output and to label the tab.
func (a *App) OpenSession(req OpenReq) string {
	var remote string
	switch req.Kind {
	case "tmux":
		remote = tmuxctl.AttachExisting(req.Name)
	case "agent":
		// Each agent session runs in its OWN tmux session (attach-or-create): real
		// tmux splits (Ctrl-B %/"), persistence, and clean reopen — no stale pane.
		remote = tmuxctl.AgentTmux(model.AgentSession{
			Agent: model.Agent(req.Agent), SID: req.SID, CWD: req.CWD})
	case "newSession":
		remote = tmuxctl.NewSession(req.CWD, req.Agent)
	case "login":
		remote = "" // plain interactive ssh (password/passphrase entry)
	default:
		remote = tmuxctl.AttachSession()
	}
	args := discover.RunArgs(req.Host, remote)
	return a.spawn("ssh", args)
}

// RescanHost re-probes a single host and emits a "host:update". Used after an
// interactive login warms the ControlMaster, so its sessions can appear.
func (a *App) RescanHost(host string) model.Host {
	be := discover.NewSSH(6*time.Second, a.entries)
	res := be.Discover([]string{host}, func(h model.Host) {
		runtime.EventsEmit(a.ctx, "host:update", h)
	})
	if len(res) > 0 {
		return res[0]
	}
	return model.Host{Name: host}
}

// ---- terminal / PTY (one per tab) ----

func (a *App) spawn(name string, args []string) string {
	a.mu.Lock()
	a.seq++
	id := "t" + itoa(a.seq)
	a.mu.Unlock()

	// go-pty: create the pseudo-terminal first, then build the command bound to
	// it. On Unix this is a real PTY (creack under the hood); on Windows it's a
	// ConPTY — so terminals actually work on Windows, unlike raw creack/pty.
	ptmx, err := pty.New()
	if err != nil {
		runtime.EventsEmit(a.ctx, "pty:data:"+id, "\r\n  failed: "+err.Error()+"\r\n")
		return id
	}
	cmd := ptmx.Command(name, args...)
	// A GUI app launched via Finder/LaunchServices does NOT inherit the shell's
	// LANG/LC_* — force a UTF-8 locale so the remote emits real UTF-8 (else CJK
	// renders as underscores).
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color", "COLORTERM=truecolor",
		"LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8", "LC_CTYPE=en_US.UTF-8")
	// On Windows, a console-subsystem child (ssh.exe) spawned from this GUI app
	// would pop its own black console window. Suppress it (no-op elsewhere).
	hideConsole(cmd)

	if err := cmd.Start(); err != nil {
		ptmx.Close()
		runtime.EventsEmit(a.ctx, "pty:data:"+id, "\r\n  failed: "+err.Error()+"\r\n")
		return id
	}

	s := &ptySession{id: id, ptmx: ptmx, cmd: cmd}
	a.mu.Lock()
	a.sessions[id] = s
	a.mu.Unlock()

	go func() {
		buf := make([]byte, 8192)
		var carry []byte
		for {
			n, rerr := ptmx.Read(buf)
			if n > 0 {
				chunk := append(carry, buf[:n]...)
				good := completeUTF8Len(chunk) // don't cut a UTF-8 rune across events
				carry = append(carry[:0:0], chunk[good:]...)
				if good > 0 {
					s.emitOrBuffer(a, chunk[:good])
				}
			}
			if rerr != nil {
				if len(carry) > 0 {
					s.emitOrBuffer(a, carry)
				}
				a.mu.Lock()
				delete(a.sessions, id)
				a.mu.Unlock()
				runtime.EventsEmit(a.ctx, "pty:exit:"+id, nil)
				return
			}
		}
	}()
	return id
}

// emitOrBuffer sends data to the frontend if it has attached to this tab, else
// stashes it in the backlog to be flushed the moment AttachTab is called.
func (s *ptySession) emitOrBuffer(a *App, data []byte) {
	s.bufMu.Lock()
	if !s.attached {
		s.backlog = append(s.backlog, data...)
		s.bufMu.Unlock()
		return
	}
	s.bufMu.Unlock()
	runtime.EventsEmit(a.ctx, "pty:data:"+s.id, string(data))
}

// AttachTab is called by the frontend once it has subscribed to this tab's
// "pty:data:<id>" event. It flushes any output buffered before the subscription
// existed, then switches the session to live streaming. This closes the race
// where the PTY's initial burst is emitted before anyone is listening.
func (a *App) AttachTab(id string) {
	s := a.get(id)
	if s == nil {
		return
	}
	s.bufMu.Lock()
	backlog := s.backlog
	s.backlog = nil
	s.attached = true
	s.bufMu.Unlock()
	if len(backlog) > 0 {
		runtime.EventsEmit(a.ctx, "pty:data:"+id, string(backlog))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func (a *App) get(id string) *ptySession {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[id]
}

// CloseSession kills the PTY for a tab.
func (a *App) CloseSession(id string) {
	s := a.get(id)
	if s == nil {
		return
	}
	a.mu.Lock()
	delete(a.sessions, id)
	a.mu.Unlock()
	if s.ptmx != nil {
		s.ptmx.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
}

// completeUTF8Len returns the length of the longest prefix of b that ends on a
// complete UTF-8 rune boundary (so we never emit a half-character).
func completeUTF8Len(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	// walk back over trailing continuation bytes (0b10xxxxxx) to find a lead byte
	i := len(b) - 1
	for i >= 0 && b[i]&0xC0 == 0x80 {
		i--
	}
	if i < 0 {
		return len(b)
	}
	lead := b[i]
	var need int
	switch {
	case lead&0x80 == 0x00:
		need = 1
	case lead&0xE0 == 0xC0:
		need = 2
	case lead&0xF0 == 0xE0:
		need = 3
	case lead&0xF8 == 0xF0:
		need = 4
	default:
		return len(b) // invalid lead; don't hold anything back
	}
	if len(b)-i == need {
		return len(b) // trailing rune is complete
	}
	return i // hold back the incomplete trailing rune
}

// SendInput writes frontend keystrokes into the given tab's PTY.
func (a *App) SendInput(id string, data string) {
	if s := a.get(id); s != nil && s.ptmx != nil {
		s.ptmx.Write([]byte(data))
	}
}

// Resize matches a tab's PTY to its xterm grid.
func (a *App) Resize(id string, cols int, rows int) {
	if s := a.get(id); s != nil && s.ptmx != nil && cols > 0 && rows > 0 {
		s.ptmx.Resize(cols, rows)
	}
}
