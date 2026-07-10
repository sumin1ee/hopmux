package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/isumin/hopmux/core/mcp"
)

//go:embed all:frontend/dist
var assets embed.FS

const version = "0.3.0"

func main() {
	// `hopmux mcp` — run as a headless MCP server instead of the GUI, so the
	// INSTALLED app is also the orchestration backend:
	//
	//	claude mcp add hopmux -- "C:\...\hopmux.exe" mcp
	//
	// This works from a GUI-subsystem exe because the MCP client (Claude Code)
	// spawns us with stdio pipes; no console is needed.
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if err := mcp.Run(version); err != nil {
			fmt.Fprintln(os.Stderr, "hopmux mcp:", err)
			os.Exit(1)
		}
		return
	}

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
