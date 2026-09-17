package main

import (
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// ── route ─────────────────────────────────────────────────────────────────────

func TestRouteDefaultPolicy(t *testing.T) {
	// Без правил и без менеджера — политика по умолчанию: proxy.
	p := NewProxyServer(nil, nil)
	if got := p.route("example.com").policy; got != PolicyProxy {
		t.Fatalf("route без правил = %q, ожидалось %q", got, PolicyProxy)
	}

	// Явная политика по умолчанию.
	p.SetDefaultPolicy(PolicyDirect)
	if got := p.route("example.com").policy; got != PolicyDirect {
		t.Fatalf("route с default=direct = %q, ожидалось %q", got, PolicyDirect)
	}
}

func TestRouteInlineRules(t *testing.T) {
	p := NewProxyServer(nil, nil)
	p.SetRulesetManager(NewRulesetManager(""))
	p.SetRulesets([]RulesetConfig{
		{Rule: "domain:exact.example", Policy: PolicyBlock, Enable: true},
		{Rule: "domain-suffix:sobaka.com", Policy: PolicyDirect, Enable: true},
		{Rule: "keyword:youtube", Policy: PolicyProxy, Enable: true},
	})
	p.SetDefaultPolicy(PolicyProxy)

	cases := []struct {
		host string
		want string
	}{
		{"exact.example", PolicyBlock},
		{"sub.exact.example", PolicyProxy}, // domain: — только точное совпадение
		{"music.sobaka.com", PolicyDirect},
		{"www.youtube.com", PolicyProxy},
		{"other.org", PolicyProxy}, // нет совпадения → default
	}
	for _, c := range cases {
		if got := p.route(c.host).policy; got != c.want {
			t.Errorf("route(%q) = %q, ожидалось %q", c.host, got, c.want)
		}
	}
}

func TestRouteFirstMatchWins(t *testing.T) {
	// Порядок правил = приоритет: первое совпадение выигрывает.
	p := NewProxyServer(nil, nil)
	p.SetRulesetManager(NewRulesetManager(""))
	p.SetRulesets([]RulesetConfig{
		{Rule: "keyword:example", Policy: PolicyBlock, Enable: true},
		{Rule: "domain-suffix:example.com", Policy: PolicyDirect, Enable: true},
	})

	res := p.route("sub.example.com")
	if res.policy != PolicyBlock {
		t.Fatalf("первое совпадение должно выигрывать: %q", res.policy)
	}
	if res.rule != "keyword:example" {
		t.Fatalf("rule = %q, ожидалось keyword:example", res.rule)
	}
}

func TestRouteDisabledRulesSkipped(t *testing.T) {
	p := NewProxyServer(nil, nil)
	p.SetRulesetManager(NewRulesetManager(""))
	p.SetRulesets([]RulesetConfig{
		{Rule: "domain:exact.example", Policy: PolicyBlock, Enable: false},
	})
	p.SetDefaultPolicy(PolicyProxy)

	if got := p.route("exact.example").policy; got != PolicyProxy {
		t.Fatalf("выключенное правило не должно матчить: %q", got)
	}
}

// ── connOpen / connClose / connCount ──────────────────────────────────────────

func TestConnCounters(t *testing.T) {
	var stats []ProxyStats
	p := NewProxyServer(nil, func(s ProxyStats) { stats = append(stats, s) })

	p.connOpen()
	p.connOpen()
	if got := p.connCount(); got != 2 {
		t.Fatalf("connCount после 2x open = %d, ожидалось 2", got)
	}
	p.connClose()
	if got := p.connCount(); got != 1 {
		t.Fatalf("connCount после 1 close = %d, ожидалось 1", got)
	}
	p.connClose()
	if got := p.connCount(); got != 0 {
		t.Fatalf("connCount после 2x close = %d, ожидалось 0", got)
	}
}

func TestConnCloseUnderflow(t *testing.T) {
	// connClose без connOpen не должен уходить в отрицательные значения.
	var stats []ProxyStats
	p := NewProxyServer(nil, func(s ProxyStats) { stats = append(stats, s) })

	p.connClose()
	if got := p.connCount(); got != 0 {
		t.Fatalf("connCount после лишнего close = %d, ожидалось 0", got)
	}
}

func TestConnStatsCallback(t *testing.T) {
	var stats []ProxyStats
	p := NewProxyServer(nil, func(s ProxyStats) { stats = append(stats, s) })

	p.connOpen()
	p.connOpen()
	p.connClose()

	if len(stats) != 3 {
		t.Fatalf("статистика должна вызываться 3 раза, вызвана %d", len(stats))
	}
	if stats[0].Active != 1 || stats[0].Total != 1 {
		t.Errorf("после первого open: %+v, ожидалось Active=1 Total=1", stats[0])
	}
	if stats[1].Active != 2 || stats[1].Total != 2 {
		t.Errorf("после второго open: %+v, ожидалось Active=2 Total=2", stats[1])
	}
	if stats[2].Active != 1 || stats[2].Total != 2 {
		t.Errorf("после close: %+v, ожидалось Active=1 Total=2", stats[2])
	}
}

func TestConnStatsNilCallback(t *testing.T) {
	// statsFn=nil (как в прод.) — не должно паниковать.
	p := NewProxyServer(nil, nil)
	p.connOpen()
	p.connClose()
}

// ── SetRulesets / SetDefaultPolicy ────────────────────────────────────────────

func TestSetRulesetsCopies(t *testing.T) {
	// SetRulesets должен копировать слайс: последующая мутация исходника
	// не должна менять внутреннее состояние.
	p := NewProxyServer(nil, nil)
	orig := []RulesetConfig{{Rule: "domain:a.com", Policy: PolicyBlock, Enable: true}}
	p.SetRulesets(orig)
	orig[0].Rule = "mutated"

	got := p.getRulesets()
	if got[0].Rule != "domain:a.com" {
		t.Fatalf("getRulesets() = %+v, ожидалась копия с domain:a.com", got)
	}
}

func TestGetDefaultPolicyEmpty(t *testing.T) {
	p := NewProxyServer(nil, nil)
	if got := p.getDefaultPolicy(); got != PolicyProxy {
		t.Fatalf("getDefaultPolicy() без значения = %q, ожидалось %q", got, PolicyProxy)
	}
}

// ── parseProxyAuth ────────────────────────────────────────────────────────────

func TestParseProxyAuth(t *testing.T) {
	mk := func(auth string) *http.Request {
		r := &http.Request{Header: http.Header{}}
		if auth != "" {
			r.Header.Set("Proxy-Authorization", auth)
		}
		return r
	}
	basic := func(u, p string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(u+":"+p))
	}

	cases := []struct {
		name     string
		auth     string
		wantOK   bool
		wantUser string
		wantPass string
	}{
		{"valid", basic("user", "pass"), true, "user", "pass"},
		{"empty-header", "", false, "", ""},
		{"wrong-scheme", "Bearer xyz", false, "", ""},
		{"not-base64", "Basic !!!not-base64!!!", false, "", ""},
		{"no-colon", "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")), false, "", ""},
		{"password-with-colon", basic("user", "pa:ss"), true, "user", "pa:ss"},
	}
	for _, c := range cases {
		u, p, ok := parseProxyAuth(mk(c.auth))
		if ok != c.wantOK || u != c.wantUser || p != c.wantPass {
			t.Errorf("%s: parseProxyAuth = (%q,%q,%v), ожидалось (%q,%q,%v)",
				c.name, u, p, ok, c.wantUser, c.wantPass, c.wantOK)
		}
	}
}

