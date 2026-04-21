package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Windsurf Proxy",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		Bind: []interface{}{app},
		Mac: &mac.Options{
			// Custom title bar: transparent + full-size content so the
			// sidebar extends to the very top. Crucially we keep
			// UseToolbar=false, which drops the NSToolbar wrapper and
			// lets the red/yellow/green buttons sit in the standard
			// (higher) title-bar slot instead of being vertically
			// centered inside a taller toolbar strip.
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: true,
				HideTitle:                  true,
				HideTitleBar:               false,
				FullSizeContent:            true,
				UseToolbar:                 false,
				HideToolbarSeparator:       true,
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}