package ui

import "github.com/charmbracelet/lipgloss"

// Palette holds every color the UI uses, resolved for one mode (dark or light).
// Agent accent roles stay consistent across modes; only surfaces/foreground flip.
type Palette struct {
	Dark bool

	Fg       lipgloss.Color // primary text
	Muted    lipgloss.Color // secondary text
	Bg       lipgloss.Color // window background
	Surface  lipgloss.Color // sidebar background
	Panel    lipgloss.Color // header/footer background
	Border   lipgloss.Color

	Claude  lipgloss.Color // coral
	Codex   lipgloss.Color // cyan
	Tmux    lipgloss.Color // green
	Danger  lipgloss.Color // red
	Warning lipgloss.Color // amber (needs-auth)
	SelBg   lipgloss.Color // selection background
}

func darkPalette() Palette {
	return Palette{
		Dark:    true,
		Fg:      "#e6e6e6",
		Muted:   "#8b95a3",
		Bg:      "#0d1017",
		Surface: "#12161f",
		Panel:   "#171c26",
		Border:  "#2a313c",
		Claude:  "#d97757",
		Codex:   "#2bb6c4",
		Tmux:    "#3fb950",
		Danger:  "#f85149",
		Warning: "#d6a15e",
		SelBg:   "#243040",
	}
}

func lightPalette() Palette {
	return Palette{
		Dark:    false,
		Fg:      "#1c2128",
		Muted:   "#57606a",
		Bg:      "#fbfbfa",
		Surface: "#f2f1ee",
		Panel:   "#e9e8e4",
		Border:  "#d7d5d0",
		Claude:  "#c25b3a",
		Codex:   "#0e8fa0",
		Tmux:    "#1a7f37",
		Danger:  "#cf222e",
		Warning: "#9a6a1f",
		SelBg:   "#e2dfd8",
	}
}

// AgentColor returns the accent for a given agent name.
func (p Palette) AgentColor(agent string) lipgloss.Color {
	switch agent {
	case "claude":
		return p.Claude
	case "codex":
		return p.Codex
	default:
		return p.Tmux
	}
}
