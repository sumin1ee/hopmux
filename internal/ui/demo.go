package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/isumin/hopmux/core/discover"
	"github.com/isumin/hopmux/core/model"
)

// RenderFrame builds a model over the given hosts, applies a completed scan of
// `hosts`, replays the key sequence, and returns the rendered View. It is used
// to generate documentation screenshots and to eyeball the UI without a TTY.
//
// keys understands: "down","up","enter","tab","b","d","/","<rune>".
func RenderFrame(width, height int, hostList []string, scanned []model.Host, keys []string, simulate bool) string {
	base := New(mockish{}, hostList)
	if simulate {
		base = base.WithSimulate()
	}
	var mm tea.Model = base
	mm, _ = mm.Update(tea.WindowSizeMsg{Width: width, Height: height})
	mm, _ = mm.Update(scanDoneMsg{hosts: scanned})
	for _, k := range keys {
		var msg tea.Msg
		switch k {
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		mm, _ = mm.Update(msg)
	}
	return mm.View()
}

// mockish is a no-op backend; RenderFrame injects the scan directly.
type mockish struct{}

func (mockish) Discover(hosts []string, onUpdate discover.Update) []model.Host { return nil }
