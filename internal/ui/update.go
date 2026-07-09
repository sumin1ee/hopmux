package ui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/isumin/hopmux/core/discover"
	"github.com/isumin/hopmux/core/model"
	"github.com/isumin/hopmux/core/tmuxctl"
)

// startScan launches discovery in a goroutine and returns a command that waits
// for the first streamed host. The stream lives only in the command closures and
// in the streamed messages (Bubble Tea passes the model by value, so it can't be
// reliably stored on the model from Init/commands).
func (m Model) startScan() tea.Cmd {
	st := &discoveryStream{
		ch:   make(chan model.Host, 64),
		done: make(chan []model.Host, 1),
	}
	backend := m.backend
	hostList := m.hostList
	go func() {
		res := backend.Discover(hostList, func(h model.Host) {
			st.ch <- h
		})
		close(st.ch)
		st.done <- res
	}()
	return waitForHost(st)
}

// waitForHost blocks (in a command goroutine) for the next host or scan end.
// It threads the stream pointer through the message so Update can re-issue.
func waitForHost(st *discoveryStream) tea.Cmd {
	return func() tea.Msg {
		h, ok := <-st.ch
		if !ok {
			return scanDoneMsg{hosts: <-st.done}
		}
		return hostUpdatedMsg{host: h, stream: st}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()
		return m, nil

	case hostUpdatedMsg:
		m.hosts[msg.host.Name] = msg.host
		if msg.host.Scanned {
			m.scanned = m.countScanned()
		}
		m.rebuildRows()
		return m, waitForHost(msg.stream)

	case scanDoneMsg:
		for _, h := range msg.hosts {
			m.hosts[h.Name] = h
		}
		m.now = discover.NowEpoch()
		m.scanning = false
		m.scanned = m.countScanned()
		m.rebuildRows()
		return m, nil

	case execDoneMsg:
		// returned from tea.ExecProcess after we come back from tmux
		m.statusMsg = ""
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// execDoneMsg is produced when an attached ssh/tmux process exits.
type execDoneMsg struct{ err error }

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A prior simulated-action banner clears on the next key.
	m.simMsg = ""

	// New-session picker swallows keys while active.
	if m.nsActive {
		return m.handleNewSessionKey(msg)
	}

	// Filter input mode swallows most keys.
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.filter = ""
			m.rebuildRows()
		case "enter":
			m.filtering = false
			m.foc = focusSessions
		case "backspace":
			if len(m.filter) > 0 {
				r := []rune(m.filter)
				m.filter = string(r[:len(r)-1])
				m.rebuildRows()
			}
		default:
			if len(msg.Runes) > 0 {
				m.filter += string(msg.Runes)
				m.rebuildRows()
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "up", "k":
		m.moveUp()
	case "down", "j":
		m.moveDown()

	case "left", "h":
		m.focusServers()
	case "right", "l":
		m.focusSessions()
	case "tab":
		if m.foc == focusServers {
			m.focusSessions()
		} else {
			m.focusServers()
		}

	case "enter":
		return m.actionOpen()
	case "s":
		return m.actionSplit(false)
	case "v":
		return m.actionSplit(true)
	case "x":
		return m.actionClose()
	case "n":
		return m.actionNewShell()

	case "b":
		m.toggleSidebar()
	case "g":
		m.showGPU = !m.showGPU
	case "d":
		m.toggleTheme()
	case "r":
		if !m.scanning {
			m.now = discover.NowEpoch()
			m.statusMsg = "rescanning…"
			m.scanning = true
			return m, m.startScan()
		}
	case "/":
		m.filtering = true
		m.foc = focusSessions
	}
	return m, nil
}

// ---- movement / focus ----

func (m *Model) sidebarCount() int { return 1 + len(m.hostList) } // ★Recent + hosts

func (m *Model) moveUp() {
	if m.foc == focusServers {
		if m.serverIdx > 0 {
			m.serverIdx--
			m.onServerChange()
		}
	} else {
		if m.sessionIdx > 0 {
			m.sessionIdx--
			m.clampScroll()
		}
	}
}

func (m *Model) moveDown() {
	if m.foc == focusServers {
		if m.serverIdx < m.sidebarCount()-1 {
			m.serverIdx++
			m.onServerChange()
		}
	} else {
		if m.sessionIdx < len(m.rows)-1 {
			m.sessionIdx++
			m.clampScroll()
		}
	}
}

// onServerChange updates which server's sessions are shown. It NEVER hides the
// sidebar (that only happens via the `b` toggle) — a fix for the old behavior
// where selecting a server made the panel vanish.
func (m *Model) onServerChange() {
	if m.serverIdx == 0 {
		m.showRecent = true
		m.curHost = ""
	} else {
		m.showRecent = false
		m.curHost = m.hostList[m.serverIdx-1]
	}
	m.sessionIdx = 0
	m.sessOffset = 0
	m.rebuildRows()
}

func (m *Model) focusServers() {
	// Left also brings a collapsed sidebar back.
	if !m.sidebarOpen {
		m.sidebarOpen = true
	}
	m.foc = focusServers
}

func (m *Model) focusSessions() {
	if len(m.rows) > 0 {
		m.foc = focusSessions
	}
}

func (m *Model) toggleSidebar() {
	m.sidebarOpen = !m.sidebarOpen
	if !m.sidebarOpen {
		m.foc = focusSessions
	} else {
		m.foc = focusServers
	}
}

func (m *Model) toggleTheme() {
	m.dark = !m.dark
	if m.dark {
		m.pal = darkPalette()
	} else {
		m.pal = lightPalette()
	}
}

// ---- actions (attach / split / close) ----

func (m Model) run(alias, remoteCmd string) (tea.Model, tea.Cmd) {
	if alias == "" {
		return m, nil
	}
	if m.simulate {
		// Demo mode: never touch the network. Show what would run.
		m.simMsg = "DEMO — would run:  ssh -t " + alias + " '" + remoteCmd + "'"
		return m, nil
	}
	c := exec.Command("ssh", discover.RunArgs(alias, remoteCmd)...)
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return execDoneMsg{err: err}
	})
}

