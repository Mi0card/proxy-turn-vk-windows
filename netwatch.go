package main

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

const (
	// netPollInterval — частота опроса сетевых интерфейсов.
	netPollInterval = 5 * time.Second
	// netCooldown — пауза между перезапусками, инициированными сменой сети,
	// защита от шторма при «мерцающих» интерфейсах (флэп/док-станция).
	netCooldown = 30 * time.Second
)

// Injectable для тестов.
var (
	netInterfacesFn = net.Interfaces
	netAddrsFn      = func(i net.Interface) ([]net.Addr, error) { return i.Addrs() }
)

// netSignature строит детерминированную сигнатуру сетевых интерфейсов:
// для каждого не-loopback интерфейса — имя, состояние и отсортированные адреса.
// Сортировка устраняет ложные срабатывания из-за порядка срезов ОС.
func netSignature() (string, error) {
	ifs, err := netInterfacesFn()
	if err != nil {
		return "", err
	}

	entries := make([]string, 0, len(ifs))
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := netAddrsFn(ifc)
		if err != nil {
			continue // интерфейс с ошибкой пропускаем, остальные учитываем
		}
		addrStrs := make([]string, 0, len(addrs))
		for _, a := range addrs {
			addrStrs = append(addrStrs, a.String())
		}
		sort.Strings(addrStrs)
		entries = append(entries, fmt.Sprintf("%s|%t|%t|%s",
			ifc.Name, ifc.Flags&net.FlagUp != 0, ifc.Flags&net.FlagRunning != 0,
			strings.Join(addrStrs, ",")))
	}
	sort.Strings(entries)
	return strings.Join(entries, ";"), nil
}

// startNetMonitor поллит сетевые интерфейсы каждые netPollInterval; при смене
// сигнатуры вызывает onNetworkChange. Первая выборка только инициализирует базу —
// старт приложения не считается сменой сети.
func (a *App) startNetMonitor(stop chan struct{}) {
	ticker := time.NewTicker(netPollInterval)
	defer ticker.Stop()

	prev, err := netSignature()
	if err != nil {
		prev = ""
	}
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			cur, err := netSignature()
			if err != nil {
				continue // временная ошибка — не детектируем, следующий тик увидит реальную смену
			}
			if prev != "" && cur != prev {
				a.onNetworkChange()
			}
			prev = cur
		}
	}
}

// networkChangeAllowed — можно ли прямо сейчас перезапустить туннель по смене сети:
// туннель работает, не на паузе и вне кулдауна.
func (a *App) networkChangeAllowed() bool {
	a.tunnelMu.Lock()
	defer a.tunnelMu.Unlock()

	if !a.tunnelRunning || a.tunnelPaused {
		return false
	}
	if !a.lastNetRestartAt.IsZero() && time.Since(a.lastNetRestartAt) < netCooldown {
		return false
	}
	return true
}

// onNetworkChange — реакция на смену сети: перезапуск туннеля (с кулдауном).
// Бюджет перезапусков не сбрасывается (в отличие от wake): «мерцающая» сеть
// должна сжечь лимит и привести к жёсткой остановке.
func (a *App) onNetworkChange() {
	if !a.getCfg().AutoRestoreOnNetChange {
		return
	}
	if !a.networkChangeAllowed() {
		return
	}
	a.tunnelMu.Lock()
	a.lastNetRestartAt = time.Now()
	a.tunnelMu.Unlock()

	a.log("🌐 Сеть изменилась — перезапуск туннеля...", "warn")
	a.ensureRestart("Сеть изменилась")
}