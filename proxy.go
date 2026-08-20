package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

const maxProxyConn = 512

// ── WireGuard netstack ────────────────────────────────────────────────────────

type wgTunnel struct {
	mu     sync.Mutex
	dev    *device.Device
	tnet   *netstack.Net
	active bool
}

var wgTun = &wgTunnel{}

func wgDial(network, addr string) (net.Conn, error) {
	wgTun.mu.Lock()
	tnet := wgTun.tnet
	active := wgTun.active
	wgTun.mu.Unlock()

	if active && tnet != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return tnet.DialContext(ctx, network, addr)
	}
	return net.DialTimeout(network, addr, 30*time.Second)
}

// wgDialStrict — только через туннель, без fallback на прямое соединение.
// Используется системным прокси: нет туннеля → ошибка → браузер получает 502.
func wgDialStrict(network, addr string) (net.Conn, error) {
	wgTun.mu.Lock()
	tnet := wgTun.tnet
	active := wgTun.active
	wgTun.mu.Unlock()

	if active && tnet != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return tnet.DialContext(ctx, network, addr)
	}
	return nil, fmt.Errorf("туннель не активен")
}

func StartWGTunnel(conf string) error {
	wgTun.mu.Lock()
	defer wgTun.mu.Unlock()

	if wgTun.dev != nil {
		wgTun.dev.Close()
		wgTun.dev = nil
		wgTun.tnet = nil
	}

	parsed, err := parseWGConf(conf)
	if err != nil {
		return fmt.Errorf("парсинг конфига: %w", err)
	}

	tun, tnet, err := netstack.CreateNetTUN(parsed.addresses, parsed.dns, parsed.mtu)
	if err != nil {
		return fmt.Errorf("netstack: %w", err)
	}

	logger := device.NewLogger(device.LogLevelSilent, "wg: ")
	dev := device.NewDevice(tun, conn.NewDefaultBind(), logger)

	if err := dev.IpcSet(parsed.ipc); err != nil {
		dev.Close()
		return fmt.Errorf("IPC: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return fmt.Errorf("Up: %w", err)
	}

	wgTun.dev = dev
	wgTun.tnet = tnet
	wgTun.active = true
	return nil
}

func StopWGTunnel() {
	wgTun.mu.Lock()
	defer wgTun.mu.Unlock()
	if wgTun.dev != nil {
		wgTun.dev.Close()
		wgTun.dev = nil
		wgTun.tnet = nil
	}
	wgTun.active = false
}

func WGTunnelActive() bool {
	wgTun.mu.Lock()
	defer wgTun.mu.Unlock()
	return wgTun.active
}

// ── Парсинг WireGuard конфига ─────────────────────────────────────────────────

type wgParsed struct {
	addresses []netip.Addr
	dns       []netip.Addr
	mtu       int
	ipc       string
}

func parseWGConf(conf string) (*wgParsed, error) {
	p := &wgParsed{mtu: 1280}
	var ipc strings.Builder
	section := ""
	peerStarted := false

	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[Interface]" {
			section = "interface"
			continue
		}
		if line == "[Peer]" {
			section = "peer"
			peerStarted = true
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])

		switch section {
		case "interface":
			switch key {
			case "PrivateKey":
				ipc.WriteString("private_key=" + wgB64ToHex(val) + "\n")
			case "Address":
				for _, a := range strings.Split(val, ",") {
					a = strings.TrimSpace(a)
					if idx := strings.Index(a, "/"); idx >= 0 {
						a = a[:idx]
					}
					if addr, err := netip.ParseAddr(a); err == nil {
						p.addresses = append(p.addresses, addr)
					}
				}
			case "DNS":
				for _, d := range strings.Split(val, ",") {
					if addr, err := netip.ParseAddr(strings.TrimSpace(d)); err == nil {
						p.dns = append(p.dns, addr)
					}
				}
			case "MTU":
				fmt.Sscanf(val, "%d", &p.mtu)
			}
		case "peer":
			switch key {
			case "PublicKey":
				ipc.WriteString("public_key=" + wgB64ToHex(val) + "\n")
			case "Endpoint":
				ipc.WriteString("endpoint=" + val + "\n")
			case "AllowedIPs":
				for _, ip := range strings.Split(val, ",") {
					ipc.WriteString("allowed_ip=" + strings.TrimSpace(ip) + "\n")
				}
			case "PersistentKeepalive":
				ipc.WriteString("persistent_keepalive_interval=" + val + "\n")
			}
		}
	}

	if len(p.addresses) == 0 {
		return nil, fmt.Errorf("нет Address в конфиге")
	}
	if !peerStarted {
		return nil, fmt.Errorf("нет [Peer] в конфиге")
	}
	if len(p.dns) == 0 {
		p.dns = []netip.Addr{netip.MustParseAddr("1.1.1.1")}
	}
	p.ipc = ipc.String()
	return p, nil
}

