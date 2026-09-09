package main

import "testing"

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

func TestDeadTunnelOnPingFails(t *testing.T) {
	cases := []struct {
		failCount int
		want      bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, true},
		{4, true},
	}
	for _, c := range cases {
		if got := deadTunnelOnPingFails(c.failCount); got != c.want {
			t.Errorf("deadTunnelOnPingFails(%d)=%v, want %v", c.failCount, got, c.want)
		}
	}
}
