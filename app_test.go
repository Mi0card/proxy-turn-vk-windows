package main

import "testing"

// Проверяет, что строки соединений ("→ ") буферизуются, а не эмитятся сразу.
// Без коалесинга socksLog дёргал бы runtime.EventsEmit на каждую строку.
func TestSocksLogBuffersConnLines(t *testing.T) {
	a := &App{}
	a.socksLog("→ example.com:443 - chrome.exe - [10ms, proxy]", "dim")
	a.socksLog("→ other.com:443 - chrome.exe - [12ms, proxy]", "dim")

	a.connLogMu.Lock()
	n := len(a.connLogBuf)
	a.connLogMu.Unlock()
	if n != 2 {
		t.Fatalf("ожидалось 2 буферизованные строки, получено %d", n)
	}
}

// Проверяет, что статистика коалесится до последнего значения (dirty-флаг),
// а не эмитится на каждый вызов connOpen/connClose.
func TestOnProxyStatsCoalescesLatest(t *testing.T) {
	a := &App{}
	a.onProxyStats(ProxyStats{Active: 1, Total: 1})
	a.onProxyStats(ProxyStats{Active: 5, Total: 7})

	a.statsMu.Lock()
	got, dirty := a.statsLatest, a.statsDirty
	a.statsMu.Unlock()
	if !dirty {
		t.Fatal("statsDirty должен быть true после onProxyStats")
	}
	if got.Active != 5 || got.Total != 7 {
		t.Fatalf("ожидалось последнее значение {5 7}, получено %+v", got)
	}
}

// parseGpuDisabled: ключ отсутствует/JSON битый → set=false (env/дефолт),
// явные значения → set=true. Нужно для приоритета «конфиг > env».
func TestParseGpuDisabled(t *testing.T) {
	cases := []struct {
		name         string
		json         string
		wantDisabled bool
		wantSet      bool
	}{
		{"unset", `{}`, false, false},
		{"enabled", `{"disable_gpu":false}`, false, true},
		{"disabled", `{"disable_gpu":true}`, true, true},
		{"corrupt", `{not json`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotDisabled, gotSet := parseGpuDisabled([]byte(tc.json))
			if gotDisabled != tc.wantDisabled || gotSet != tc.wantSet {
				t.Fatalf("parseGpuDisabled(%q) = (%v,%v), want (%v,%v)",
					tc.json, gotDisabled, gotSet, tc.wantDisabled, tc.wantSet)
			}
		})
	}
}