// parseProxyAuth читает Proxy-Authorization заголовок (Basic).
// Браузеры используют Proxy-Authorization для прокси, а не Authorization.
func parseProxyAuth(r *http.Request) (user, pass string, ok bool) {
	auth := r.Header.Get("Proxy-Authorization")
	if auth == "" {
		return "", "", false
	}
	const prefix = "Basic "
	if len(auth) < len(prefix) || auth[:len(prefix)] != prefix {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func wgB64ToHex(b64 string) string {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return b64
	}
	return fmt.Sprintf("%x", data)
}

// ── Прокси сервер ─────────────────────────────────────────────────────────────

type ProxyStats struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

type ProxyServer struct {
	mu       sync.Mutex
	socks5Ln net.Listener
	httpLn   net.Listener
	httpSrv  *http.Server // для закрытия in-flight соединений в Stop()
	running  bool
	logFn    func(msg, lv string)
	statsFn  func(ProxyStats)

	// Маршрутизация по правилам
	rulesetMu     sync.RWMutex
	rulesets      []RulesetConfig
	rulesetMgr    *RulesetManager
	defaultPolicy string

	active int32 // атомарный счётчик активных соединений
	total  int32 // всего соединений за сессию
}

func NewProxyServer(logFn func(msg, lv string), statsFn func(ProxyStats)) *ProxyServer {
	return &ProxyServer{logFn: logFn, statsFn: statsFn}
}

// SetRulesets задаёт текущий список правил маршрутизации.
func (p *ProxyServer) SetRulesets(configs []RulesetConfig) {
	p.rulesetMu.Lock()
	defer p.rulesetMu.Unlock()
	p.rulesets = append([]RulesetConfig(nil), configs...)
}

// SetDefaultPolicy задаёт политику для трафика, к которому не подошло ни одно правило.
func (p *ProxyServer) SetDefaultPolicy(policy string) {
	p.rulesetMu.Lock()
	defer p.rulesetMu.Unlock()
	p.defaultPolicy = policy
}

// getDefaultPolicy возвращает политику по умолчанию; пустая → proxy (старое поведение).
func (p *ProxyServer) getDefaultPolicy() string {
	p.rulesetMu.RLock()
	defer p.rulesetMu.RUnlock()
	if p.defaultPolicy == "" {
		return PolicyProxy
	}
	return p.defaultPolicy
}

// SetRulesetManager задаёт менеджер правил (загруженные geosite/geoip).
func (p *ProxyServer) SetRulesetManager(m *RulesetManager) {
	p.rulesetMu.Lock()
	defer p.rulesetMu.Unlock()
	p.rulesetMgr = m
}

func (p *ProxyServer) getRulesets() []RulesetConfig {
	p.rulesetMu.RLock()
	defer p.rulesetMu.RUnlock()
	return p.rulesets
}

func (p *ProxyServer) getRulesetMgr() *RulesetManager {
	p.rulesetMu.RLock()
	defer p.rulesetMu.RUnlock()
	return p.rulesetMgr
}

// ruleResult — результат применения правил маршрутизации к хосту.
type ruleResult struct {
	policy string // block | direct | proxy | ""
	rule   string
}

// route определяет политику маршрутизации для хоста.
func (p *ProxyServer) route(host string) ruleResult {
	m := p.getRulesetMgr()
	configs := p.getRulesets()
	var res ruleResult
	if m != nil && len(configs) > 0 {
		m.MatchRules(configs, host, func(match RoutingMatch) bool {
			res.policy = match.Policy
			res.rule = match.Rule
			// Порядок правил = приоритет: останавливаемся на первом совпадении.
			return true
		})
	}
	if res.policy == "" {
		res.policy = p.getDefaultPolicy()
	}
	return res
}

// dialForRoute открывает соединение согласно политике маршрутизации.
// proxy (default) → строго через туннель; direct → напрямую.
// Без туннеля proxy-политика не уходит в прямое соединение (утечка трафика) —
// возвращается ошибка, как у системного прокси (SOCKS5 → 0x05, HTTP → 502).
func (p *ProxyServer) dialForRoute(policy, network, addr string) (net.Conn, error) {
	if policy == PolicyDirect {
		return net.DialTimeout(network, addr, 30*time.Second)
	}
	return wgDialStrict(network, addr)
}

func (p *ProxyServer) connOpen() {
	active := atomic.AddInt32(&p.active, 1)
	atomic.AddInt32(&p.total, 1)
	if p.statsFn != nil {
		p.statsFn(ProxyStats{Active: int(active), Total: int(atomic.LoadInt32(&p.total))})
	}
}

func (p *ProxyServer) connClose() {
	active := atomic.AddInt32(&p.active, -1)
	if active < 0 {
		atomic.StoreInt32(&p.active, 0)
		active = 0
	}
	if p.statsFn != nil {
		p.statsFn(ProxyStats{Active: int(active), Total: int(atomic.LoadInt32(&p.total))})
	}
}

func (p *ProxyServer) connCount() int {
	return int(atomic.LoadInt32(&p.active))
}

func (p *ProxyServer) Start(host, socks5Port, httpPort string, useAuth bool, user, pass string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("уже запущен")
	}
	if socks5Port == httpPort {
		return fmt.Errorf("SOCKS5 и HTTP порты должны быть разными")
	}

	atomic.StoreInt32(&p.active, 0)
	atomic.StoreInt32(&p.total, 0)

	s5addr := net.JoinHostPort(host, socks5Port)
	s5ln, err := net.Listen("tcp", s5addr)
	if err != nil {
		return fmt.Errorf("SOCKS5 listen %s: %w", s5addr, err)
	}

	httpAddr := net.JoinHostPort(host, httpPort)
	httpLn, err := net.Listen("tcp", httpAddr)
	if err != nil {
		s5ln.Close()
		return fmt.Errorf("HTTP listen %s: %w", httpAddr, err)
	}

	p.socks5Ln = s5ln
	p.httpLn = httpLn
	p.httpSrv = nil
	p.running = true

	via := "прямое соединение"
	if WGTunnelActive() {
		via = "через туннель"
	}
	p.log(fmt.Sprintf("SOCKS5 слушает %s  (%s)", s5addr, via), "success")
	p.log(fmt.Sprintf("HTTP   слушает %s  (%s)", httpAddr, via), "success")
	if useAuth {
		p.log(fmt.Sprintf("Авторизация: %s / ***", user), "dim")
	}

	go p.acceptSocks5(s5ln, useAuth, user, pass)
	go p.acceptHTTP(httpLn, useAuth, user, pass)
	return nil
}