// ── wgB64ToHex / ParseDNSOverride ─────────────────────────────────────────────

func TestWgB64ToHex(t *testing.T) {
	cases := []struct{ in, want string }{
		{"AA==", "00"},                     // 1 байт 0x00
		{"AQ==", "01"},                     // 1 байт 0x01
		{"aGVsbG8=", "68656c6c6f"},         // "hello"
		{"not-base64!!!", "not-base64!!!"}, // decode error → возвращается как есть
	}
	for _, c := range cases {
		if got := wgB64ToHex(c.in); got != c.want {
			t.Errorf("wgB64ToHex(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

func TestParseDNSOverride(t *testing.T) {
	cases := []struct {
		in    string
		wantN int
	}{
		{"1.1.1.1,8.8.8.8", 2},
		{"1.1.1.1, not-an-ip, 8.8.8.8", 2}, // невалидные пропускаются
		{"", 0},
		{"not-an-ip", 0},
	}
	for _, c := range cases {
		if got := ParseDNSOverride(c.in); len(got) != c.wantN {
			t.Errorf("ParseDNSOverride(%q) = %v, ожидалось %d адресов", c.in, got, c.wantN)
		}
	}
	if got := ParseDNSOverride("1.1.1.1"); got[0] != netip.MustParseAddr("1.1.1.1") {
		t.Errorf("ParseDNSOverride(1.1.1.1) = %v", got)
	}
}

// ── Общие helper'ы проксирования (I16/I21) ───────────────────────────────────

func TestCopyHeader(t *testing.T) {
	src := http.Header{
		"X-A": {"1", "2"},
		"X-B": {"v"},
	}
	dst := http.Header{"X-A": {"pre"}}
	copyHeader(dst, src)
	if got := dst.Values("X-A"); len(got) != 3 || got[0] != "pre" || got[1] != "1" || got[2] != "2" {
		t.Fatalf("copyHeader X-A = %v, ожидалось [pre 1 2]", got)
	}
	if got := dst.Get("X-B"); got != "v" {
		t.Fatalf("copyHeader X-B = %q, ожидалось v", got)
	}
}

func TestStripHopByHopHeaders(t *testing.T) {
	h := http.Header{}
	for _, name := range hopByHopHeaders {
		h.Set(name, "x")
	}
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "keep")

	stripHopByHopHeaders(h)

	for _, name := range hopByHopHeaders {
		if h.Get(name) != "" {
			t.Errorf("hop-by-hop %q не удалён", name)
		}
	}
	if h.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type не должен удаляться")
	}
	if h.Get("Authorization") != "keep" {
		t.Errorf("Authorization не должен удаляться")
	}
}

func TestRelayBidirectional(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	defer a2.Close()
	defer b2.Close()
	defer b1.Close()

	done := make(chan struct{})
	go func() { relayBidirectional(a2, b2, 5*time.Second); close(done) }()

	// a1 → b1
	go a1.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(b1, buf); err != nil {
		t.Fatalf("чтение a→b: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("a→b = %q, ожидалось ping", buf)
	}

	// b1 → a1
	go b1.Write([]byte("pong"))
	if _, err := io.ReadFull(a1, buf); err != nil {
		t.Fatalf("чтение b→a: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("b→a = %q, ожидалось pong", buf)
	}

	// Закрытие одной стороны должно завершить релей (обе копирующие горутины
	// разблокируются по cancel()).
	a1.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("relayBidirectional не завершился после закрытия соединения")
	}
}

// ── sysProxyHandler (I10/I21) ────────────────────────────────────────────────

// noTunnelSysProxyServer — прокси без правил (политика по умолчанию proxy) и
// без активного туннеля: любой вывод в туннель обязан падать.
func noTunnelSysProxyServer() *ProxyServer {
	return NewProxyServer(nil, nil)
}

// Без активного WireGuard-туннеля wgDialStrict всегда падает — оба
// обработчика обязаны вернуть 502, а не висеть/паниковать.
func TestSysProxyHandlerConnectNoTunnel(t *testing.T) {
	h := newSysProxyHandler(noTunnelSysProxyServer())
	req := httptest.NewRequest(http.MethodConnect, "example.invalid:443", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("CONNECT без туннеля: статус %d, ожидалось 502", rec.Code)
	}
}

func TestSysProxyHandlerHTTPNoTunnel(t *testing.T) {
	h := newSysProxyHandler(noTunnelSysProxyServer())
	req := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("HTTP без туннеля: статус %d, ожидалось 502", rec.Code)
	}
}

func TestNewSysProxyHandlerClose(t *testing.T) {
	h := newSysProxyHandler(NewProxyServer(nil, nil))
	if h.transportDirect == nil || h.transportTunnel == nil {
		t.Fatal("транспорты не созданы")
	}
	if h.transportDirect.IdleConnTimeout <= 0 || h.transportTunnel.IdleConnTimeout <= 0 {
		t.Fatalf("IdleConnTimeout = %v/%v, ожидался положительный",
			h.transportDirect.IdleConnTimeout, h.transportTunnel.IdleConnTimeout)
	}
	h.Close() // не должно паниковать
	// nil-получатель тоже безопасен (SystemProxyDisable зовёт handler.Close()
	// даже когда прокси не был включён).
	var nilH *sysProxyHandler
	nilH.Close()
}

// Тест-хелпер: прокси с одним включённым правилом и сбором логов.
func sysProxyWithRule(rule, policy string) (*sysProxyHandler, *[]string) {
	var logs []string
	p := NewProxyServer(func(msg, lv string) { logs = append(logs, msg) }, nil)
	p.SetRulesetManager(NewRulesetManager(""))
	p.SetRulesets([]RulesetConfig{{Rule: rule, Policy: policy, Enable: true}})
	return newSysProxyHandler(p), &logs
}

// Правило block → 403 и строка лога ровно в целевом формате.
func TestSysProxyHandlerBlockRule(t *testing.T) {
	h, logs := sysProxyWithRule("domain:example.invalid", PolicyBlock)
	req := httptest.NewRequest(http.MethodConnect, "example.invalid:443", nil)
	// Порт 1 заведомо не принадлежит ни одному процессу → приложение "—".
	req.RemoteAddr = "192.0.2.1:1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("block CONNECT: статус %d, ожидалось 403", rec.Code)
	}
	want := "→ example.invalid:443 - — - [block]"
	if len(*logs) == 0 || (*logs)[len(*logs)-1] != want {
		t.Fatalf("лог = %v, ожидалось %q", *logs, want)
	}
}

// Правило direct → sysproxy ходит напрямую (в обход туннеля) и логирует direct.
func TestSysProxyHandlerDirectHTTP(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	}))
	defer target.Close()

	h, logs := sysProxyWithRule("ip:127.0.0.1", PolicyDirect)
	req := httptest.NewRequest(http.MethodGet, target.URL+"/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("direct HTTP: статус %d, ожидалось 200", rec.Code)
	}
	if len(*logs) == 0 {
		t.Fatal("нет строки лога соединения")
	}
	last := (*logs)[len(*logs)-1]
	if !strings.HasPrefix(last, "→ ") || !strings.Contains(last, ", direct]") {
		t.Fatalf("лог = %q, ожидался формат \"→ ... - ..., direct]\"", last)
	}
}

