package main

import (
	"encoding/base64"
	"net/http"
	"net/netip"
	"testing"
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