func (p *ProxyServer) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	if p.socks5Ln != nil {
		p.socks5Ln.Close()
	}
	if p.httpLn != nil {
		p.httpLn.Close()
	}
	if p.httpSrv != nil {
		p.httpSrv.Close() // завершает in-flight соединения и accept-горутину
		p.httpSrv = nil
	}
	p.running = false
	atomic.StoreInt32(&p.active, 0)
	if p.statsFn != nil {
		p.statsFn(ProxyStats{Active: 0, Total: int(atomic.LoadInt32(&p.total))})
	}
	p.log("Proxy остановлен.", "warn")
}

func (p *ProxyServer) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func (p *ProxyServer) log(msg, lv string) {
	if p.logFn != nil {
		p.logFn(msg, lv)
	}
}

// ── SOCKS5 ────────────────────────────────────────────────────────────────────

func (p *ProxyServer) acceptSocks5(ln net.Listener, useAuth bool, user, pass string) {
	defer func() { recover() }() // П.7
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		if p.connCount() >= maxProxyConn {
			c.Close()
			p.log("SOCKS5: лимит соединений достигнут", "warn")
			continue
		}
		go p.handleSocks5(c, useAuth, user, pass)
	}
}

func (p *ProxyServer) handleSocks5(c net.Conn, useAuth bool, user, pass string) {
	defer func() { recover() }() // П.7
	defer c.Close()
	p.connOpen()
	defer p.connClose()

	c.SetDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 2)
	if _, err := io.ReadFull(c, buf); err != nil || buf[0] != 5 {
		return
	}
	methods := make([]byte, int(buf[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}

	if useAuth {
		c.Write([]byte{5, 2})
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != 1 {
			return
		}
		uname := make([]byte, int(hdr[1]))
		if _, err := io.ReadFull(c, uname); err != nil {
			return
		}
		plen := make([]byte, 1)
		if _, err := io.ReadFull(c, plen); err != nil {
			return
		}
		pwd := make([]byte, int(plen[0]))
		if _, err := io.ReadFull(c, pwd); err != nil {
			return
		}
		if string(uname) != user || string(pwd) != pass {
			c.Write([]byte{1, 1})
			p.log("→ SOCKS5 авторизация отклонена", "warn")
			return
		}
		c.Write([]byte{1, 0})
	} else {
		c.Write([]byte{5, 0})
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil || req[0] != 5 || req[1] != 1 {
		c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}

	var host string
	switch req[3] {
	case 1:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(c, ip); err != nil {
			return
		}
		host = net.IP(ip).String()
	case 3:
		dlen := make([]byte, 1)
		if _, err := io.ReadFull(c, dlen); err != nil {
			return
		}
		domain := make([]byte, int(dlen[0]))
		if _, err := io.ReadFull(c, domain); err != nil {
			return
		}
		host = string(domain)
	case 4:
		ip := make([]byte, 16)
		if _, err := io.ReadFull(c, ip); err != nil {
			return
		}
		host = "[" + net.IP(ip).String() + "]"
	default:
		c.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(c, portBuf); err != nil {
		return
	}
	target := fmt.Sprintf("%s:%d", host, binary.BigEndian.Uint16(portBuf))

	// Маршрутизация по правилам.
	route := p.route(host)
	if route.policy == PolicyBlock {
		c.Write([]byte{5, 2, 0, 1, 0, 0, 0, 0, 0, 0}) // not allowed
		p.log(fmt.Sprintf("→ %s  [заблокировано %s]", target, route.rule), "warn")
		return
	}

	c.SetDeadline(time.Time{})
	start := time.Now()
	remote, err := p.dialForRoute(route.policy, "tcp", target)
	if err != nil {
		c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		p.log(fmt.Sprintf("→ %s: ошибка: %s", target, err), "warn")
		return
	}

	c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	via := "туннель"
	if route.policy == PolicyDirect {
		via = "напрямую"
	}
	p.log(fmt.Sprintf("→ %s  [%dms, %s]", target, time.Since(start).Milliseconds(), via), "dim")

	// П.6 — context для graceful cancel обеих горутин
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		io.Copy(remote, c)
	}()
	go func() {
		defer cancel()
		io.Copy(c, remote)
	}()
	<-ctx.Done()
	remote.Close()
}

// ── HTTP/HTTPS прокси ─────────────────────────────────────────────────────────

func (p *ProxyServer) acceptHTTP(ln net.Listener, useAuth bool, user, pass string) {
	defer func() { recover() }() // П.7
	srv := &http.Server{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // streaming — не ставим WriteTimeout
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() { recover() }()
			if p.connCount() >= maxProxyConn {
				http.Error(w, "Too many connections", http.StatusServiceUnavailable)
				p.log("HTTP: лимит соединений достигнут", "warn")
				return
			}
			p.handleHTTP(w, r, useAuth, user, pass)
		}),
	}
	p.mu.Lock()
	p.httpSrv = srv
	p.mu.Unlock()
	srv.Serve(ln)
}

