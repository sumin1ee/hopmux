package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/isumin/hopmux/core/discover"
	"github.com/isumin/hopmux/core/model"
)

const sidebarWidth = 28

// ---- row building ----

func (m *Model) rebuildRows() {
	m.rows = m.rows[:0]
	if m.showRecent || m.curHost == "" {
		for _, a := range discover.AllRecentAgents(m.hostsSlice(), 60) {
			if !m.match(a.Host + " " + string(a.Agent) + " " + a.Project() + " " + a.Title) {
				continue
			}
			m.rows = append(m.rows, sessionRow{kind: rowAgent, agent: a, host: a.Host})
		}
		if len(m.rows) == 0 {
			msg := "scanning… sessions will appear here"
			if !m.scanning {
				msg = "no sessions to show"
			}
			m.rows = append(m.rows, sessionRow{kind: rowInfo, info: msg})
		}
	} else {
		h, ok := m.hosts[m.curHost]
		if ok && h.AuthRequired {
			// reachable, needs interactive login → a selectable "log in" row
			m.rows = append(m.rows, sessionRow{kind: rowLogin, host: h.Name,
				info: "needs interactive login — press Enter to ssh in\n" +
					"(after logging in once, sessions will list automatically)"})
		} else if !ok || !h.Reachable {
			reason := "not scanned yet"
			if ok && h.Err != "" {
				reason = h.Err
			}
			m.rows = append(m.rows, sessionRow{kind: rowLogin, host: m.curHost,
				info: "unreachable: " + reason + "\npress Enter to try an interactive ssh."})
		} else {
			for _, t := range h.Tmux {
				if m.match("tmux " + t.Name) {
					m.rows = append(m.rows, sessionRow{kind: rowTmux, tmux: t, host: h.Name})
				}
			}
			for _, a := range h.RecentAgents() {
				if m.match(string(a.Agent) + " " + a.Project() + " " + a.Title) {
					m.rows = append(m.rows, sessionRow{kind: rowAgent, agent: a, host: h.Name})
				}
			}
			if len(m.rows) == 0 {
				m.rows = append(m.rows, sessionRow{kind: rowInfo,
					info: "no tmux or agent sessions here yet.\npress n to open a fresh shell."})
			}
		}
	}
	if m.sessionIdx >= len(m.rows) {
		m.sessionIdx = 0
	}
	m.clampScroll()
}

func (m *Model) match(text string) bool {
	if m.filter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(m.filter))
}

func (m *Model) hostsSlice() []model.Host {
	out := make([]model.Host, 0, len(m.hostList))
	for _, name := range m.hostList {
		if h, ok := m.hosts[name]; ok {
			out = append(out, h)
		}
	}
	return out
}

// visibleRows returns how many session rows fit (each row is 2 lines).
func (m *Model) bodyHeight() int {
	// total - header(1) - hint(1) - footer(1)
	h := m.height - 3
	// the GPU line, when shown for a selected host, eats one more line
	if m.showGPU && !m.showRecent && m.curHost != "" {
		if host, ok := m.hosts[m.curHost]; ok && len(host.GPUs) > 0 {
			h--
		}
	}
	if h < 3 {
		h = 3
	}
	return h
}

func (m *Model) clampScroll() {
	perRow := 2
	rowsFit := m.bodyHeight() / perRow
	if rowsFit < 1 {
		rowsFit = 1
	}
	if m.sessionIdx < m.sessOffset {
		m.sessOffset = m.sessionIdx
	}
	if m.sessionIdx >= m.sessOffset+rowsFit {
		m.sessOffset = m.sessionIdx - rowsFit + 1
	}
	if m.sessOffset < 0 {
		m.sessOffset = 0
	}
}

// ---- View ----

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "starting hopmux…"
	}
	p := m.pal
	bg := lipgloss.NewStyle().Background(p.Bg)

	var body string
	if m.sidebarOpen {
		side := m.renderSidebar()
		div := m.renderDivider()
		main := m.renderMain(m.width - sidebarWidth - 1) // -1 for the divider column
		body = lipgloss.JoinHorizontal(lipgloss.Top, side, div, main)
	} else {
		body = m.renderMain(m.width)
	}
	footer := m.renderFooter()
	return bg.Render(lipgloss.JoinVertical(lipgloss.Left, body, footer))
}

