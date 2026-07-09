package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/isumin/hopmux/core/discover"
	"github.com/isumin/hopmux/core/model"
)

func init() {
	// Tests don't run under a TTY, so Lipgloss would strip color and make every
	// theme render identically. Force truecolor so color-dependent assertions
	// (e.g. the theme toggle) actually see ANSI.
	lipgloss.SetColorProfile(termenv.TrueColor)
}

func demoHosts() []string {
	return []string{"ml-train-01", "prod-api", "research-box", "gpu-node-2",
		"staging", "db-primary", "edge-01", "old-box"}
}

// driveFinal builds a model, applies a completed scan, sends the given keys via
// Update directly, and returns View() of the FINAL frame. This is deterministic
// (no streaming timing) and — unlike teatest's cumulative output — reflects only
// the last rendered state, which is what absence-assertions need.
func driveFinal(t *testing.T, keys []string) string {
	t.Helper()
	be := discover.NewMock(1)
	be.Fast = true
	var mm tea.Model = New(be, demoHosts())
	mm, _ = mm.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	// Inject a completed scan so the sidebar/sessions are populated.
	hosts := be.Discover(demoHosts(), nil)
	mm, _ = mm.Update(scanDoneMsg{hosts: hosts})

	for _, k := range keys {
		var msg tea.Msg
		switch k {
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		mm, _ = mm.Update(msg)
	}
	return mm.View()
}

func TestRecentViewShowsCrossHostSessions(t *testing.T) {
	out := driveFinal(t, nil) // default: ★ Recent
	for _, want := range []string{"SERVERS", "Recent sessions", "claude", "codex", "ai-agent"} {
		if !strings.Contains(out, want) {
			t.Errorf("recent view missing %q", want)
		}
	}
}

func TestSelectServerShowsItsSessions(t *testing.T) {
	out := driveFinal(t, []string{"down"}) // → ml-train-01
	for _, want := range []string{"tmux", "train", "ai-agent", "data-pipeline"} {
		if !strings.Contains(out, want) {
			t.Errorf("ml-train-01 view missing %q", want)
		}
	}
}

func TestSidebarToggleHidesAndRestores(t *testing.T) {
	if with := driveFinal(t, nil); !strings.Contains(with, "SERVERS") {
		t.Fatal("sidebar should be visible by default")
	}
	if hidden := driveFinal(t, []string{"b"}); strings.Contains(hidden, "SERVERS") {
		t.Error("'b' should hide the sidebar, but SERVERS still present")
	}
	if restored := driveFinal(t, []string{"b", "b"}); !strings.Contains(restored, "SERVERS") {
		t.Error("second 'b' should restore the sidebar")
	}
}

func TestSelectingServerDoesNotHideSidebar(t *testing.T) {
	// The old (Python) bug: choosing a server made the panel vanish. Must stay.
	out := driveFinal(t, []string{"down", "down", "down"})
	if !strings.Contains(out, "SERVERS") {
		t.Error("navigating servers must NOT hide the sidebar")
	}
}

func TestThemeToggleChangesColors(t *testing.T) {
	dark := driveFinal(t, nil)
	light := driveFinal(t, []string{"d"})
	if dark == light {
		t.Error("'d' should change the rendered colors (dark vs light)")
	}
}

func TestFilterNarrowsSessions(t *testing.T) {
	// go to ml-train-01, filter for "codex" → only the codex session should remain
	all := driveFinal(t, []string{"down"})
	filtered := driveFinal(t, []string{"down", "/", "c", "o", "d", "e", "x"})
	if !strings.Contains(all, "tensorboard") {
		t.Fatal("expected tmux 'download' before filtering")
	}
	if strings.Contains(filtered, "tensorboard") {
		t.Error("filter 'codex' should have hidden the tmux 'download' row")
	}
	if !strings.Contains(filtered, "data-pipeline") {
		t.Error("filter 'codex' should keep the codex session")
	}
}

// smoke: an offline host renders its reason without crashing.
func TestUnreachableHostRendersReason(t *testing.T) {
	// index: recent(0) ml(1) prod(2) research(3) gpu2(4) staging(5) db-primary(6) edge-01(7)
	out := driveFinal(t, []string{"down", "down", "down", "down", "down", "down", "down"})
	if !strings.Contains(out, "unreachable") && !strings.Contains(out, "timed out") &&
		!strings.Contains(out, "login") {
		t.Errorf("expected a reason note, got:\n%s", firstLines(out, 6))
	}
}

func firstLines(s string, n int) string {
	parts := strings.SplitN(s, "\n", n+1)
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, "\n")
}

var _ = model.Claude // keep model import if unused above

func TestGPUToggle(t *testing.T) {
	// go to ml-train-01 (has GPUs). Without 'g' no GPU%; with 'g' the util shows.
	off := driveFinal(t, []string{"down"})
	on := driveFinal(t, []string{"down", "g"})
	if strings.Contains(off, "94%") {
		t.Error("GPU line should be hidden until toggled")
	}
	if !strings.Contains(on, "94%") || !strings.Contains(on, "GPU0") {
		t.Errorf("'g' should reveal GPU utilization, got:\n%s", firstLines(on, 4))
	}
}

func TestNewSessionPicker(t *testing.T) {
	// n on ml-train-01 opens the picker with directory suggestions from its sessions
	out := driveFinal(t, []string{"down", "n"})
	for _, want := range []string{"NEW SESSION", "ml-train-01", "ai-agent", "shell", "claude", "codex"} {
		if !strings.Contains(out, want) {
			t.Errorf("new-session panel missing %q", want)
		}
	}
	// Esc cancels the picker
	cancelled := driveFinal(t, []string{"down", "n", "esc"})
	if strings.Contains(cancelled, "NEW SESSION") {
		t.Error("Esc should close the new-session picker")
	}
}

func TestDividerPresent(t *testing.T) {
	out := driveFinal(t, nil)
	if !strings.Contains(out, "│") {
		t.Error("expected a vertical divider between sidebar and main pane")
	}
}

func TestNewSessionTabComplete(t *testing.T) {
	be := discover.NewMock(1); be.Fast = true
	var mm tea.Model = New(be, demoHosts())
	mm, _ = mm.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	mm, _ = mm.Update(scanDoneMsg{hosts: be.Discover(demoHosts(), nil)})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyDown})  // ml-train-01
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}) // open picker
	// clear "~" and type a prefix shared by that host's dirs: "/home/dev/a"
	for i := 0; i < 1; i++ { mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyBackspace}) }
	for _, r := range "/home/dev/a" { mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}) }
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyTab})  // autocomplete
	m := mm.(Model)
	if m.nsDir != "/home/dev/ai-agent" {
		t.Errorf("Tab should complete to the only match, got %q", m.nsDir)
	}
	// arrow-right changes agent shell->claude
	before := m.nsAgent
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRight})
	if mm.(Model).nsAgent == before {
		t.Error("right arrow should change the agent")
	}
}
