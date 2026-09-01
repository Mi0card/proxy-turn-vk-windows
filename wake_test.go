package main

import (
	"testing"
	"time"
)

func TestDetectWake(t *testing.T) {
	threshold := 15 * time.Second
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		prev time.Time
		cur  time.Time
		want bool
	}{
		{"first sample (prev zero)", time.Time{}, base, false},
		{"no movement", base, base, false},
		{"small forward (ntp)", base, base.Add(2 * time.Second), false},
		{"exactly threshold", base, base.Add(threshold), false},
		{"just under", base, base.Add(threshold - time.Millisecond), false},
		{"just over", base, base.Add(threshold + time.Millisecond), true},
		{"big sleep jump", base, base.Add(90 * time.Second), true},
		{"clock backwards", base, base.Add(-5 * time.Second), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detectWake(c.prev, c.cur, threshold); got != c.want {
				t.Fatalf("detectWake(%v,%v)=%v, want %v", c.prev, c.cur, got, c.want)
			}
		})
	}
}

func TestShouldHaltRestarts(t *testing.T) {
	const (
		max      = 8
		now      = int64(1_000_000)
		staleMs  = int64(120_000)
		freshAt  = now - 1_000       // активность секунду назад
		staleAt  = now - 300_000     // активность 5 минут назад
	)

	cases := []struct {
		name         string
		attempts     int
		lastActiveAt int64
		want         bool
	}{
		{"at budget", max, 0, false},
		{"just over budget, never active", max + 1, 0, true},
		{"under budget, fresh", 3, freshAt, false},
		{"over budget, fresh activity", max + 1, freshAt, false},
		{"over budget, stale activity", max + 1, staleAt, true},
		{"zero attempts", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldHaltRestarts(c.attempts, max, c.lastActiveAt, now, staleMs); got != c.want {
				t.Fatalf("shouldHaltRestarts(attempts=%d,active=%d)=%v, want %v", c.attempts, c.lastActiveAt, got, c.want)
			}
		})
	}
}

func TestOnWakeGuards(t *testing.T) {
	// Остановленный туннель — перезапуск не должен произойти.
	stopped := &App{cfg: Config{AutoRestoreOnWake: true}}
	stopped.onWake()
	if stopped.restarting {
		t.Fatal("stopped tunnel should not restart")
	}

	// На паузе — перезапуск не должен произойти.
	paused := &App{tunnelRunning: true, tunnelPaused: true, cfg: Config{AutoRestoreOnWake: true}}
	paused.onWake()
	if paused.restarting {
		t.Fatal("paused tunnel should not restart")
	}

	// Сброс счётчиков при пробуждении работающего туннеля.
	running := &App{tunnelRunning: true, tunnelPaused: false,
		cfg: Config{AutoRestoreOnWake: true},
		floodCount: 4, mismatchCount: 5, refusedCount: 400, wrapTimeoutCount: 2, restartAttempts: 7}
	running.tunnelMu.Lock()
	running.resetWakeCountersLocked()
	running.tunnelMu.Unlock()
	if running.floodCount != 0 || running.mismatchCount != 0 || running.refusedCount != 0 ||
		running.wrapTimeoutCount != 0 || running.restartAttempts != 0 {
		t.Fatalf("counters not reset on wake: %+v", running)
	}
}

func TestOnWakeDisabled(t *testing.T) {
	// Автовосстановление выключено — пробуждение не должно сбрасывать счётчики
	// и не должно запускать перезапуск.
	a := &App{tunnelRunning: true, tunnelPaused: false,
		cfg:           Config{AutoRestoreOnWake: false},
		floodCount: 4, restartAttempts: 7}
	a.onWake()
	a.tunnelMu.Lock()
	defer a.tunnelMu.Unlock()
	if a.floodCount == 0 || a.restartAttempts == 0 {
		t.Fatal("disabled wake restore must not touch counters")
	}
	if a.restarting {
		t.Fatal("disabled wake restore must not restart")
	}
}

func TestEnsureRestartSingleFlight(t *testing.T) {
	a := &App{tunnelRunning: true, tunnelPaused: false}
	a.tunnelMu.Lock()
	a.tunnelRunning = false // restartTunnel требует lastTunnelParams; ставим пустой — вернётся без действий
	a.tunnelMu.Unlock()

	a.restartMu.Lock()
	a.restarting = true // уже идёт перезапуск
	a.restartMu.Unlock()
	a.ensureRestart("test")
	// Должен вернуться сразу, не трогая restarting.
	a.restartMu.Lock()
	still := a.restarting
	a.restartMu.Unlock()
	if !still {
		t.Fatal("single-flight must not be cleared by a concurrent caller")
	}
}
