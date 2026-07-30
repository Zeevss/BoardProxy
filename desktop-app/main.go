package main

import (
	"embed"
	"log"
	"os"

	"boardproxy-desktop/internal/helper"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// helperFlag — маркер режима привилегированного демона в том же бинаре. GUI
// запускает себя как `<self> --helper <bootstrap>` с повышением прав.
const helperFlag = "--helper"

func main() {
	// Режим helper: тот же бинарь, но без GUI (см. internal/helper).
	if len(os.Args) > 1 && os.Args[1] == helperFlag {
		if len(os.Args) < 3 {
			log.Fatal("helper: missing bootstrap path")
		}
		if err := helper.Run(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}

	app := NewApp()
	startTray, stopTray := setupTray(app)
	startTray()
	defer stopTray()

	err := wails.Run(&options.App{
		Title:     "BoardProxy",
		Width:     980,
		Height:    840,
		MinWidth:  820,
		MinHeight: 620,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour:  &options.RGBA{R: 13, G: 17, B: 23, A: 1},
		HideWindowOnClose: true,
		OnStartup:         app.startup,
		OnShutdown:        app.shutdown,
		// Один экземпляр: повторный запуск бинаря не поднимает второй инстанс, а
		// показывает окно уже работающего (тот в трее). helper-режим сюда не
		// попадает — он выходит из main раньше.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "ru.zevsus.boardproxy",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				app.showWindow()
			},
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Print(err)
	}
}
