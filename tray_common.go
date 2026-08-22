package main

// trayGetApp возвращает указатель на App, зарегистрированный в trayInit.
// Реализация зависит от платформы (см. tray_windows.go / tray_darwin.go).

func trayShowAction() {
	if a := trayGetApp(); a != nil {
		a.showWindowFromTray()
	}
}

func trayQuitAction() {
	if a := trayGetApp(); a != nil {
		a.quitApp()
	}
}