// ── Формат строк лога соединений ─────────────────────────────────────────────

func TestConnLine(t *testing.T) {
	if got := connLine("8.8.8.8:2345", "chrome.exe", PolicyProxy, 10391); got != "→ 8.8.8.8:2345 - chrome.exe - [10391ms, proxy]" {
		t.Fatalf("connLine = %q", got)
	}
	if got := connLine("a.com:443", unknownApp, PolicyDirect, 5); got != "→ a.com:443 - — - [5ms, direct]" {
		t.Fatalf("connLine direct = %q", got)
	}
	if got := connLineBlock("a.com:443", "curl"); got != "→ a.com:443 - curl - [block]" {
		t.Fatalf("connLineBlock = %q", got)
	}
	if got := connLineError("a.com:443", "curl", "i/o timeout"); got != "→ a.com:443 - curl: ошибка: i/o timeout" {
		t.Fatalf("connLineError = %q", got)
	}
	// CR/LF в подставляемых значениях не должно разрывать строку лога.
	if got := connLine("a.com:443\r\n→ evil:1", "ap\np.exe", PolicyProxy, 1); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("connLine оставил перевод строки: %q", got)
	}
	if got := connLineError("a.com:443", "curl", "boom\n→ fake"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("connLineError оставил перевод строки: %q", got)
	}
}