func (m Model) selectedRow() (sessionRow, bool) {
	if m.foc != focusSessions || m.sessionIdx < 0 || m.sessionIdx >= len(m.rows) {
		return sessionRow{}, false
	}
	r := m.rows[m.sessionIdx]
	if r.kind == rowInfo {
		return sessionRow{}, false
	}
	return r, true // rowTmux, rowAgent, rowLogin are all actionable
}

func (m Model) actionOpen() (tea.Model, tea.Cmd) {
	r, ok := m.selectedRow()
	if !ok {
		// Enter on a server focuses its sessions.
		if m.foc == focusServers {
			m.focusSessions()
		}
		return m, nil
	}
	switch r.kind {
	case rowTmux:
		return m.run(r.host, tmuxctl.AttachExisting(r.tmux.Name))
	case rowAgent:
		return m.run(r.host, tmuxctl.OpenAgent(r.agent))
	case rowLogin:
		// interactive login: attach-or-create the hopmux tmux session. Logging in
		// once warms a ControlMaster so later scans can list sessions silently.
		return m.run(r.host, tmuxctl.AttachSession())
	}
	return m, nil
}

func (m Model) actionSplit(vertical bool) (tea.Model, tea.Cmd) {
	r, ok := m.selectedRow()
	if !ok || r.kind != rowAgent {
		m.statusMsg = "split: pick a Claude/Codex session"
		return m, nil
	}
	return m.run(r.host, tmuxctl.Split(r.agent, vertical))
}

// actionNewShell now opens the new-session picker (directory + agent).
func (m Model) actionNewShell() (tea.Model, tea.Cmd) {
	if m.curHost == "" {
		m.statusMsg = "new session: select a server first"
		return m, nil
	}
	m.nsActive = true
	m.nsAgent = 0
	m.nsSug = m.newSessionDirs()
	m.nsDir = "~"
	m.nsSugIdx = -1
	m.nsTabIdx = 0
	return m, nil
}

