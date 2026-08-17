package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── Ruleset-based routing ────────────────────────────────────────────────
//
// Реализация маршрутизации по правилам (как в Throne), с офлайн-кешем
// geosite.dat / geoip.dat из github.com/runetfreedom/russia-v2ray-rules-dat.
//
// Формат правила:  "ruleset:<тип>-<группа>"
//   - ruleset:geosite-category-ru   — домены группы CATEGORY-RU из geosite.dat
//   - ruleset:geosite-youtube       — домены группы YOUTUBE из geosite.dat
//   - ruleset:geoip-private         — подсети группы PRIVATE из geoip.dat
//
// Политика правила: block | direct | proxy.

// Routing policies
const (
	PolicyBlock  = "block"
	PolicyDirect = "direct"
	PolicyProxy  = "proxy"
)

const (
	defaultGeositeURL = "https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat"
	defaultGeoIPURL   = "https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geoip.dat"
)

// RulesetConfig — одно правило маршрутизации, хранится в Config.
type RulesetConfig struct {
	Rule   string `json:"rule"`   // "ruleset:geosite-category-ru" / "ruleset:geoip-private"
	Policy string `json:"policy"` // block | direct | proxy
	Enable bool   `json:"enable"`
}

// RoutingMatch — результат совпадения правила для хоста/IP.
type RoutingMatch struct {
	Rule   string
	Policy string
}

// Manager ─────────────────────────────────────────────────────────────────

type RulesetManager struct {
	mu         sync.RWMutex
	cacheDir   string
	geosite    map[string]*GeositeGroup
	geoip      map[string]*GeoIPGroup
	loaded     bool
	lastUpdate time.Time
	logFn      func(msg, lv string)
	progressFn func(stage string, pct int)
	viaTunnel  bool // скачивать через активный туннель (wgDialStrict)
}

func NewRulesetManager(baseDir string) *RulesetManager {
	return &RulesetManager{
		cacheDir: filepath.Join(baseDir, "rulesets"),
		geosite:  make(map[string]*GeositeGroup),
		geoip:    make(map[string]*GeoIPGroup),
	}
}

// SetLogFn задаёт функцию логирования (например, отправка события в UI).
func (m *RulesetManager) SetLogFn(fn func(msg, lv string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logFn = fn
}

// SetProgressFn задаёт функцию прогресса скачивания (stage: "geosite"/"geoip",
// pct: 0-100 или -1 если точный прогресс неизвестен).
func (m *RulesetManager) SetProgressFn(fn func(stage string, pct int)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progressFn = fn
}

// SetViaTunnel включает/выключает скачивание правил через активный туннель.
func (m *RulesetManager) SetViaTunnel(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.viaTunnel = v
}

// newClient создаёт http.Client для скачивания. При viaTunnel соединения
// идут строго через туннель (без fallback на прямое соединение).
// Вызывается только при удержании m.mu (см. UpdateRulesets).
func (m *RulesetManager) newClient() *http.Client {
	return newRulesetClient(m.viaTunnel)
}

func newRulesetClient(viaTunnel bool) *http.Client {
	if viaTunnel {
		return &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return wgDialStrict(network, addr)
				},
			},
		}
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// progress отправляет прогресс через progressFn, если он задан.
func (m *RulesetManager) progress(stage string, pct int) {
	if m.progressFn != nil {
		m.progressFn(stage, pct)
	}
}

// log отправляет сообщение через logFn, если он задан. Вызывается только при
// удержании m.mu или до старта конкурентных горутин (logFn задаётся один раз
// при старте), поэтому читается без блокировки.
func (m *RulesetManager) log(msg, lv string) {
	if m.logFn != nil {
		m.logFn(msg, lv)
	}
}

// ── Скачивание и обновление ─────────────────────────────────────────────

