// Command app is the Fragile desktop app (Wails).
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// frontend/dist holds a tracked placeholder so this compiles before the
// frontend is built; `wails build` fills it in.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "Fragile",
		Width:            1200,
		Height:           800,
		MinWidth:         800,
		MinHeight:        500,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 31, G: 31, B: 30, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		// Wails disables the green zoom/fullscreen button when Mac options are nil.
		Mac: &mac.Options{},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