// newSessionDirs offers home plus the distinct working directories of sessions
// already on the selected host — the interesting places to start something new.
func (m Model) newSessionDirs() []string {
	dirs := []string{"~"}
	seen := map[string]bool{"~": true}
	if h, ok := m.hosts[m.curHost]; ok {
		for _, a := range h.RecentAgents() {
			if a.CWD != "" && !seen[a.CWD] {
				seen[a.CWD] = true
				dirs = append(dirs, a.CWD)
			}
		}
	}
	return dirs
}

var nsAgentNames = []string{"shell", "claude", "codex"}

// handleNewSessionKey drives the new-session picker.
//
//	Tab      autocomplete the directory (bash-style: longest common prefix,
//	         repeated Tab cycles through matches)
//	← / →    choose the agent (shell / claude / codex)
//	↑ / ↓    browse directory suggestions into the field
//	Enter    start · Esc cancel
func (m Model) handleNewSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.nsActive = false
	case "left":
		m.nsAgent = (m.nsAgent - 1 + len(nsAgentNames)) % len(nsAgentNames)
	case "right":
		m.nsAgent = (m.nsAgent + 1) % len(nsAgentNames)
	case "tab":
		m.autocompleteDir()
	case "up":
		if m.nsSugIdx < 0 {
			m.nsSugIdx = len(m.nsSug) - 1
		} else {
			m.nsSugIdx--
		}
		if m.nsSugIdx >= 0 {
			m.nsDir = m.nsSug[m.nsSugIdx]
		}
	case "down":
		if m.nsSugIdx >= len(m.nsSug)-1 {
			m.nsSugIdx = -1
		} else {
			m.nsSugIdx++
			m.nsDir = m.nsSug[m.nsSugIdx]
		}
	case "backspace":
		m.nsSugIdx = -1
		m.nsTabIdx = 0
		if len(m.nsDir) > 0 {
			r := []rune(m.nsDir)
			m.nsDir = string(r[:len(r)-1])
		}
	case "enter":
		host := m.curHost
		dir := m.nsDir
		agent := ""
		if m.nsAgent > 0 {
			agent = nsAgentNames[m.nsAgent]
		}
		m.nsActive = false
		return m.run(host, tmuxctl.NewSession(dir, agent))
	default:
		if len(msg.Runes) > 0 {
			m.nsSugIdx = -1
			m.nsTabIdx = 0
			m.nsDir += string(msg.Runes)
		}
	}
	return m, nil
}

// autocompleteDir does bash-style Tab completion of the directory field against
// the candidate directories. First Tab fills the longest common prefix of all
// matches; if the field already equals that prefix, repeated Tab cycles through
// the individual matches.
func (m *Model) autocompleteDir() {
	matches := m.dirMatches(m.nsDir)
	if len(matches) == 0 {
		return
	}
	if len(matches) == 1 {
		m.nsDir = matches[0]
		m.nsTabIdx = 0
		return
	}
	lcp := longestCommonPrefix(matches)
	if lcp != "" && lcp != m.nsDir {
		// extend to the shared prefix first
		m.nsDir = lcp
		m.nsTabIdx = 0
		return
	}
	// already at the common prefix → cycle through the candidates
	m.nsDir = matches[m.nsTabIdx%len(matches)]
	m.nsTabIdx++
}

// dirMatches returns candidate dirs that start with the given prefix.
func (m Model) dirMatches(prefix string) []string {
	var out []string
	for _, d := range m.nsSug {
		if prefix == "" || strings.HasPrefix(d, prefix) {
			out = append(out, d)
		}
	}
	return out
}

func longestCommonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
			if p == "" {
				return ""
			}
		}
	}
	return p
}

func (m Model) actionClose() (tea.Model, tea.Cmd) {
	host := m.curHost
	if r, ok := m.selectedRow(); ok && r.host != "" {
		host = r.host
	}
	if host == "" {
		m.statusMsg = "close: select a server"
		return m, nil
	}
	return m.run(host, tmuxctl.ClosePane())
}

// ---- helpers ----

func (m *Model) countScanned() int {
	n := 0
	for _, h := range m.hosts {
		if h.Scanned {
			n++
		}
	}
	return n
}
