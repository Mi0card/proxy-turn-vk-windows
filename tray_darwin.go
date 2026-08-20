//go:build darwin

package main

import (
	"sync"

	"github.com/progrium/darwinkit/dispatch"
	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/objc"
)

// ── Состояние трея ───────────────────────────────────────────────────────────

var (
	trayMu             sync.Mutex
	trayApp            *App
	trayStatusItem     *appkit.StatusItem
	trayStatusMenuItem appkit.MenuItem
	trayInitialized    bool
)

// ── Публичный API (общий для всех платформ) ──────────────────────────────────

// trayInit создаёт NSStatusItem в меню-баре. Все вызовы AppKit обязаны
// выполняться на главном потоке (run loop принадлежит Wails), поэтому создание
// уходит в dispatch.MainQueue, а вызывающая горутина ждёт готовности.
//
// НЕ ПРОВЕРЕНО СБОРКОЙ НА РЕАЛЬНОМ MAC — сигнатуры сверены с darwinkit v0.5.0
// (appkit_custom.go, status_item.gen.go, menu.gen.go, menu_item.gen.go,
// image.gen.go, status_bar_button.gen.go), рантайм нужно перепроверить.
func trayInit(a *App) {
	trayMu.Lock()
	trayApp = a
	trayMu.Unlock()

	ready := make(chan struct{})
	dispatch.MainQueue().DispatchAsync(func() {
		item := appkit.StatusBar_SystemStatusBar().StatusItemWithLength(appkit.VariableStatusItemLength)
		objc.Retain(&item)

		// Иконка из embedded PNG (build/windows/icon.ico). NSImage умеет
		// PNG напрямую; SetTemplate(true) включает автоматический
		// перекрас под светлую/тёмную строку меню.
		if png, err := trayIconPNG(); err == nil {
			img := appkit.NewImageWithData(png)
			img.SetTemplate(true)
			item.Button().SetImage(img)
		}
		item.Button().SetToolTip("WinDTT")

		menu := appkit.NewMenu()
		statusItem := appkit.NewMenuItemWithTitleActionKeyEquivalent("", objc.Selector{}, "")
		statusItem.SetEnabled(false)
		menu.AddItem(statusItem)
		menu.AddItem(appkit.MenuItem_SeparatorItem())
		menu.AddItem(appkit.NewMenuItemWithAction("Показать окно", "", func(_ objc.Object) {
			trayShowAction()
		}))
		menu.AddItem(appkit.MenuItem_SeparatorItem())
		menu.AddItem(appkit.NewMenuItemWithAction("Выход", "", func(_ objc.Object) {
			trayQuitAction()
		}))
		item.SetMenu(menu)

		trayMu.Lock()
		trayStatusItem = &item
		trayStatusMenuItem = statusItem
		trayInitialized = true
		trayMu.Unlock()

		trayUpdateStatus(a)
		close(ready)
	})
	<-ready
}

func trayGetApp() *App {
	trayMu.Lock()
	defer trayMu.Unlock()
	return trayApp
}

// trayRemove убирает иконку из меню-бара. Вызывается из shutdown.
func trayRemove(a *App) {
	trayMu.Lock()
	item := trayStatusItem
	trayStatusItem = nil
	trayStatusMenuItem = appkit.MenuItem{}
	trayInitialized = false
	trayApp = nil
	trayMu.Unlock()

	if item == nil {
		return
	}
	dispatch.MainQueue().DispatchAsync(func() {
		appkit.StatusBar_SystemStatusBar().RemoveStatusItem(*item)
	})
}

// trayUpdateStatus обновляет строку статуса в меню и tooltip иконки.
func trayUpdateStatus(a *App) {
	trayMu.Lock()
	initialized := trayInitialized
	item := trayStatusItem
	trayMu.Unlock()
	if !initialized || item == nil {
		return
	}
	dispatch.MainQueue().DispatchAsync(func() {
		trayMu.Lock()
		if trayStatusMenuItem.Ptr() != 0 {
			text := "WinDTT"
			if trayApp != nil {
				text = trayApp.trayStatusText()
			}
			trayStatusMenuItem.SetTitle(text)
			(*trayStatusItem).Button().SetToolTip(text)
		}
		trayMu.Unlock()
	})
}

// trayActivateApp переводит приложение в обычный режим и выводит на передний
// план — нужно при разворачивании окна из трея (в Accessory-режиме окно не
// получит фокус).
func trayActivateApp() {
	dispatch.MainQueue().DispatchAsync(func() {
		app := appkit.Application_SharedApplication()
		app.SetActivationPolicy(appkit.ApplicationActivationPolicyRegular)
		app.ActivateIgnoringOtherApps(true)
	})
}
