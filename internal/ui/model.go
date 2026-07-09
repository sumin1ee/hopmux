// Package ui is the Bubble Tea front-end for hopmux.
//
// Layout: left sidebar = SSH servers, right = the selected server's sessions
// (live tmux + resumable Claude/Codex), or a cross-host "recent" view when the
// ★ Recent entry is selected. Opening/splitting a session hands the terminal to
// tmux on the remote via tea.ExecProcess and restores the TUI on return.
package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/isumin/hopmux/core/discover"
	"github.com/isumin/hopmux/core/model"
)

type focus int

const (
	focusServers focus = iota
	focusSessions
)

// rowKind distinguishes the two things that appear in the session list.
type rowKind int

const (
	rowTmux rowKind = iota
	rowAgent
	rowInfo  // non-selectable message (empty state)
	rowLogin // selectable: Enter opens an interactive ssh login to host
)

type sessionRow struct {
	kind  rowKind
	tmux  model.TmuxSession
	agent model.AgentSession
	host  string // which host this session lives on (for the recent view)
	info  string
}

// Model is the whole application state.
type Model struct {
	backend  discover.Backend
	hostList []string // aliases in config order

	hosts   map[string]model.Host
	now     int64
	scanned int

	// selection / focus
	serverIdx  int // index into the sidebar (0 = ★ Recent)
	sessionIdx int
	foc        focus

	showRecent  bool
	curHost     string
	rows        []sessionRow
	sessOffset  int

	// chrome
	sidebarOpen bool
	showGPU     bool // 'g' toggles a GPU utilization line per host
	pal         Palette
	dark        bool
	filtering   bool
	filter      string

	// new-session flow ('n'): pick a directory + agent, then start it.
	nsActive bool
	nsDir    string   // editable directory buffer
	nsAgent  int      // 0=shell 1=claude 2=codex
	nsSug    []string // candidate directories (home + known project dirs)
	nsSugIdx int      // highlighted suggestion (-1 = editing the text field)
	nsTabIdx int      // Tab-completion cycle position

	width, height int
	scanning      bool
	quitting      bool
	statusMsg     string

	// simulate=true (demo mode) means "open/split/close" must NOT run real ssh —
	// it shows the command that would run instead. Critical: without this, the
	// demo fires real connections to your servers.
	simulate bool
	simMsg   string // banner shown after a simulated action; cleared on next key
}

// WithSimulate marks the model as demo/simulation mode (no real ssh on attach).
func (m Model) WithSimulate() Model { m.simulate = true; return m }

// recentLabel is the synthetic first sidebar entry.
const recentLabel = "★ Recent sessions"

// New builds the initial model. hostList is the config-ordered aliases.
func New(backend discover.Backend, hostList []string) Model {
	return Model{
		backend:     backend,
		hostList:    hostList,
		hosts:       map[string]model.Host{},
		now:         discover.NowEpoch(),
		serverIdx:   0,
		foc:         focusServers,
		showRecent:  true,
		sidebarOpen: true,
		dark:        true,
		pal:         darkPalette(),
		scanning:    true,
	}
}

// ----- messages -----

type hostUpdatedMsg struct {
	host   model.Host
	stream *discoveryStream
}
type scanDoneMsg struct{ hosts []model.Host }

// discoveryStream carries the discovery goroutine's streamed results. It lives in
// command closures and messages, not on the model (Bubble Tea copies the model).
type discoveryStream struct {
	ch   chan model.Host
	done chan []model.Host
}

func (m Model) Init() tea.Cmd {
	return m.startScan()
}
