package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "hopmux",
		Width:     1100,
		Height:    720,
		MinWidth:  700,
		MinHeight: 400,
		// match the TUI's dark charcoal so there's no flash of a different color
		BackgroundColour: &options.RGBA{R: 0x0d, G: 0x10, B: 0x17, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		Bind:             []interface{}{app},
		// Elements with `--wails-draggable: drag` become window drag handles.
		CSSDragProperty: "--wails-draggable",
		CSSDragValue:    "drag",
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(), // clean native integrated title bar
			Appearance: mac.NSAppearanceNameDarkAqua,
			About: &mac.AboutInfo{
				Title:   "hopmux",
				Message: "Hop across your SSH servers and resume any Claude Code / Codex session.",
			},
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