func (p *ProxyServer) handleHTTP(w http.ResponseWriter, r *http.Request, useAuth bool, user, pass string) {
	if useAuth {
		// Браузеры отправляют Proxy-Authorization, не Authorization
		u, pw, ok := parseProxyAuth(r)
		if !ok || u != user || pw != pass {
			w.Header().Set("Proxy-Authenticate", `Basic realm="WinDTT"`)
			http.Error(w, "Proxy Auth Required", http.StatusProxyAuthRequired)
			return
		}
	}

	if r.Method == http.MethodConnect {
		p.connOpen()
		defer p.connClose()

		// Маршрутизация по правилам.
		route := p.route(r.Host)
		if route.policy == PolicyBlock {
			http.Error(w, "Connection blocked by ruleset", http.StatusForbidden)
			p.log(fmt.Sprintf("→ %s [CONNECT]  [заблокировано %s]", r.Host, route.rule), "warn")
			return
		}

		remote, err := p.dialForRoute(route.policy, "tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			p.log(fmt.Sprintf("→ %s: ошибка: %s", r.Host, err), "warn")
			return
		}
		defer remote.Close()
		w.WriteHeader(http.StatusOK)
		via := "туннель"
		if route.policy == PolicyDirect {
			via = "напрямую"
		}
		p.log(fmt.Sprintf("→ %s [CONNECT, %s]", r.Host, via), "dim")

		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		clientConn, _, _ := hj.Hijack()
		defer clientConn.Close()

		// Idle-timeout: дедлайн сбрасывается при каждой активности,
		// долгие стримы не обрываются по абсолютному дедлайну.
		idle := 10 * time.Minute
		ci := &idleConn{Conn: clientConn, idle: idle}
		ri := &idleConn{Conn: remote, idle: idle}
		ci.SetDeadline(time.Now().Add(idle))
		ri.SetDeadline(time.Now().Add(idle))

		// П.6 — context для graceful cancel
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { defer cancel(); io.Copy(ri, ci) }()
		go func() { defer cancel(); io.Copy(ci, ri) }()
		<-ctx.Done()
		return
	}

	p.connOpen()
	defer p.connClose()

	// Маршрутизация по правилам.
	route := p.route(r.URL.Host)
	if route.policy == PolicyBlock {
		http.Error(w, "Connection blocked by ruleset", http.StatusForbidden)
		p.log(fmt.Sprintf("→ %s %s  [заблокировано %s]", r.Method, r.URL.Host, route.rule), "warn")
		return
	}

	r.RequestURI = ""
	// Удаляем hop-by-hop заголовки (RFC 2616 §13.5.1) — они не должны
	// пересылаться через прокси.
	for _, h := range []string{
		"Proxy-Connection", "Connection", "Keep-Alive",
		"TE", "Trailer", "Transfer-Encoding", "Upgrade",
		"Proxy-Authenticate", "Proxy-Authorization",
	} {
		r.Header.Del(h)
	}
	via := "туннель"
	if route.policy == PolicyDirect {
		via = "напрямую"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return p.dialForRoute(route.policy, network, addr)
		},
	}
	resp, err := transport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	p.log(fmt.Sprintf("→ %s %s [%s]", r.Method, r.URL.Host, via), "dim")
}

