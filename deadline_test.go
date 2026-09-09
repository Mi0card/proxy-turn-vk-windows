package main

import (
	"testing"
	"time"
)

func TestDeadlineBurstLimit(t *testing.T) {
	cases := []struct {
		totalWorkers int
		want         int
	}{
		{0, 1},
		{1, 1},
		{4, 4}, // account-mode максимум
		{9, 9}, // значение по умолчанию
		{108, 108},
	}
	for _, c := range cases {
		if got := deadlineBurstLimit(c.totalWorkers); got != c.want {
			t.Errorf("deadlineBurstLimit(%d)=%d, want %d", c.totalWorkers, got, c.want)
		}
	}
}

func TestNoteWorkerDeadline(t *testing.T) {
	// Туннель мёртв только когда в окне откажут ВСЕ воркеры.
	a := &App{totalWorkers: 9}
	if a.noteWorkerDeadline(1) {
		t.Fatal("1 воркер из 9 не должен считать туннель мёртвым")
	}
	for i := 2; i <= 8; i++ {
		if a.noteWorkerDeadline(i) {
			t.Fatalf("воркер %d из 9 не должен считать туннель мёртвым", i)
		}
	}
	// Повторный отказ того же воркера не приближает к порогу (dedup по ID).
	if a.noteWorkerDeadline(3) {
		t.Fatal("повторный отказ того же воркера не должен триггерить перезапуск")
	}
	if !a.noteWorkerDeadline(9) {
		t.Fatal("последний воркер должен считать туннель мёртвым")
	}
}

func TestNoteWorkerDeadlineSingleWorker(t *testing.T) {
	// При одном воркере его таймаут = мёртвый туннель.
	a := &App{totalWorkers: 1}
	if !a.noteWorkerDeadline(1) {
		t.Fatal("единственный воркер должен сразу считать туннель мёртвым")
	}
}

func TestDeadTunnelDue(t *testing.T) {
	now := time.Now().UnixMilli()
	ms := func(d time.Duration) int64 { return int64(d / time.Millisecond) }

	cases := []struct {
		name          string
		procStartedMs int64         // от now назад
		lastActiveMs  int64         // от now назад; -1 = никогда (0)
		cooldownAgo   time.Duration // время с последнего dead-рестарта; 0 = не было
		running       bool
		paused        bool
		want          bool
	}{
		{"dead: давно стартовал, активности не было", ms(10 * time.Minute), -1, 0, true, false, true},
		{"dead: активность устарела", ms(10 * time.Minute), ms(5 * time.Minute), 0, true, false, true},
		{"startup grace ещё не прошёл", ms(30 * time.Second), -1, 0, true, false, false},
		{"свежая активность — туннель жив", ms(10 * time.Minute), ms(30 * time.Second), 0, true, false, false},
		{"в кулдауне после dead-рестарта", ms(10 * time.Minute), -1, 1 * time.Minute, true, false, false},
		{"кулдаун истёк", ms(10 * time.Minute), -1, 4 * time.Minute, true, false, true},
		{"на паузе", ms(10 * time.Minute), -1, 0, true, true, false},
		{"туннель остановлен", ms(10 * time.Minute), -1, 0, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			procStarted := now - c.procStartedMs
			lastActive := int64(0)
			if c.lastActiveMs >= 0 {
				lastActive = now - c.lastActiveMs
			}
			var lastRestart time.Time
			if c.cooldownAgo > 0 {
				lastRestart = time.Now().Add(-c.cooldownAgo)
			}
			if got := deadTunnelDue(now, procStarted, lastActive, lastRestart,
				deadStartGrace, deadActivityStale, deadRestartCooldown, c.running, c.paused); got != c.want {
				t.Errorf("deadTunnelDue = %v, want %v", got, c.want)
			}
		})
	}
}
