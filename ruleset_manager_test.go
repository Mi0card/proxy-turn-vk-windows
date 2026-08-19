package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// ── Protobuf-хелперы для синтетических фикстур ───────────────────────────────

func pbVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func pbTag(field, wire int) []byte {
	return pbVarint(uint64(field)<<3 | uint64(wire))
}

func pbBytes(field int, b []byte) []byte {
	out := pbTag(field, 2)
	out = append(out, pbVarint(uint64(len(b)))...)
	return append(out, b...)
}

func pbStr(field int, s string) []byte {
	return pbBytes(field, []byte(s))
}

// geositeDomain строит сообщение Domain: field1=type (varint), field2=value.
func geositeDomain(domType int, pattern string) []byte {
	out := pbTag(1, 0) // 0x08
	out = append(out, pbVarint(uint64(domType))...)
	return append(out, pbStr(2, pattern)...)
}

// geositeGroup строит сообщение Category: field1=name, field2=domain.
func geositeGroup(name string, domains ...[]byte) []byte {
	out := pbStr(1, name)
	for _, d := range domains {
		out = append(out, pbBytes(2, d)...)
	}
	return out
}

// geoipCIDR строит сообщение CIDR: field1=ip(bytes), field2=prefix(varint).
func geoipCIDR(ip netip.Addr, prefix int) []byte {
	out := pbBytes(1, ip.AsSlice())
	out = append(out, pbTag(2, 0)...) // 0x10
	out = append(out, pbVarint(uint64(prefix))...)
	return out
}

// geoipCountry строит сообщение Country: field1=code, field2=cidr.
func geoipCountry(code string, cidrs ...[]byte) []byte {
	out := pbStr(1, code)
	for _, c := range cidrs {
		out = append(out, pbBytes(2, c)...)
	}
	return out
}