// UpdateRulesets скачивает свежие geosite.dat и geoip.dat в кеш-папку.
// При успехе перечитывает их в память.
func (m *RulesetManager) UpdateRulesets() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(m.cacheDir, 0o755); err != nil {
		m.log("Маршрутизация: не удалось создать кеш-папку "+m.cacheDir+": "+err.Error(), "error")
		return fmt.Errorf("кеш-папка: %w", err)
	}
	m.log("Маршрутизация: кеш-папка готова: "+m.cacheDir, "info")

	geositePath := filepath.Join(m.cacheDir, "geosite.dat")
	geoipPath := filepath.Join(m.cacheDir, "geoip.dat")

	m.log("Маршрутизация: скачиваю geosite.dat (" + defaultGeositeURL + ") ...", "info")
	if err := downloadFile(m.newClient(), defaultGeositeURL, geositePath, func(done, total int64) {
		m.progress("geosite", pctOf(done, total))
	}); err != nil {
		m.log("Маршрутизация: не удалось скачать geosite.dat: "+err.Error(), "error")
		return fmt.Errorf("geosite.dat: %w", err)
	}
	m.log("Маршрутизация: geosite.dat сохранён в "+geositePath, "info")
	m.progress("geosite", 100)

	m.log("Маршрутизация: скачиваю geoip.dat (" + defaultGeoIPURL + ") ...", "info")
	if err := downloadFile(m.newClient(), defaultGeoIPURL, geoipPath, func(done, total int64) {
		m.progress("geoip", pctOf(done, total))
	}); err != nil {
		m.log("Маршрутизация: не удалось скачать geoip.dat: "+err.Error(), "error")
		return fmt.Errorf("geoip.dat: %w", err)
	}
	m.log("Маршрутизация: geoip.dat сохранён в "+geoipPath, "info")
	m.progress("geoip", 100)

	gs, err := parseGeositeFile(geositePath)
	if err != nil {
		m.log("Маршрутизация: ошибка парсинга geosite.dat: "+err.Error(), "error")
		return fmt.Errorf("парсинг geosite: %w", err)
	}
	gi, err := parseGeoIPFile(geoipPath)
	if err != nil {
		m.log("Маршрутизация: ошибка парсинга geoip.dat: "+err.Error(), "error")
		return fmt.Errorf("парсинг geoip: %w", err)
	}

	m.geosite = gs
	m.geoip = gi
	m.loaded = true
	m.lastUpdate = time.Now()
	m.log("Маршрутизация: правила обновлены ("+fmt.Sprintf("%d", len(gs))+" групп geosite, "+fmt.Sprintf("%d", len(gi))+" групп geoip).", "success")
	return nil
}

// EnsureLoaded загружает правила из кеша с диска, если ещё не загружены.
// Если локального кеша нет — пробует скачать. Ошибки возвращаются, но
// не фатальны для работы прокси (маршрутизация просто не активна).
func (m *RulesetManager) EnsureLoaded() error {
	m.mu.RLock()
	loaded := m.loaded
	m.mu.RUnlock()
	if loaded {
		return nil
	}

	m.mu.Lock()
	if m.loaded {
		m.mu.Unlock()
		return nil
	}

	geositePath := filepath.Join(m.cacheDir, "geosite.dat")
	geoipPath := filepath.Join(m.cacheDir, "geoip.dat")

	// Пробуем загрузить с диска.
	if gs, err := parseGeositeFile(geositePath); err == nil {
		if gi, err2 := parseGeoIPFile(geoipPath); err2 == nil {
			m.geosite = gs
			m.geoip = gi
			m.loaded = true
			if fi, err := os.Stat(geositePath); err == nil {
				m.lastUpdate = fi.ModTime()
			}
			m.mu.Unlock()
			m.log("Маршрутизация: правила загружены из кеша: "+m.cacheDir, "info")
			return nil
		}
	}
	m.mu.Unlock()

	// Кеша нет — скачиваем (без удержания блокировки, чтобы не было взаимоблокировки
	// с UpdateRulesets, который сам берёт m.mu).
	m.log("Маршрутизация: локального кеша нет — скачиваю правила...", "info")
	if err := m.UpdateRulesets(); err != nil {
		m.log("Маршрутизация: не удалось загрузить правила: "+err.Error(), "error")
		return err
	}
	return nil
}

func (m *RulesetManager) Loaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded
}

func (m *RulesetManager) LastUpdate() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUpdate
}

