//go:build !windows && !darwin

package main

// На платформах без системного трея (Linux и пр.) все функции — no-op.
// Интерфейс сохранён, чтобы app.go собирался на любой ОС.

func trayInit(a *App)         {}
func trayRemove(a *App)       {}
func trayUpdateStatus(a *App) {}
func trayActivateApp()        {}
func trayGetApp() *App        { return nil }