func writeFixture(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ── Парсинг geosite ───────────────────────────────────────────────────────────

func TestParseGeositeFile(t *testing.T) {
	data := pbBytes(1, geositeGroup("CATEGORY-RU",
		geositeDomain(2, "example.com"),
		geositeDomain(3, "exact.example"),
		geositeDomain(1, `\.yandex\.ru$`),
	))
	data = append(data, pbBytes(1, geositeGroup("YOUTUBE",
		geositeDomain(0, "youtube.com"),
	))...)

	path := writeFixture(t, "geosite.dat", data)
	groups, err := parseGeositeFile(path)
	if err != nil {
		t.Fatalf("parseGeositeFile: %v", err)
	}

	ru, ok := groups["CATEGORY-RU"]
	if !ok {
		t.Fatal("группа CATEGORY-RU не найдена")
	}
	if _, ok := ru.suffix["example.com"]; !ok {
		t.Error("suffix example.com не добавлен")
	}
	if _, ok := ru.full["exact.example"]; !ok {
		t.Error("full exact.example не добавлен")
	}
	if len(ru.regex) != 1 {
		t.Errorf("regexp: ожидалось 1, получено %d", len(ru.regex))
	}

	if _, ok := groups["YOUTUBE"]; !ok {
		t.Fatal("группа YOUTUBE не найдена")
	}
	if _, ok := groups["youtube"]; ok {
		t.Error("ключ должен быть в верхнем регистре")
	}
}

func TestParseGeositeFileEmpty(t *testing.T) {
	path := writeFixture(t, "geosite.dat", []byte{0x00})
	if _, err := parseGeositeFile(path); err == nil {
		t.Error("пустой файл должен давать ошибку")
	}
}

// ── Парсинг geoip ─────────────────────────────────────────────────────────────

func TestParseGeoIPFile(t *testing.T) {
	data := pbBytes(1, geoipCountry("PRIVATE",
		geoipCIDR(netip.MustParseAddr("10.0.0.0"), 8),
		geoipCIDR(netip.MustParseAddr("192.168.0.0"), 16),
	))
	path := writeFixture(t, "geoip.dat", data)

	groups, err := parseGeoIPFile(path)
	if err != nil {
		t.Fatalf("parseGeoIPFile: %v", err)
	}
	g, ok := groups["PRIVATE"]
	if !ok {
		t.Fatal("группа PRIVATE не найдена")
	}
	if len(g.cidrs) != 2 {
		t.Fatalf("cidrs: ожидалось 2, получено %d", len(g.cidrs))
	}
	if !g.cidrs[0].Contains(netip.MustParseAddr("10.5.5.5")) {
		t.Error("10.5.5.5 должен попадать в 10.0.0.0/8")
	}
	if g.cidrs[0].Contains(netip.MustParseAddr("11.0.0.1")) {
		t.Error("11.0.0.1 не должен попадать в 10.0.0.0/8")
	}
}

// ── Матчинг доменов ───────────────────────────────────────────────────────────

func TestGeositeGroupMatches(t *testing.T) {
	re, _ := regexp.Compile(`\.yandex\.ru$`)
	g := &GeositeGroup{
		suffix: map[string]struct{}{"example.com": {}},
		full:   map[string]struct{}{"exact.example": {}},
		regex:  []*regexp.Regexp{re},
	}
	cases := []struct {
		domain string
		want   bool
	}{
		{"example.com", true},
		{"sub.example.com", true},
		{"deep.sub.example.com", true},
		{"exact.example", true},
		{"sub.exact.example", false}, // full — только точное совпадение
		{"music.yandex.ru", true},
		{"example.org", false},
		{"notexample.com", false},
		{"", false},
	}
	for _, c := range cases {
		if got := g.matches(c.domain); got != c.want {
			t.Errorf("matches(%q) = %v, ожидалось %v", c.domain, got, c.want)
		}
	}
}

func TestMatchRulesDomain(t *testing.T) {
	m := NewRulesetManager("")
	parsed, err := parseGeositeFile(writeFixture(t, "geosite.dat",
		pbBytes(1, geositeGroup("CATEGORY-RU", geositeDomain(2, "example.com"))),
	))
	if err != nil {
		t.Fatal(err)
	}
	m.geosite = parsed
	m.loaded = true

	configs := []RulesetConfig{
		{Rule: "ruleset:geosite-category-ru", Policy: "block", Enable: true},
	}

	got := m.MatchRules(configs, "sub.example.com", func(match RoutingMatch) bool {
		if match.Policy != "block" || match.Rule != "ruleset:geosite-category-ru" {
			t.Errorf("неожиданный матч: %+v", match)
		}
		return true
	})
	if !got {
		t.Error("sub.example.com должен совпадать")
	}

	if m.MatchRules(configs, "example.org", func(RoutingMatch) bool { return true }) {
		t.Error("example.org не должен совпадать")
	}

	// Выключенное правило не матчит.
	off := []RulesetConfig{{Rule: "ruleset:geosite-category-ru", Policy: "block", Enable: false}}
	if m.MatchRules(off, "example.com", func(RoutingMatch) bool { return true }) {
		t.Error("выключенное правило не должно матчить")
	}
}

// ── Матчинг IP ────────────────────────────────────────────────────────────────

func TestMatchRulesIP(t *testing.T) {
	m := NewRulesetManager("")
	parsed, err := parseGeoIPFile(writeFixture(t, "geoip.dat",
		pbBytes(1, geoipCountry("PRIVATE", geoipCIDR(netip.MustParseAddr("10.0.0.0"), 8))),
	))
	if err != nil {
		t.Fatal(err)
	}
	m.geoip = parsed
	m.loaded = true

	configs := []RulesetConfig{
		{Rule: "ruleset:geoip-private", Policy: "direct", Enable: true},
	}

	if !m.MatchRules(configs, "10.1.2.3", func(match RoutingMatch) bool {
		if match.Policy != "direct" {
			t.Errorf("неожиданная политика: %s", match.Policy)
		}
		return true
	}) {
		t.Error("10.1.2.3 должен совпадать с PRIVATE")
	}

	// IPv4-mapped IPv6 тоже матчит после Unmap.
	if !m.MatchRules(configs, "::ffff:10.1.2.3", func(RoutingMatch) bool { return true }) {
		t.Error("::ffff:10.1.2.3 должен матчить после unmapping")
	}

	if m.MatchRules(configs, "8.8.8.8", func(RoutingMatch) bool { return true }) {
		t.Error("8.8.8.8 не должен совпадать")
	}
}

// ── parseRule / validateRule / validPolicy ────────────────────────────────────

func TestParseRule(t *testing.T) {
	cases := []struct {
		in, typ, group string
	}{
		{"ruleset:geosite-category-ru", "geosite", "category-ru"},
		{"ruleset:geoip-private", "geoip", "private"},
		{"ruleset:geosite-youtube", "geosite", "youtube"},
		{"ruleset:plain", "plain", ""},
		{"not-a-rule", "", ""},
		{"domain:example.com", "domain", "example.com"},
		{"domain-suffix:example.com", "domain-suffix", "example.com"},
		{"keyword:youtube", "keyword", "youtube"},
		{"regex:\\.example\\.com$", "regex", `\.example\.com$`},
		{"cidr:10.0.0.0/8", "cidr", "10.0.0.0/8"},
		{"ip:1.2.3.4", "ip", "1.2.3.4"},
		{"DOMAIN:Example.COM", "domain", "Example.COM"},
		{"  domain-suffix:vk.com  ", "domain-suffix", "vk.com"},
	}
	for _, c := range cases {
		typ, group := parseRule(c.in)
		if typ != c.typ || group != c.group {
			t.Errorf("parseRule(%q) = (%q, %q), ожидалось (%q, %q)",
				c.in, typ, group, c.typ, c.group)
		}
	}
}

func TestValidateRule(t *testing.T) {
	for _, ok := range []struct {
		rule string
		want bool
	}{
		{"ruleset:geosite-category-ru", true},
		{"ruleset:geoip-private", true},
		{"ruleset:geosite-youtube", true},
		{"ruleset:foo", false},
		{"geosite-category-ru", false},
		{"", false},
		{"ruleset:", false},
		{"domain:example.com", true},
		{"domain:", false},
		{"domain-suffix:example.com", true},
		{"domain-suffix:", false},
		{"keyword:youtube", true},
		{"keyword:", false},
		{"regex:\\.example\\.com$", true},
		{"regex:[", false},
		{"cidr:10.0.0.0/8", true},
		{"cidr:not-a-subnet", false},
		{"ip:1.2.3.4", true},
		{"ip:not-an-ip", false},
	} {
		if got := validateRule(ok.rule); got != ok.want {
			t.Errorf("validateRule(%q) = %v, ожидалось %v", ok.rule, got, ok.want)
		}
	}
}

func TestNormalizeRule(t *testing.T) {
	cases := map[string]string{
		"geosite-category-ru":    "ruleset:geosite-category-ru",
		"geoip-private":          "ruleset:geoip-private",
		"ruleset:geoip-private":  "ruleset:geoip-private",
		"domain:example.com":     "domain:example.com",
		"DOMAIN-SUFFIX:vk.com":   "DOMAIN-SUFFIX:vk.com",
		"keyword:youtube":         "keyword:youtube",
		"regex:\\.com$":          `regex:\.com$`,
		"cidr:10.0.0.0/8":        "cidr:10.0.0.0/8",
		"ip:1.2.3.4":             "ip:1.2.3.4",
		"  keyword:youtube  ":     "keyword:youtube",
	}
	for in, want := range cases {
		if got := normalizeRule(in); got != want {
			t.Errorf("normalizeRule(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestNeedsDownloadedRulesets(t *testing.T) {
	cases := []struct {
		configs []RulesetConfig
		want    bool
	}{
		{nil, false},
		{[]RulesetConfig{{Rule: "domain:example.com"}}, false},
		{[]RulesetConfig{{Rule: "keyword:youtube"}, {Rule: "cidr:10.0.0.0/8"}}, false},
		{[]RulesetConfig{{Rule: "ruleset:geosite-youtube"}}, true},
		{[]RulesetConfig{{Rule: "ruleset:geoip-private"}}, true},
		{[]RulesetConfig{{Rule: "domain:example.com"}, {Rule: "ruleset:geoip-private"}}, true},
	}
	for _, c := range cases {
		if got := needsDownloadedRulesets(c.configs); got != c.want {
			t.Errorf("needsDownloadedRulesets(%+v) = %v, ожидалось %v", c.configs, got, c.want)
		}
	}
}

func TestValidPolicy(t *testing.T) {
	for _, c := range []struct {
		p    string
		want bool
	}{
		{"block", true}, {"direct", true}, {"proxy", true},
		{"", false}, {"Block", false}, {"reject", false},
	} {
		if got := validPolicy(c.p); got != c.want {
			t.Errorf("validPolicy(%q) = %v, ожидалось %v", c.p, got, c.want)
		}
	}
}

// ── Матчинг встроенных (inline) правил ────────────────────────────────────────

func TestMatchRulesInlineDomain(t *testing.T) {
	m := NewRulesetManager("") // без geosite/geoip — скачивание не нужно
	configs := []RulesetConfig{
		{Rule: "domain:exact.example", Policy: "block", Enable: true},
		{Rule: "domain-suffix:sobaka.com", Policy: "direct", Enable: true},
		{Rule: "keyword:youtube", Policy: "proxy", Enable: true},
	}

	// domain: — только точное совпадение.
	if !m.MatchRules(configs, "exact.example", func(match RoutingMatch) bool {
		return match.Policy == "block"
	}) {
		t.Error("exact.example должен совпадать с domain:")
	}
	if m.MatchRules(configs, "sub.exact.example", func(RoutingMatch) bool { return true }) {
		t.Error("sub.exact.example не должен совпадать с domain: (точное)")
	}

	// domain-suffix: — сам домен и поддомены.
	if !m.MatchRules(configs, "sobaka.com", func(match RoutingMatch) bool {
		return match.Policy == "direct"
	}) {
		t.Error("sobaka.com должен совпадать с domain-suffix:")
	}
	if !m.MatchRules(configs, "music.sobaka.com", func(match RoutingMatch) bool {
		return match.Policy == "direct"
	}) {
		t.Error("music.sobaka.com должен совпадать с domain-suffix:")
	}
	if m.MatchRules(configs, "notsobaka.com", func(RoutingMatch) bool { return true }) {
		t.Error("notsobaka.com не должен совпадать")
	}

	// keyword: — подстрока в домене (без учёта регистра).
	if !m.MatchRules(configs, "www.youtube.com", func(match RoutingMatch) bool {
		return match.Policy == "proxy"
	}) {
		t.Error("www.youtube.com должен совпадать с keyword:")
	}
	if !m.MatchRules(configs, "YouTuBe.com", func(match RoutingMatch) bool {
		return match.Policy == "proxy"
	}) {
		t.Error("YouTuBe.com должен совпадать с keyword: (регистронезависимо)")
	}
}

func TestMatchRulesInlineRegex(t *testing.T) {
	m := NewRulesetManager("")
	configs := []RulesetConfig{
		{Rule: `regex:\.yandex\.ru$`, Policy: "block", Enable: true},
	}
	if !m.MatchRules(configs, "music.yandex.ru", func(match RoutingMatch) bool {
		return match.Policy == "block"
	}) {
		t.Error("music.yandex.ru должен совпадать с regex:")
	}
	if m.MatchRules(configs, "yandex.ru.evil.com", func(RoutingMatch) bool { return true }) {
		t.Error("yandex.ru.evil.com не должен совпадать")
	}
}

func TestMatchRulesInlineCIDRAndIP(t *testing.T) {
	m := NewRulesetManager("")
	configs := []RulesetConfig{
		{Rule: "cidr:10.0.0.0/8", Policy: "direct", Enable: true},
		{Rule: "ip:8.8.8.8", Policy: "block", Enable: true},
	}

	if !m.MatchRules(configs, "10.1.2.3", func(match RoutingMatch) bool {
		return match.Policy == "direct"
	}) {
		t.Error("10.1.2.3 должен совпадать с cidr:")
	}
	if m.MatchRules(configs, "11.0.0.1", func(RoutingMatch) bool { return true }) {
		t.Error("11.0.0.1 не должен совпадать")
	}
	if !m.MatchRules(configs, "8.8.8.8", func(match RoutingMatch) bool {
		return match.Policy == "block"
	}) {
		t.Error("8.8.8.8 должен совпадать с ip:")
	}
	if !m.MatchRules(configs, "::ffff:8.8.8.8", func(match RoutingMatch) bool {
		return match.Policy == "block"
	}) {
		t.Error("::ffff:8.8.8.8 должен совпадать с ip: после unmapping")
	}
}

// ── normalizeDomain / prefixContains ──────────────────────────────────────────

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		"Example.COM.": "example.com",
		"  vk.com  ":   "vk.com",
		"":             "",
	}
	for in, want := range cases {
		if got := normalizeDomain(in); got != want {
			t.Errorf("normalizeDomain(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestPrefixContains(t *testing.T) {
	cidrs := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	if !prefixContains(cidrs, netip.MustParseAddr("10.1.2.3")) {
		t.Error("10.1.2.3 должен содержаться")
	}
	if !prefixContains(cidrs, netip.MustParseAddr("::ffff:10.1.2.3")) {
		t.Error("IPv4-mapped должен быть размаплен и найден")
	}
	if !prefixContains(cidrs, netip.MustParseAddr("2001:db8::1")) {
		t.Error("2001:db8::1 должен содержаться")
	}
	if prefixContains(cidrs, netip.MustParseAddr("8.8.8.8")) {
		t.Error("8.8.8.8 не должен содержаться")
	}
}
