package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed frontend
var assets embed.FS

// wdtt-client.exe вшивается при сборке через go:embed.
// В репо лежит 64-байтная MZ-заглушка; CI перезаписывает её
// настоящим бинарником перед wails build.
//
//go:embed wdtt-client.exe
var clientExeData []byte

// wdtt-server — серверный бинарник Linux amd64.
// В репо лежит ELF-заглушка; CI собирает настоящий из server_src/.
//
//go:embed assets/server/wdtt-server
var serverBinaryData []byte

// deploy.sh — установочный скрипт для VPS, зеркалируется из upstream
// через sync.yml. Раньше качался с raw.githubusercontent.com на каждый
// клик Deploy/Удалить — теперь встроен, деплой не зависит от сети в моменте.
//
//go:embed assets/deploy.sh
var deployScriptData []byte

func main() {
	// Распаковываем wdtt-client.exe во временный файл
	tmpExe, err := extractClientExe(clientExeData)
	if err != nil {
		tmpExe = ""
	}

	if tmpExe != "" {
		defer os.Remove(tmpExe)
	}

	app := NewAppWithExe(tmpExe)
	app.serverBinary = serverBinaryData
	app.deployScript = deployScriptData

	wailsErr := wails.Run(&options.App{
		Title:     "WinDTT  v" + AppVersion,
		Width:     1050,
		Height:    640,
		MinWidth:  980,
		MinHeight: 580,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 11, G: 18, B: 32, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			Theme:                windows.SystemDefault,
		},
	})

	if wailsErr != nil {
		panic(wailsErr)
	}
}