// renderDivider is the 1-column vertical rule between the sidebar and main pane,
// giving a clear visual seam (and a natural break for text selection).
func (m Model) renderDivider() string {
	p := m.pal
	h := m.height - 1 // minus footer
	rows := make([]string, h)
	style := lipgloss.NewStyle().Foreground(p.Border).Background(p.Bg)
	for i := range rows {
		rows[i] = style.Render("│")
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// ---- sidebar ----

func (m Model) renderSidebar() string {
	p := m.pal
	h := m.height - 1 // minus footer
	title := lipgloss.NewStyle().
		Foreground(p.Claude).Bold(true).Background(p.Panel).
		Width(sidebarWidth).Render("  SERVERS")

	lines := []string{title}
	rowsAvail := h - 1
	for i := 0; i < m.sidebarCount() && i < rowsAvail; i++ {
		lines = append(lines, m.renderServerRow(i))
	}
	// pad to height
	for len(lines) < h {
		lines = append(lines, lipgloss.NewStyle().Background(p.Surface).Width(sidebarWidth).Render(""))
	}
	col := lipgloss.NewStyle().Background(p.Surface).Width(sidebarWidth).Height(h)
	return col.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m Model) renderServerRow(i int) string {
	p := m.pal
	selected := m.serverIdx == i
	var content string
	if i == 0 {
		content = lipgloss.NewStyle().Foreground(p.Claude).Render("★ ") + "Recent sessions"
	} else {
		name := m.hostList[i-1]
		h, ok := m.hosts[name]
		switch {
		case !ok || !h.Scanned:
			content = lipgloss.NewStyle().Foreground(p.Muted).Render("◌ " + name + " ···")
		case h.AuthRequired:
			// reachable but needs interactive login — amber lock, still openable
			content = lipgloss.NewStyle().Foreground(p.Warning).Render("⚿ " + name)
		case !h.Reachable:
			content = lipgloss.NewStyle().Foreground(p.Danger).Render("✗ " + name)
		case h.SessionCount() > 0:
			dot := lipgloss.NewStyle().Foreground(p.Tmux).Render("● ")
			cnt := lipgloss.NewStyle().Foreground(p.Muted).Render(fmt.Sprintf("  %d", h.SessionCount()))
			content = dot + name + cnt
		default:
			content = lipgloss.NewStyle().Foreground(p.Muted).Render("○ " + name)
		}
	}
	st := lipgloss.NewStyle().Width(sidebarWidth).Padding(0, 1).Background(p.Surface)
	if selected {
		if m.foc == focusServers {
			st = st.Background(p.SelBg).Bold(true)
		} else {
			st = st.Background(p.SelBg)
		}
	}
	return st.Render(content)
}

// ---- main pane ----

func (m Model) renderMain(w int) string {
	p := m.pal
	h := m.height - 1 // minus footer
	title, hint := m.mainHeader()
	titleLine := lipgloss.NewStyle().Background(p.Panel).Foreground(p.Fg).
		Bold(true).Width(w).Render(title)
	var secondLine string
	if m.simMsg != "" {
		// demo banner replaces the hint line while showing
		fg := p.Bg
		if !p.Dark {
			fg = lipgloss.Color("#ffffff")
		}
		secondLine = lipgloss.NewStyle().Background(p.Claude).Foreground(fg).
			Bold(true).Width(w).Render(" " + m.simMsg)
	} else {
		secondLine = lipgloss.NewStyle().Background(p.Bg).Foreground(p.Muted).
			Width(w).Render(hint)
	}

	lines := []string{titleLine, secondLine}
	if m.showGPU && !m.showRecent && m.curHost != "" {
		if host, ok := m.hosts[m.curHost]; ok {
			if gl := m.renderGPULine(host, w); gl != "" {
				lines = append(lines, gl)
			}
		}
	}
	rendered := m.renderSessionLines(w)
	lines = append(lines, rendered...)

	// pad
	bodyStyle := lipgloss.NewStyle().Background(p.Bg).Width(w)
	for lipgloss.Height(strings.Join(lines, "\n")) < h {
		lines = append(lines, bodyStyle.Render(""))
	}
	if m.filtering {
		lines[len(lines)-1] = lipgloss.NewStyle().Background(p.Bg).Foreground(p.Codex).
			Width(w).Render(" / " + m.filter + "▏")
	}
	if m.nsActive {
		panel := m.renderNewSessionPanel(w)
		pl := strings.Count(panel, "\n") + 1
		// replace the last pl lines with the panel
		for i := 0; i < pl && len(lines) > 0; i++ {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, panel)
	}
	return lipgloss.NewStyle().Background(p.Bg).Width(w).Height(h).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// renderNewSessionPanel draws the "new session" picker (directory + agent).
func (m Model) renderNewSessionPanel(w int) string {
	p := m.pal
	label := lipgloss.NewStyle().Foreground(p.Bg).Background(p.Codex).Bold(true)
	dim := lipgloss.NewStyle().Foreground(p.Muted)
	acc := lipgloss.NewStyle().Foreground(p.Codex)

	// agent selector: [shell] claude codex
	var ag []string
	for i, name := range nsAgentNames {
		if i == m.nsAgent {
			ag = append(ag, lipgloss.NewStyle().Foreground(p.Claude).Bold(true).Render("["+name+"]"))
		} else {
			ag = append(ag, dim.Render(name))
		}
	}
	head := " " + label.Render(" NEW SESSION ") + "  on " +
		acc.Render(m.curHost) + "    " + strings.Join(ag, " ") + dim.Render("  (←→ agent)")

	caret := ""
	if m.nsSugIdx < 0 {
		caret = "▏"
	}
	dirLine := " dir " + acc.Render("▸ ") + m.nsDir + caret + dim.Render("   (Tab: complete)")

	// suggestions row
	var sug []string
	for i, d := range m.nsSug {
		s := d
		if i == m.nsSugIdx {
			s = lipgloss.NewStyle().Foreground(p.Bg).Background(p.Claude).Render(" " + d + " ")
		} else {
			s = dim.Render(d)
		}
		sug = append(sug, s)
	}
	sugLine := "     " + dim.Render("↑↓ ") + strings.Join(sug, dim.Render("  "))
	foot := dim.Render(" Enter start · Esc cancel")

	full := lipgloss.NewStyle().Background(p.Surface).Width(w)
	return lipgloss.JoinVertical(lipgloss.Left,
		full.Render(head), full.Render(dirLine), full.Render(sugLine), full.Render(foot))
}

func (m Model) mainHeader() (string, string) {
	p := m.pal
	if m.showRecent || m.curHost == "" {
		muted := lipgloss.NewStyle().Foreground(p.Muted)
		return "★ Recent sessions " + muted.Render("· across all servers"),
			fmt.Sprintf("pick where to jump back in · %d/%d servers scanned", m.scanned, len(m.hostList))
	}
	h, ok := m.hosts[m.curHost]
	if !ok {
		return m.curHost, "…"
	}
	nc, nx := 0, 0
	for _, a := range h.Agents {
		if a.Agent == model.Claude {
			nc++
		} else if a.Agent == model.Codex {
			nx++
		}
	}
	sub := lipgloss.NewStyle().Foreground(p.Muted).Render("  " + h.Hostname)
	return m.curHost + sub,
		fmt.Sprintf("%d tmux · %d claude · %d codex   —  Enter open · s split · x close",
			len(h.Tmux), nc, nx)
}

func (m Model) renderSessionLines(w int) []string {
	var out []string
	perRow := 2
	rowsFit := m.bodyHeight() / perRow
	if rowsFit < 1 {
		rowsFit = 1
	}
	end := m.sessOffset + rowsFit
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := m.sessOffset; i < end; i++ {
		r := m.rows[i]
		sel := m.foc == focusSessions && i == m.sessionIdx
		out = append(out, m.renderRow(r, sel, w)...)
	}
	return out
}

// renderRow renders one session as two lines (title + path·time), colored.
func (m Model) renderRow(r sessionRow, sel bool, w int) []string {
	p := m.pal
	rowBg := p.Bg
	if sel {
		rowBg = p.SelBg
	}
	line := lipgloss.NewStyle().Background(rowBg).Width(w)

	if r.kind == rowInfo || r.kind == rowLogin {
		fg := p.Muted
		if r.kind == rowLogin {
			fg = p.Warning
		}
		l := lipgloss.NewStyle().Background(rowBg).Foreground(fg).Width(w)
		parts := strings.SplitN(r.info, "\n", 2)
		lead := " "
		if r.kind == rowLogin {
			lead = " ⚿ "
		}
		res := []string{l.Render(lead + parts[0])}
		if len(parts) > 1 {
			res = append(res, l.Render("   "+parts[1]))
		}
		return res
	}

	if r.kind == rowTmux {
		t := r.tmux
		tag := lipgloss.NewStyle().Foreground(p.Tmux).Bold(true).Render("▣ tmux ")
		name := lipgloss.NewStyle().Bold(true).Foreground(p.Fg).Render(t.Name)
		l1 := line.Render(" " + tag + name)
		state := lipgloss.NewStyle().Foreground(p.Muted).Render("detached")
		if t.Attached {
			state = lipgloss.NewStyle().Foreground(p.Tmux).Render("attached")
		}
		l2 := line.Render("    " + lipgloss.NewStyle().Foreground(p.Muted).
			Render(fmt.Sprintf("%s windows · ", t.Windows)) + state)
		return []string{l1, l2}
	}

	a := r.agent
	col := p.AgentColor(string(a.Agent))
	label := "claude"
	if a.Agent == model.Codex {
		label = "codex"
	}
	tag := lipgloss.NewStyle().Foreground(col).Bold(true).Render("◉ " + label + " ")
	hostPrefix := ""
	if m.showRecent {
		hostPrefix = lipgloss.NewStyle().Foreground(p.Muted).Render("["+r.host+"] ")
	}
	title := a.Title
	if strings.TrimSpace(title) == "" {
		title = lipgloss.NewStyle().Foreground(p.Muted).Italic(true).Render("(no prompt yet)")
	} else {
		title = lipgloss.NewStyle().Foreground(p.Fg).Render(title)
	}
	l1 := line.Render(" " + tag + hostPrefix + title)

	path := a.CWD
	if path == "" {
		path = "~"
	}
	pathS := lipgloss.NewStyle().Foreground(col).Render(path)
	timeS := lipgloss.NewStyle().Foreground(p.Muted).Render(" · " + relTime(a.MTime, m.now))
	l2 := line.Render("    " + pathS + timeS)
	// truncate lines to width to avoid wrapping
	return []string{truncateTo(l1, w, rowBg, p), truncateTo(l2, w, rowBg, p)}
}

func (m Model) renderFooter() string {
	p := m.pal
	keys := []struct{ k, d string }{
		{"↵", "open"}, {"s", "split"}, {"x", "close"}, {"n", "shell"},
		{"g", "gpu"}, {"r", "rescan"}, {"d", "theme"}, {"b", "sidebar"}, {"/", "filter"}, {"q", "quit"},
	}
	var parts []string
	kb := lipgloss.NewStyle().Foreground(p.Codex).Bold(true)
	db := lipgloss.NewStyle().Foreground(p.Muted)
	for _, x := range keys {
		parts = append(parts, kb.Render(x.k)+" "+db.Render(x.d))
	}
	left := strings.Join(parts, "  ")
	if m.statusMsg != "" {
		left = lipgloss.NewStyle().Foreground(p.Claude).Render(m.statusMsg)
	}
	return lipgloss.NewStyle().Background(p.Panel).Width(m.width).Padding(0, 1).Render(left)
}

// ---- small helpers ----

// gpuLoadColor picks green (idle) → yellow → red (busy) for a 0..100 load.
func (m Model) gpuLoadColor(pct int) lipgloss.Color {
	switch {
	case pct >= 80:
		return m.pal.Danger
	case pct >= 40:
		return m.pal.Claude // warm/amber-ish
	default:
		return m.pal.Tmux
	}
}

// renderGPULine renders one host's GPUs as a compact colored line, or "" if none.
func (m Model) renderGPULine(h model.Host, w int) string {
	if len(h.GPUs) == 0 {
		return ""
	}
	p := m.pal
	var segs []string
	for _, g := range h.GPUs {
		// pick the higher of compute/mem load for the color signal
		load := g.Util
		if mp := g.MemPct(); mp > load {
			load = mp
		}
		col := m.gpuLoadColor(load)
		bar := miniBar(load, 6)
		seg := lipgloss.NewStyle().Foreground(col).Render(
			fmt.Sprintf("GPU%d %s %d%%", g.Index, bar, g.Util))
		mem := lipgloss.NewStyle().Foreground(p.Muted).Render(
			fmt.Sprintf(" %.0f/%.0fG", float64(g.MemUsed)/1024, float64(g.MemTotal)/1024))
		segs = append(segs, seg+mem)
	}
	line := " " + strings.Join(segs, lipgloss.NewStyle().Foreground(p.Muted).Render("  ·  "))
	return lipgloss.NewStyle().Background(p.Surface).Foreground(p.Fg).Width(w).Render(line)
}

// miniBar draws a small proportional bar like ▓▓▓░░░ for pct in 0..100.
func miniBar(pct, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * width / 100
	return strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
}

func relTime(epoch, now int64) string {
	if epoch == 0 {
		return "?"
	}
	d := now - epoch
	if d < 0 {
		d = 0
	}
	switch {
	case d < 60:
		return fmt.Sprintf("%ds", d)
	case d < 3600:
		return fmt.Sprintf("%dm", d/60)
	case d < 86400:
		return fmt.Sprintf("%dh", d/3600)
	case d < 86400*30:
		return fmt.Sprintf("%dd", d/86400)
	default:
		return fmt.Sprintf("%dmo", d/(86400*30))
	}
}

// truncateTo hard-caps a rendered line to width (Lipgloss handles wide runes).
func truncateTo(s string, w int, bg lipgloss.Color, p Palette) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return lipgloss.NewStyle().Background(bg).MaxWidth(w).Render(s)
}