func TestUAAppName(t *testing.T) {
	cases := []struct{ ua, want string }{
		{"", ""},
		{"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0 Safari/537.36", "Chrome"},
		{"Mozilla/5.0 (X11; Linux) Gecko/20100101 Firefox/137.0", "Firefox"},
		{"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/146.0 Safari/537.36 Edg/146.0", "Edge"},
		{"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/146.0 Safari/537.36 OPR/100.0", "Opera"},
		{"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/146.0 Safari/537.36 YaBrowser/24.1", "Yandex"},
		{"Mozilla/5.0 (Macintosh) AppleWebKit/605.1 Safari/605.1", "Safari"},
		{"curl/8.4.0", "curl"},
		{"Mozilla/5.0 (unknown)", ""},
	}
	for _, c := range cases {
		if got := uaAppName(c.ua); got != c.want {
			t.Errorf("uaAppName(%q) = %q, ожидалось %q", c.ua, got, c.want)
		}
	}
}

func TestClientPortFromAddr(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"127.0.0.1:54321", 54321},
		{"[::1]:8080", 8080},
		{"127.0.0.1", 0},
		{"", 0},
		{"127.0.0.1:abc", 0},
		{"127.0.0.1:0", 0},
		{"127.0.0.1:99999", 0},
	}
	for _, c := range cases {
		if got := clientPortFromAddr(c.in); got != c.want {
			t.Errorf("clientPortFromAddr(%q) = %d, ожидалось %d", c.in, got, c.want)
		}
	}
}

