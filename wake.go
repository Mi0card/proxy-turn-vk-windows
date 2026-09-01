package main

import (
	"time"
)

const (
	// wakePollInterval — частота опроса системного времени для детекта пробуждения.
	wakePollInterval = 2 * time.Second
	// wakeThreshold — минимальный «прыжок» времени, считающийся пробуждением из сна.
	// RTC продолжает тикать во сне, а дрейф NTP сильно меньше этой величины.
	wakeThreshold = 15 * time.Second
	// maxRestartAttempts — лимит последовательных неудачных перезапусков, после
	// которого туннель останавливается (а не крутится бесконечно).
	maxRestartAttempts = 8
	// restartStaleWindow — окно, после которого lastActiveAt считается устаревшим.
	restartStaleWindow = 2 * time.Minute
)

// wakeSettleDelay — пауза после пробуждения, чтобы сеть устаканилась (var для тестов).
var wakeSettleDelay = 5 * time.Second

// detectWake возвращает true, если настенные часы прыгнули вперёд больше чем на
// threshold с предыдущей выборки. RTC тикает во сне, поэтому это надёжный
// кросс-платформенный сигнал пробуждения; подстройка NTP значительно меньше
// порога и не даёт ложных срабатываний.
func detectWake(prev, cur time.Time, threshold time.Duration) bool {
	if prev.IsZero() {
		return false
	}
	return cur.Sub(prev) > threshold
}

// shouldHaltRestarts решает, пора ли остановить туннель вместо очередного
// перезапуска: исчерпан лимит попыток И не было свежей активности воркеров
// (lastActiveAt пуст или устарел). Активность воркеров сбрасывает restartAttempts,
// поэтому attempts>maxAttempts уже означает отсутствие восстановления.
func shouldHaltRestarts(attempts, maxAttempts int, lastActiveAt, nowMs, staleMs int64) bool {
	if attempts <= maxAttempts {
		return false
	}
	if lastActiveAt == 0 {
		return true
	}
	return nowMs-lastActiveAt > staleMs
}

// startWakeMonitor поллит настенные часы каждые wakePollInterval; при прыжке
// больше wakeThreshold вызывает onWake один раз. Останавливается по close(stop).
func (a *App) startWakeMonitor(stop chan struct{}, onWake func()) {
	ticker := time.NewTicker(wakePollInterval)
	defer ticker.Stop()
	prev := time.Now()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			if detectWake(prev, now, wakeThreshold) {
				onWake()
			}
			prev = now
		}
	}
}

// onWake — реакция на выход из сна: перезапускаем туннель, если он работал
// и не был поставлен на паузу. Свежий после сна бюджет перезапусков.
func (a *App) onWake() {
	if !a.getCfg().AutoRestoreOnWake {
		return
	}
	a.tunnelMu.Lock()
	running := a.tunnelRunning
	paused := a.tunnelPaused
	if running && !paused {
		a.resetWakeCountersLocked()
	}
	a.tunnelMu.Unlock()

	if !running || paused {
		return
	}

	a.log("💤 ПК вышел из сна — перезапуск туннеля...", "warn")
	// Даём сети устаканиться после пробуждения.
	time.Sleep(wakeSettleDelay)
	a.ensureRestart("ПК вышел из сна")
}

// resetWakeCountersLocked сбрасывает счётчики circuit breaker и бюджет
// перезапусков после пробуждения. Вызывается при взятом tunnelMu.
func (a *App) resetWakeCountersLocked() {
	a.floodCount = 0
	a.mismatchCount = 0
	a.refusedCount = 0
	a.wrapTimeoutCount = 0
	a.restartAttempts = 0
}

// ensureRestart — единая точка перезапуска: single-flight (конкурентные вызовы
// схлопываются в один) и лимит неудачных попыток. При исчерпании лимита без
// свежей активности — жёсткая остановка с ошибкой.
func (a *App) ensureRestart(reason string) {
	a.restartMu.Lock()
	if a.restarting {
		a.restartMu.Unlock()
		return
	}
	a.restarting = true
	a.restartMu.Unlock()

	defer func() {
		a.restartMu.Lock()
		a.restarting = false
		a.restartMu.Unlock()
	}()

	a.tunnelMu.Lock()
	attempts := a.restartAttempts
	lastActiveAt := a.lastActiveAt
	running := a.tunnelRunning
	paused := a.tunnelPaused
	a.tunnelMu.Unlock()

	// Туннель мог быть остановлен или поставлен на паузу, пока мы ждали очереди.
	if !running || paused {
		return
	}

	now := time.Now().UnixMilli()
	if shouldHaltRestarts(attempts, maxRestartAttempts, lastActiveAt, now, int64(restartStaleWindow/time.Millisecond)) {
		a.handleCritical("⚠ Много неудачных перезапусков — туннель остановлен. Проверьте сеть/пароль.")
		return
	}

	a.log("↺ "+reason, "warn")
	a.restartTunnel()
}
