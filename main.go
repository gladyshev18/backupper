package main

import (
	"embed"
	"os/exec"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"backupper/internal/desktop"
)

//go:embed all:frontend/dist
var assets embed.FS

func runtimeOpenExplorer(path string) error {
	return exec.Command("explorer", "/select,", path).Start()
}

func main() {
	store, err := desktop.OpenStore()
	if err != nil {
		println("Error:", err.Error())
		return
	}
	app := NewApp(store)

	err = wails.Run(&options.App{
		Title:     "Backupper",
		Width:     1280,
		Height:    820,
		MinWidth:  980,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "backupper-panel-6f1c2a",
		},
		Bind: []interface{}{app},
		Windows: &windows.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			BackdropType:         windows.Mica,
			Theme:                windows.SystemDefault,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