func TestHTTPTarget(t *testing.T) {
	cases := []struct{ url, want string }{
		{"http://example.com/path", "example.com:80"},
		{"https://example.com/path", "example.com:443"},
		{"http://example.com:8080/path", "example.com:8080"},
		{"http://[::1]/path", "[::1]:80"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.url, nil)
		if got := httpTarget(req); got != c.want {
			t.Errorf("httpTarget(%q) = %q, ожидалось %q", c.url, got, c.want)
		}
	}
}

func TestAppNameCache(t *testing.T) {
	c := &appNameCache{ttl: time.Second, items: map[int]appNameEntry{}}
	if _, ok := c.get(80); ok {
		t.Fatal("пустой кеш не должен содержать запись")
	}
	c.put(80, "svc")
	if name, ok := c.get(80); !ok || name != "svc" {
		t.Fatalf("get = %q/%v, ожидалось svc/true", name, ok)
	}
	c.items[81] = appNameEntry{name: "old", at: time.Now().Add(-2 * time.Second)}
	if _, ok := c.get(81); ok {
		t.Fatal("истёкшая запись не должна возвращаться")
	}
}

func TestConnAppNameFallback(t *testing.T) {
	// Порт 1 заведомо не принадлежит процессу → откат на UA.
	if got := connAppName(1, "curl/8.4.0"); got != "curl" {
		t.Fatalf("connAppName = %q, ожидалось curl", got)
	}
	// Ни порта, ни UA → "—".
	if got := connAppName(0, ""); got != unknownApp {
		t.Fatalf("connAppName = %q, ожидалось %q", got, unknownApp)
	}
}

// ── wgDialFallbackTimeout (индикатор пинга) ──────────────────────────────────

// Без активного туннеля fallback-dial обязан идти напрямую (не падать с
// «туннель не активен») — иначе пинг в шапке не отображался бы до поднятия WG.
func TestWgDialFallbackNoTunnel(t *testing.T) {
	StopWGTunnel() // гарантируем неактивный глобальный туннель

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	conn, err := wgDialFallbackTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("fallback dial без туннеля: %v", err)
	}
	conn.Close()
}