// ListGroups возвращает доступные группы правил для автоподсказок.
// Возвращает geosite и geoip группы в отображаемом виде.
func (m *RulesetManager) ListGroups() (geosite, geoip []string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	geosite = make([]string, 0, len(m.geosite))
	for name := range m.geosite {
		geosite = append(geosite, "geosite-"+strings.ToLower(name))
	}
	sort.Strings(geosite)
	
	geoip = make([]string, 0, len(m.geoip))
	for name := range m.geoip {
		geoip = append(geoip, "geoip-"+strings.ToLower(name))
	}
	sort.Strings(geoip)
	
	return geosite, geoip
}

// pctOf переводит (done, total) в проценты. Если total неизвестен (<=0) —
// возвращает -1 (неопределённый прогресс).
func pctOf(done, total int64) int {
	if total <= 0 {
		return -1
	}
	if done >= total {
		return 100
	}
	return int(done * 100 / total)
}

func downloadFile(client *http.Client, url, path string, onProgress func(done, total int64)) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Пишем во временный файл, затем переименовываем — чтобы не оставить
	// битый кеш при обрыве скачивания.
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	body := io.Reader(resp.Body)
	if onProgress != nil {
		total := resp.ContentLength
		body = &progressReader{r: resp.Body, total: total, onProgress: onProgress}
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// progressReader считает прочитанные байты и вызывает onProgress.
type progressReader struct {
	r          io.Reader
	total      int64
	done       int64
	onProgress func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.done += int64(n)
		p.onProgress(p.done, p.total)
	}
	return n, err
}

// ── Данные групп ────────────────────────────────────────────────────────

// GeositeGroup — группа доменов из geosite.dat.
type GeositeGroup struct {
	Name   string
	suffix map[string]struct{} // поддомены/суффиксы (нижний регистр)
	full   map[string]struct{} // точные совпадения
	regex  []*regexp.Regexp    // регулярки
}

// GeoIPGroup — группа CIDR-подсетей из geoip.dat.
type GeoIPGroup struct {
	Name  string
	cidrs []netip.Prefix
}

// ── Матчинг ─────────────────────────────────────────────────────────────

func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	return strings.TrimSuffix(d, ".")
}

// MatchRules вызывает fn для каждого включённого правила, у которого хост
// совпал. Возвращает true, если fn вернула true (т.е. нужно остановиться).
func (m *RulesetManager) MatchRules(configs []RulesetConfig, host string, fn func(RoutingMatch) bool) bool {
	h := host
	if i := strings.LastIndexByte(h, ':'); i > 0 && strings.Count(h, ":") == 1 {
		h = h[:i] // убрать порт для IPv4/hostname
	}
	if ip, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil {
		return m.matchIP(configs, ip, fn)
	}
	return m.matchDomain(configs, host, fn)
}

func (m *RulesetManager) matchIP(configs []RulesetConfig, ip netip.Addr, fn func(RoutingMatch) bool) bool {
	m.mu.RLock()
	geoip := m.geoip
	m.mu.RUnlock()
	if geoip == nil {
		return false
	}
	stopped := false
	for _, rc := range configs {
		if !rc.Enable || rc.Policy == "" {
			continue
		}
		typ, group := parseRule(rc.Rule)
		if typ != "geoip" {
			continue
		}
		g, ok := geoip[strings.ToUpper(group)]
		if !ok {
			continue
		}
		if prefixContains(g.cidrs, ip) {
			if fn(RoutingMatch{Rule: rc.Rule, Policy: rc.Policy}) {
				stopped = true
				break
			}
		}
	}
	return stopped
}

func (m *RulesetManager) matchDomain(configs []RulesetConfig, host string, fn func(RoutingMatch) bool) bool {
	m.mu.RLock()
	geosite := m.geosite
	m.mu.RUnlock()
	if geosite == nil {
		return false
	}
	d := normalizeDomain(host)
	if d == "" {
		return false
	}
	stopped := false
	for _, rc := range configs {
		if !rc.Enable || rc.Policy == "" {
			continue
		}
		typ, group := parseRule(rc.Rule)
		if typ != "geosite" {
			continue
		}
		g, ok := geosite[strings.ToUpper(group)]
		if !ok {
			continue
		}
		if g.matches(d) {
			if fn(RoutingMatch{Rule: rc.Rule, Policy: rc.Policy}) {
				stopped = true
				break
			}
		}
	}
	return stopped
}