// idleConn обновляет deadline при каждом успешном чтении/записи — это даёт
// idle-timeout вместо абсолютного дедлайна: долгие (дольше idle) стримы не
// обрываются, а «мёртвые» соединения без активности корректно отмирают.
type idleConn struct {
	net.Conn
	idle time.Duration
}

func (c *idleConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err == nil {
		c.Conn.SetDeadline(time.Now().Add(c.idle))
	}
	return n, err
}

func (c *idleConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if err == nil {
		c.Conn.SetDeadline(time.Now().Add(c.idle))
	}
	return n, err
}

// ── System Proxy handler (без auth, отдельный листенер) ──────────────────────

// sysProxyHandler — минимальный HTTP-прокси без аутентификации.
// Используется для WinINET system proxy на отдельном порту.
type sysProxyHandler struct {
	transport *http.Transport
}

func newSysProxyHandler() *sysProxyHandler {
	return &sysProxyHandler{
		transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return wgDialStrict(network, addr)
			},
		},
	}
}

func (h *sysProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		h.handleConnect(w, r)
	} else {
		h.handleHTTP(w, r)
	}
}

func (h *sysProxyHandler) handleConnect(w http.ResponseWriter, r *http.Request) {
	remote, err := wgDialStrict("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer remote.Close()
	w.WriteHeader(http.StatusOK)

	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	client, _, _ := hj.Hijack()
	defer client.Close()

	// Idle-timeout вместо абсолютного дедлайна.
	idle := 10 * time.Minute
	ci := &idleConn{Conn: client, idle: idle}
	ri := &idleConn{Conn: remote, idle: idle}
	ci.SetDeadline(time.Now().Add(idle))
	ri.SetDeadline(time.Now().Add(idle))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { defer cancel(); io.Copy(ri, ci) }()
	go func() { defer cancel(); io.Copy(ci, ri) }()
	<-ctx.Done()
}

func (h *sysProxyHandler) handleHTTP(w http.ResponseWriter, r *http.Request) {
	r.RequestURI = ""
	for _, hdr := range []string{
		"Proxy-Connection", "Connection", "Keep-Alive",
		"TE", "Trailer", "Transfer-Encoding", "Upgrade",
		"Proxy-Authenticate", "Proxy-Authorization",
	} {
		r.Header.Del(hdr)
	}
	resp, err := h.transport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