// matches проверяет домен на суффикс, точное совпадение и регулярки.
func (g *GeositeGroup) matches(domain string) bool {
	// suffix-матчинг: проверяем сам домен и все родительские уровни.
	cur := domain
	for {
		if _, ok := g.suffix[cur]; ok {
			return true
		}
		idx := strings.IndexByte(cur, '.')
		if idx < 0 {
			break
		}
		cur = cur[idx+1:]
	}
	if _, ok := g.full[domain]; ok {
		return true
	}
	for _, re := range g.regex {
		if re.MatchString(domain) {
			return true
		}
	}
	return false
}

func prefixContains(cidrs []netip.Prefix, ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range cidrs {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// parseRule разбирает "ruleset:geosite-category-ru" на ("geosite","CATEGORY-RU").
func parseRule(rule string) (typ, group string) {
	const prefix = "ruleset:"
	if !strings.HasPrefix(rule, prefix) {
		return "", ""
	}
	rest := strings.TrimPrefix(rule, prefix)
	idx := strings.IndexByte(rest, '-')
	if idx < 0 {
		return strings.ToLower(rest), ""
	}
	return strings.ToLower(rest[:idx]), rest[idx+1:]
}

// ── Парсинг protobuf ────────────────────────────────────────────────────

// readVarint декодирует protobuf varint начиная с позиции i.
// Возвращает значение и позицию сразу после байтов varint.
func readVarint(b []byte, i int) (uint64, int) {
	var v uint64
	var shift uint
	for {
		if i >= len(b) {
			return 0, i
		}
		x := b[i]
		i++
		v |= uint64(x&0x7f) << shift
		if x&0x80 == 0 {
			break
		}
		shift += 7
	}
	return v, i
}

// nextField разбирает текущее поле protobuf и возвращает (field, wireType, posAfterValue).
// Для wireType 2 возвращает также длину и позицию данных.
type pf struct {
	field, wire uint64
	start, end  int    // границы value (для wireType 2 — данные)
	varint      uint64 // значение для wireType 0
}

// parseField читает одно поле protobuf. Возвращает nil, если за пределами.
func parseField(b []byte, i, end int) *pf {
	if i >= end {
		return nil
	}
	tag, p := readVarint(b, i)
	if p > end {
		return nil
	}
	f := &pf{field: tag >> 3, wire: tag & 7}
	wt := f.wire
	if wt == 2 {
		ln, p2 := readVarint(b, p)
		f.start = p2
		f.end = p2 + int(ln)
		if f.end > end {
			f.end = end
		}
	} else if wt == 0 {
		v, p2 := readVarint(b, p)
		f.start = p
		f.end = p2
		f.varint = v
	} else if wt == 5 {
		f.start = p
		f.end = p + 4
	} else if wt == 1 {
		f.start = p
		f.end = p + 8
	} else {
		f.start = p
		f.end = p
	}
	return f
}

// parseGeositeFile — парсит бинарный geosite.dat (protobuf v2fly).
// Внешние записи: field1 (0x0a) → группа { name, домены[] }.
func parseGeositeFile(path string) (map[string]*GeositeGroup, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	groups := make(map[string]*GeositeGroup)
	i := 0
	for i < len(data) {
		if i+1 >= len(data) || data[i] != 0x0a {
			break
		}
		ln, p := readVarint(data, i+1)
		groupStart := p
		groupEnd := p + int(ln)
		if groupEnd > len(data) {
			break
		}
		g := &GeositeGroup{Name: "", suffix: map[string]struct{}{}, full: map[string]struct{}{}}
		gi := groupStart
		var name string
		for gi < groupEnd {
			f := parseField(data, gi, groupEnd)
			if f == nil {
				break
			}
			switch f.field {
			case 1: // имя группы
				name = string(data[f.start:f.end])
			case 2: // домен
				domType, pattern := parseDomainMsg(data[f.start:f.end])
				insertGeositeDomain(g, domType, pattern)
			}
			gi = f.end
		}
		g.Name = name
		groups[strings.ToUpper(name)] = g
		i = groupEnd
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("нет групп в geosite")
	}
	return groups, nil
}

// parseDomainMsg разбирает сообщение домена: type (field1 varint) + pattern (field2 str).
func parseDomainMsg(b []byte) (domType int, pattern string) {
	i := 0
	for i < len(b) {
		f := parseField(b, i, len(b))
		if f == nil {
			break
		}
		switch f.field {
		case 1:
			v, _ := readVarint(b, f.start)
			domType = int(v)
		case 2:
			pattern = string(b[f.start:f.end])
		}
		i = f.end
	}
	return domType, pattern
}

// insertGeositeDomain добавляет домен по типу v2ray:
// 0 = plain (любой), 1 = regex, 2 = domain/suffix, 3 = full (точное).
func insertGeositeDomain(g *GeositeGroup, domType int, pattern string) {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return
	}
	switch domType {
	case 1:
		re, err := regexp.Compile(p)
		if err == nil {
			g.regex = append(g.regex, re)
		}
	case 2:
		// Суффикс; "domain" без ведущей точки означает поддомены включая сам домен.
		g.suffix[normalizeDomain(p)] = struct{}{}
	case 3:
		g.full[normalizeDomain(p)] = struct{}{}
	default:
		// plain: обрабатываем как суффикс (наиболее привычно для пользователя)
		g.suffix[normalizeDomain(p)] = struct{}{}
	}
}

// parseGeoIPFile — парсит бинарный geoip.dat (protobuf v2fly).
func parseGeoIPFile(path string) (map[string]*GeoIPGroup, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	groups := make(map[string]*GeoIPGroup)
	i := 0
	for i < len(data) {
		if i+1 >= len(data) || data[i] != 0x0a {
			break
		}
		ln, p := readVarint(data, i+1)
		groupStart := p
		groupEnd := p + int(ln)
		if groupEnd > len(data) {
			break
		}
		g := &GeoIPGroup{}
		gi := groupStart
		var code string
		for gi < groupEnd {
			f := parseField(data, gi, groupEnd)
			if f == nil {
				break
			}
			switch f.field {
			case 1: // код страны
				code = string(data[f.start:f.end])
			case 2: // CIDR
				if pfx, ok := parseCIDRMsg(data[f.start:f.end]); ok {
					g.cidrs = append(g.cidrs, pfx)
				}
			}
			gi = f.end
		}
		g.Name = code
		groups[strings.ToUpper(code)] = g
		i = groupEnd
	}
	return groups, nil
}

// parseCIDRMsg разбирает сообщение CIDR: ip (field1 bytes) + prefix (field2 varint).
func parseCIDRMsg(b []byte) (netip.Prefix, bool) {
	var ipBytes []byte
	var prefix int
	i := 0
	for i < len(b) {
		f := parseField(b, i, len(b))
		if f == nil {
			break
		}
		switch f.field {
		case 1:
			ipBytes = append(ipBytes[:0], b[f.start:f.end]...)
		case 2:
			v, _ := readVarint(b, f.start)
			prefix = int(v)
		}
		i = f.end
	}
	if len(ipBytes) == 0 {
		return netip.Prefix{}, false
	}
	ip, ok := netip.AddrFromSlice(ipBytes)
	if !ok {
		return netip.Prefix{}, false
	}
	pfx := netip.PrefixFrom(ip.Unmap(), prefix)
	return pfx, true
}

// validateRule проверяет корректность строки правила вида ruleset:geoip-private.
func validateRule(rule string) bool {
	typ, group := parseRule(rule)
	return (typ == "geosite" || typ == "geoip") && group != ""
}

// normalizeRule нормализует правило: если нет префикса "ruleset:", добавляет его.
func normalizeRule(rule string) string {
	if !strings.HasPrefix(strings.ToLower(rule), "ruleset:") {
		return "ruleset:" + rule
	}
	return rule
}

// validPolicy возвращает true, если политика поддерживается.
func validPolicy(p string) bool {
	switch p {
	case PolicyBlock, PolicyDirect, PolicyProxy:
		return true
	}
	return false
}
