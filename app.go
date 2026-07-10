package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"
)

const AppVersion = "0.2.3.1"

// ── Config ────────────────────────────────────────────────────────────────────

type Config struct {
	VK          string `json:"vk"`
	Srv         string `json:"srv"`
	Sec         string `json:"sec"`
	N           string `json:"n"`
	Listen      string `json:"listen"`
	CaptchaMode  string `json:"captcha_mode"`
	ObfsMode     string `json:"obfs_mode"`
	Fingerprint  string `json:"fingerprint"`
	DeviceID     string `json:"device_id"`
	PxHost      string `json:"px_host"`
	PxSocksPort string `json:"px_socks_port"`
	PxHttpPort  string `json:"px_http_port"`
	PxUseAuth   bool   `json:"px_use_auth"`
	PxUser      string `json:"px_user"`
	PxPass      string `json:"px_pass"`
	Theme       string `json:"theme"`
}

// ── Log entry ─────────────────────────────────────────────────────────────────

type LogEntry struct {
	Ts  string `json:"ts"`
	Msg string `json:"msg"`
	Lv  string `json:"lv"`
}

// ── Worker stats ──────────────────────────────────────────────────────────────

type WorkerStats struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

// ── App ───────────────────────────────────────────────────────────────────────

// tunnelParams хранит параметры запуска туннеля для автоперезапуска
type tunnelParams struct {
	vk, srv, sec, n, listen, captchaMode, deviceID, fingerprint, obfsMode string
}

type App struct {
	ctx context.Context

	// Пути
	baseDir           string
	clientExe         string
	configFile        string
	overrideClientExe string
	serverBinary      []byte // встроенный wdtt-server (Linux amd64)
	deployScript      []byte // встроенный deploy.sh

	// Туннель
	tunnelMu      sync.Mutex
	tunnelProc    *exec.Cmd
	tunnelStdin   io.WriteCloser
	tunnelRunning bool
	tunnelPaused  bool
	totalWorkers  int
	lastTrafficMB float64
	lastStatTime  time.Time
	wgConfPath string // путь к wg-turn.conf рядом с wdtt-client.exe
	pingStop   chan struct{}

	// Watchdog + Circuit Breaker
	watchdogStop     chan struct{}
	lastTunnelParams *tunnelParams // параметры для автоперезапуска
	restartAttempts  int
	floodCount       int
	mismatchCount    int
	refusedCount     int
	wrapTimeoutCount int
	lastActiveAt     int64 // unix ms последней активности воркеров
	procStartedAt    int64 // unix ms запуска процесса
	lastCBReset      int64 // unix ms последнего сброса circuit breaker
	activeWorkers int

	// Закрытие
	closeAllowed atomic.Bool

	// Системный прокси (WinINET)
	sysProxyOn  atomic.Bool
	sysProxyLn  net.Listener   // отдельный HTTP-прокси без auth
	sysProxySrv *http.Server   // для graceful close

	// Прокси (SOCKS5 + HTTP)
	proxy         *ProxyServer
	socksStats    struct {
		mu     sync.Mutex
		active int
		total  int
	}

	// Конфиг
	cfg Config
}

// ── Regex ────────────────────────────────────────────────────────────────────

var (
	reStat    = regexp.MustCompile(`(?i)\[СТАТИСТИКА\]|\[STAT`)
	reWorkers = regexp.MustCompile(`(?i)всего:\s*(\d+)|осталось:\s*(\d+)|активных:\s*(\d+)`)
	reTraffic = regexp.MustCompile(`(?i)трафик:\s*([\d.]+)\s*(МБ|MB|KB|КБ|GB|ГБ)`)
	reFatal   = regexp.MustCompile(`(?i)фатальн|fatal_auth|fatal auth`)
)


// extractClientExe распаковывает встроенный wdtt-client.exe во временную папку.
func extractClientExe(data []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "wdtt-client-*.exe")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()
	if _, err := tmpFile.Write(data); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}
	// На Windows это no-op (там нет exec-бита), на macOS/Linux — обязательно,
	// иначе exec.Command() падает с "permission denied".
	if err := os.Chmod(tmpFile.Name(), 0o755); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}
	return tmpFile.Name(), nil
}

// NewAppWithExe создаёт App с явным путём к wdtt-client.exe (из embed).
// Если exePath пустой — startup ищет exe рядом с WinDTT.exe.
func NewAppWithExe(exePath string) *App {
	a := &App{overrideClientExe: exePath}
	a.proxy = NewProxyServer(a.socksLog, a.onProxyStats)
	return a
}

func NewApp() *App {
	a := &App{}
	a.proxy = NewProxyServer(a.socksLog, a.onProxyStats)
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Определяем пути
	exe, _ := os.Executable()
	a.baseDir = filepath.Dir(exe)
	a.configFile = filepath.Join(a.baseDir, "windtt_config.json")

	if a.overrideClientExe != "" {
		a.clientExe = a.overrideClientExe
	} else {
		a.clientExe = filepath.Join(a.baseDir, "wdtt-client.exe")
	}

	// Загружаем конфиг
	a.loadConfig()

	// Крэш-восстановление: если остался бэкап системного прокси, значит прошлый
	// сеанс завершился аварийно с включённым перенаправлением — возвращаем настройки.
	if s, ok := a.loadSysProxyBackup(); ok {
		sysProxyRestore(s)
		a.clearSysProxyBackup()
		a.log("Обнаружен и восстановлен системный прокси от прошлого сеанса.", "warn")
	}
}

// beforeClose вызывается Wails перед закрытием окна.
func (a *App) beforeClose(ctx context.Context) bool {
	if a.closeAllowed.Load() {
		return false
	}

	a.tunnelMu.Lock()
	tunnelActive := a.tunnelRunning
	a.tunnelMu.Unlock()
	socksActive := a.proxy.Running()

	// Формируем текст диалога
	var msg string
	if tunnelActive || socksActive {
		parts := []string{}
		if tunnelActive {
			parts = append(parts, "туннель")
		}
		if socksActive {
			parts = append(parts, "Proxy")
		}
		what := strings.Join(parts, " и ")
		msg = "Остановить " + what + " и закрыть программу?"
	} else {
		msg = "Закрыть WinDTT?"
	}

	btn, _ := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "WinDTT",
		Message:       msg,
		Buttons:       []string{"Закрыть", "Отмена"},
		DefaultButton: "Закрыть",
		CancelButton:  "Отмена",
	})

	cancelled := btn == "Отмена" || btn == "Cancel" || btn == "No" || btn == ""
	if cancelled {
		return true
	}

	// Останавливаем сервисы если запущены
	if tunnelActive || socksActive {
		go func() {
			// Системный прокси снимаем первым — восстановить настройки до выхода.
			if a.sysProxyOn.Load() {
				a.SystemProxyDisable()
			}
			if tunnelActive {
				a.log("Останавливаю туннель...", "warn")
				a.tunnelSend("STOP")
				for i := 0; i < 40; i++ {
					time.Sleep(100 * time.Millisecond)
					a.tunnelMu.Lock()
					running := a.tunnelRunning
					a.tunnelMu.Unlock()
					if !running {
						break
					}
				}
				a.tunnelMu.Lock()
				proc := a.tunnelProc
				still := a.tunnelRunning
				a.tunnelMu.Unlock()
				if still && proc != nil {
					proc.Process.Kill()
				}
				a.log("Туннель остановлен.", "warn")
			}
			if socksActive {
				a.log("Останавливаю локальный прокси...", "warn")
				a.SocksStop()
				a.log("Локальный прокси остановлен.", "warn")
			}
			a.closeAllowed.Store(true)
			runtime.Quit(ctx)
		}()
		return true
	}

	// Ничего не запущено — закрываем сразу
	return false
}

func (a *App) shutdown(ctx context.Context) {
	a.SystemProxyDisable() // снять перенаправление и восстановить настройки
	a.TunnelStop()
	StopWGTunnel()
	a.SocksStop()
}

// ── Config API ────────────────────────────────────────────────────────────────

func (a *App) loadConfig() {
	data, err := os.ReadFile(a.configFile)
	if err != nil {
		return
	}
	json.Unmarshal(data, &a.cfg)
}

func (a *App) GetConfig() Config {
	return a.cfg
}

func (a *App) SaveConfig(cfg Config) {
	a.cfg = cfg
	a.persistConfig()
}

func (a *App) GetVersion() string {
	return AppVersion
}

func (a *App) GetTheme() string {
	if a.cfg.Theme == "" {
		return "light"
	}
	return a.cfg.Theme
}

func (a *App) SetTheme(theme string) {
	a.cfg.Theme = theme
	a.persistConfig()
}

func (a *App) GetClientExeExists() bool {
	_, err := os.Stat(a.clientExe)
	return err == nil
}

func (a *App) GetClientExePath() string {
	return a.clientExe
}

// ── Parse wdtt:// ─────────────────────────────────────────────────────────────

type ParseResult struct {
	OK     bool   `json:"ok"`
	Server string `json:"server"`
	Hash   string `json:"hash"`
	Secret string `json:"secret"`
}

func (a *App) ParseWdtt(link string) ParseResult {
	s := strings.TrimSpace(link)
	if !strings.HasPrefix(s, "wdtt://") {
		return ParseResult{}
	}
	parts := strings.Split(s[7:], ":")
	if len(parts) < 6 {
		return ParseResult{}
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil || port < 1 || port > 65535 {
		return ParseResult{}
	}
	return ParseResult{
		OK:     true,
		Server: parts[0] + ":" + parts[1],
		Hash:   parts[5],
		Secret: parts[4],
	}
}

// shellQuote экранирует строку для безопасной передачи в shell
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// persistConfig сохраняет конфиг с правами 0600 (содержит секреты).
func (a *App) persistConfig() {
	data, _ := json.MarshalIndent(a.cfg, "", "  ")
	_ = os.WriteFile(a.configFile, data, 0600)
}

// sshHostKeyCallback сверяет ключ хоста с ожидаемым SHA-256 отпечатком
// (формат "AA:BB:...", как отдаёт DeployGetFingerprint). Fail-closed.
func sshHostKeyCallback(expectedFP string) ssh.HostKeyCallback {
	want := strings.ToUpper(strings.TrimSpace(expectedFP))
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		digest := sha256.Sum256(key.Marshal())
		pairs := make([]string, 32)
		for i, b := range digest {
			pairs[i] = fmt.Sprintf("%02X", b)
		}
		got := strings.Join(pairs, ":")
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			return fmt.Errorf("fingerprint не совпал — возможна подмена хоста (MITM).\n  ожидался: %s\n  получен:  %s", want, got)
		}
		return nil
	}
}

// validPort проверяет, что строка — корректный порт 1..65535,
// и возвращает нормализованное значение.
func validPort(s string) (string, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return "", false
	}
	return strconv.Itoa(n), true
}

var (
	reBotToken = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]{20,}$`)
	reAdminID  = regexp.MustCompile(`^\d{1,20}$`)
)

func (a *App) socksLog(msg, lv string) {
	ts := time.Now().Format("15:04:05")
	runtime.EventsEmit(a.ctx, "socks:log", LogEntry{Ts: ts, Msg: msg, Lv: lv})
}

func (a *App) onProxyStats(stats ProxyStats) {
	runtime.EventsEmit(a.ctx, "socks:stats", stats)
}

func (a *App) deployLog(msg, lv string) {
	ts := time.Now().Format("15:04:05")
	runtime.EventsEmit(a.ctx, "deploy:log", LogEntry{Ts: ts, Msg: msg, Lv: lv})
}

// ── Tunnel API ────────────────────────────────────────────────────────────────

func (a *App) log(msg, lv string) {
	ts := time.Now().Format("15:04:05")
	runtime.EventsEmit(a.ctx, "log", LogEntry{Ts: ts, Msg: msg, Lv: lv})
}

func (a *App) classifyLine(line string) (label, level string) {
	ll := strings.ToLower(line)
	switch {
	case reFatal.MatchString(line):
		return "", "error"
	case strings.Contains(ll, "error") || strings.Contains(ll, "ошибка") ||
		strings.Contains(ll, "failed") || strings.Contains(ll, "failure"):
		return "", "error"
	case strings.Contains(ll, "handshake") || strings.Contains(ll, "рукопожати") ||
		strings.Contains(ll, "dtls"):
		return "Handshake", "info"
	case strings.Contains(ll, "turn") || strings.Contains(ll, "relay") ||
		strings.Contains(ll, "alloc"):
		return "TURN relay", "info"
	case strings.Contains(ll, "воркер") || strings.Contains(ll, "worker") ||
		strings.Contains(ll, "дисп") || strings.Contains(ll, "группа"):
		return "Воркеры", "info"
	case strings.Contains(ll, "сессия") || strings.Contains(ll, "session") ||
		strings.Contains(ll, "udp") || strings.Contains(ll, "tcp"):
		return "Подключение", "info"
	case strings.Contains(line, "═══") || strings.Contains(ll, "слушаю") ||
		strings.Contains(ll, "listen"):
		return "Туннель активен", "success"
	case strings.Contains(ll, "готов к работе") || strings.Contains(ll, "ready"):
		return "Туннель активен", "success"
	case strings.Contains(ll, "пауза") || strings.Contains(ll, "pause"):
		return "Пауза", "warn"
	case strings.Contains(ll, "возобновл") || strings.Contains(ll, "resum"):
		return "Возобновлён", "success"
	default:
		return "", "info"
	}
}


// generateUUID генерирует случайный UUID v4 без внешних зависимостей.

func cryptoReader() io.Reader {
	return rand.Reader
}

func generateUUID() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(cryptoReader(), b); err != nil {
		// Fallback на time-based если crypto недоступен
		t := time.Now().UnixNano()
		for i := 0; i < 16; i++ {
			b[i] = byte(t >> (i * 4))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (a *App) TunnelStart(
	vk, srv, sec, n, listen, captchaMode, deviceID, fingerprint, obfsMode string,
) string {
	a.tunnelMu.Lock()
	defer a.tunnelMu.Unlock()

	if a.tunnelRunning {
		return "already running"
	}
	if _, err := os.Stat(a.clientExe); err != nil {
		return "wdtt-client.exe не найден"
	}

	// Запоминаем путь к wg-turn.conf — go_client создаст его рядом с собой
	exeDir := filepath.Dir(a.clientExe)
	a.wgConfPath = filepath.Join(exeDir, "wg-turn.conf")

	// Генерируем device_id если пустой и сохраняем в конфиг
	if deviceID == "" {
		deviceID = generateUUID()
		a.cfg.DeviceID = deviceID
		a.persistConfig()
	}

	totalN, _ := strconv.Atoi(n)
	a.totalWorkers = totalN
	a.activeWorkers = 0

	args := []string{
		"-peer", srv,
		"-vk", vk,
		"-password", sec,
		"-n", n,
		"-listen", listen,
		"-device-id", deviceID,
		"-captcha-mode", captchaMode,
	}
	if fingerprint != "" && fingerprint != "chrome" {
		args = append(args, "-fingerprint", fingerprint)
	}
	if obfsMode != "" && obfsMode != "audio" {
		args = append(args, "-obfs", obfsMode)
	}

	cmd := exec.Command(a.clientExe, args...)
	cmd.Dir = filepath.Dir(a.clientExe) // wg-turn.conf создаётся в этой папке
	cmd.SysProcAttr = sysProcAttr()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err.Error()
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err.Error()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err.Error()
	}

	if err := cmd.Start(); err != nil {
		return err.Error()
	}

	a.tunnelProc = cmd
	a.tunnelStdin = stdin
	a.tunnelRunning = true
	a.tunnelPaused = false
	a.pingStop = make(chan struct{})
	go a.pingLoop(a.pingStop)
	// Сохраняем параметры для автоперезапуска
	a.lastTunnelParams = &tunnelParams{vk, srv, sec, n, listen, captchaMode, deviceID, fingerprint, obfsMode}
	a.procStartedAt = time.Now().UnixMilli()
	a.lastActiveAt = 0
	a.floodCount = 0
	a.mismatchCount = 0
	a.refusedCount = 0
	a.wrapTimeoutCount = 0
	a.lastCBReset = 0
	a.watchdogStop = make(chan struct{})
	go a.watchdog(a.watchdogStop)

	startTs := time.Now()

	a.log("─────────────────────────────────────────────────", "dim")
	a.log(fmt.Sprintf("▶  Запуск wdtt-client  %s", time.Now().Format("15:04:05")), "dim")
	// Логируем команду с замаскированным паролем
	safeArgs := make([]string, len(args))
	copy(safeArgs, args)
	for i, a2 := range safeArgs {
		if a2 == "-password" && i+1 < len(safeArgs) {
			safeArgs[i+1] = "***"
		}
	}
	a.log("$ "+a.clientExe+" "+strings.Join(safeArgs, " "), "dim")
	a.log(fmt.Sprintf("   PID %d", cmd.Process.Pid), "dim")

	runtime.EventsEmit(a.ctx, "tunnel:status", map[string]interface{}{
		"running": true, "paused": false,
	})

	// Читаем оба потока; финализируем только после того, как оба пайпа дочитаны
	// до EOF (требование os/exec — нельзя звать Wait() во время чтения пайпов).
	var streamsWG sync.WaitGroup
	streamsWG.Add(2)
	go func() { defer streamsWG.Done(); a.readStream(stderr, startTs) }() // логи go_client
	go func() { defer streamsWG.Done(); a.readStream(stdout, startTs) }() // WireGuard конфиг
	go func() {
		streamsWG.Wait()
		cmd.Wait()
		a.finalizeTunnel(startTs)
	}()

	return ""
}

func (a *App) readStream(r io.Reader, startTs time.Time) {
	defer func() {
		if rec := recover(); rec != nil {
			a.log(fmt.Sprintf("   [паника в readStream] %v", rec), "error")
		}
	}()
	scanner := bufio.NewScanner(r)
	// Увеличиваем буфер до 1MB — go_client может выводить длинные строки
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	lastStage := ""

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		ll := strings.ToLower(line)

		// Статистика — всегда dim
		if reStat.MatchString(line) {
			if m := reWorkers.FindStringSubmatch(line); m != nil {
				var n int
				if m[3] != "" {
					n, _ = strconv.Atoi(m[3])
				}
				a.tunnelMu.Lock()
				a.activeWorkers = n
				total := a.totalWorkers
				a.tunnelMu.Unlock()
				runtime.EventsEmit(a.ctx, "tunnel:workers", WorkerStats{Active: n, Total: total})
			}
			// Парсим трафик и считаем скорость
			if m := reTraffic.FindStringSubmatch(line); m != nil {
				currentMB, _ := strconv.ParseFloat(m[1], 64)
				// Конвертируем в МБ
				unit := strings.ToUpper(m[2])
				switch {
				case strings.Contains(unit, "KB") || strings.Contains(unit, "КБ"):
					currentMB /= 1024
				case strings.Contains(unit, "GB") || strings.Contains(unit, "ГБ"):
					currentMB *= 1024
				}
				a.tunnelMu.Lock()
				now := time.Now()
				var speedKBs float64
				if !a.lastStatTime.IsZero() && now.Sub(a.lastStatTime) > 0 {
					diff := currentMB - a.lastTrafficMB
					if diff < 0 { diff = 0 }
					dt := now.Sub(a.lastStatTime).Seconds()
					speedKBs = (diff * 1024) / dt
				}
				a.lastTrafficMB = currentMB
				a.lastStatTime = now
				a.tunnelMu.Unlock()
				runtime.EventsEmit(a.ctx, "tunnel:stats", map[string]interface{}{
					"trafficMB": currentMB,
					"speedKBs":  speedKBs,
				})
			}
			a.log("   "+line, "dim")
			continue
		}

		// Капча
		if strings.Contains(ll, "captcha") || strings.Contains(ll, "капча") ||
			strings.Contains(ll, "смарт") {
			if strings.Contains(ll, "требуется") || strings.Contains(ll, "needed") ||
				strings.Contains(ll, "нужна") || strings.Contains(ll, "запрос") {
				runtime.EventsEmit(a.ctx, "tunnel:captcha", line)
			}
			a.log("⚠  "+line, "warn")
			continue
		}

		// Счётчик воркеров из диспетчера
		if strings.Contains(ll, "дисп") || strings.Contains(ll, "disp") {
			if m := reWorkers.FindStringSubmatch(line); m != nil {
				var n int
				if m[1] != "" {
					n, _ = strconv.Atoi(m[1])
				} else if m[2] != "" {
					n, _ = strconv.Atoi(m[2])
				}
				a.tunnelMu.Lock()
				a.activeWorkers = n
				total := a.totalWorkers
				a.tunnelMu.Unlock()
				runtime.EventsEmit(a.ctx, "tunnel:workers", WorkerStats{Active: n, Total: total})
			}
		}

		label, level := a.classifyLine(line)

		// Circuit Breaker — проверяем строку на критические ошибки
		a.checkCircuitBreaker(line)

		// WireGuard конфиг — читаем файл когда go_client сообщает о сохранении
		if strings.Contains(line, "[КОНФИГ]") && strings.Contains(line, "wg-turn.conf") {
			a.tunnelMu.Lock()
			confPath := a.wgConfPath
			a.tunnelMu.Unlock()
			if confPath != "" {
				if data, err := os.ReadFile(confPath); err == nil {
					conf := strings.TrimSpace(string(data))
					runtime.EventsEmit(a.ctx, "tunnel:wgconfig", conf)
					// Поднимаем WireGuard userspace для прокси
					go func() {
						if err := StartWGTunnel(conf); err != nil {
							a.log("   [WG] Ошибка netstack: "+err.Error(), "warn")
						} else {
							a.log("   [WG] Userspace туннель активен — прокси работает через туннель.", "success")
						}
					}()
				}
			}
		}


		// Заголовок этапа — только при смене
		parallelStages := map[string]bool{
			"Handshake": true, "TURN relay": true,
			"Подключение": true, "Воркеры": true,
		}
		if label != "" && label != lastStage {
			if !parallelStages[label] {
				a.log("◆  "+label, level)
			} else if label != lastStage {
				a.log("◆  "+label, level)
			}
			lastStage = label
		}

		// Финальный статус
		if level == "success" && strings.Contains(line, "═══") {
			total := time.Since(startTs).Seconds()
			a.log("   "+line, "success")
			a.log(fmt.Sprintf("   ✔  Подключено за %.1fs", total), "success")
			continue
		}

		a.log("   "+line, level)
	}
}

// finalizeTunnel выполняется один раз при завершении процесса туннеля
// (guard через a.tunnelRunning). Освобождает ресурсы и снимает системный прокси.
func (a *App) finalizeTunnel(startTs time.Time) {
	a.tunnelMu.Lock()
	if !a.tunnelRunning { // уже финализировано (watchdog/повторный вызов)
		a.tunnelMu.Unlock()
		return
	}
	a.tunnelRunning = false
	a.tunnelPaused = false
	a.tunnelProc = nil
	a.tunnelStdin = nil
	a.lastTrafficMB = 0
	a.lastStatTime = time.Time{}
	ps := a.pingStop
	a.pingStop = nil
	ws := a.watchdogStop
	a.watchdogStop = nil
	a.tunnelMu.Unlock()

	if ps != nil {
		close(ps)
	}
	if ws != nil {
		close(ws)
	}

	// Системный прокси остаётся включённым, но без туннеля запросы будут отклоняться.
	if a.sysProxyOn.Load() {
		a.socksLog("⚠ Туннель остановлен — системный прокси активен, но запросы будут отклоняться до перезапуска туннеля.", "warn")
	}

	total := time.Since(startTs).Seconds()
	a.log(fmt.Sprintf("■  wdtt-client завершён  время=%.1fs", total), "warn")
	a.log("─────────────────────────────────────────────────", "dim")
	runtime.EventsEmit(a.ctx, "tunnel:status", map[string]interface{}{
		"running": false, "paused": false,
	})
	runtime.EventsEmit(a.ctx, "tunnel:workers", WorkerStats{Active: 0, Total: a.totalWorkers})
}

func (a *App) tunnelSend(cmd string) {
	a.tunnelMu.Lock()
	defer a.tunnelMu.Unlock()
	if a.tunnelStdin != nil {
		fmt.Fprintln(a.tunnelStdin, cmd)
	}
}

// ── Circuit Breaker — разбор строк лога ───────────────────────────────────────

func (a *App) checkCircuitBreaker(line string) {
	ll := strings.ToLower(line)
	a.tunnelMu.Lock()
	defer a.tunnelMu.Unlock()

	// Сброс счётчиков каждые 60 секунд (как в Android)
	now := time.Now().UnixMilli()
	if a.lastCBReset == 0 { a.lastCBReset = now }
	if now-a.lastCBReset > 60_000 {
		a.floodCount = 0
		a.mismatchCount = 0
		a.refusedCount = 0
		a.lastCBReset = now
	}

	switch {
	case strings.Contains(ll, "flood control"):
		a.floodCount++
		if a.floodCount >= 5 {
			go a.handleCritical("⚠ Flood Control — VK ограничил IP. Подождите.")
		}
	case strings.Contains(ll, "ip mismatch"):
		a.mismatchCount++
		if a.mismatchCount >= 5 {
			go a.handleCritical("⚠ IP Mismatch — IP изменился. Переподключитесь.")
		}
	case strings.Contains(ll, "connection refused") || strings.Contains(ll, "i/o timeout"):
		a.refusedCount++
		if a.refusedCount >= 400 {
			go a.handleCritical("⚠ 400+ таймаутов — нет сети. Отключение.")
		}
	case strings.Contains(ll, "wrap") && strings.Contains(ll, "timeout"):
		a.wrapTimeoutCount++
		if a.wrapTimeoutCount >= 3 && a.lastActiveAt == 0 && time.Now().UnixMilli()-a.procStartedAt > 30_000 {
			go a.handleCritical("⚠ Неверный пароль или несовместимый WRAP. Остановка.")
		}
	case strings.Contains(ll, "активных:"):
		// Парсим активных воркеров для lastActiveAt
		if strings.Contains(ll, "активных: 0") || strings.Contains(ll, "активных:0") {
			// не обновляем
		} else {
			a.lastActiveAt = time.Now().UnixMilli()
			a.restartAttempts = 0
		}
	case strings.Contains(ll, "call not found") || (strings.Contains(ll, "9000") && strings.Contains(ll, "error")):
		// hash error — можно добавить переключение хеша в будущем
	}
}

func (a *App) handleCritical(msg string) {
	a.log(msg, "error")
	a.TunnelStop()
}

// ── Watchdog ──────────────────────────────────────────────────────────────────

func (a *App) watchdog(stop chan struct{}) {
	defer func() { recover() }()

	// Даём 10 секунд на старт
	select {
	case <-stop:
		return
	case <-time.After(10 * time.Second):
	}

	zeroWorkersSince := int64(0)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			a.tunnelMu.Lock()
			proc := a.tunnelProc
			running := a.tunnelRunning
			workers := a.activeWorkers
			a.tunnelMu.Unlock()

			if !running {
				return
			}

			// Процесс упал — перезапускаем
			if proc == nil || proc.ProcessState != nil {
				backoff := a.calcBackoff()
				a.log(fmt.Sprintf("⚠ Процесс упал. Перезапуск через %ds (попытка %d)...", backoff/1000, a.restartAttempts+1), "warn")
				time.Sleep(time.Duration(backoff) * time.Millisecond)
				a.tunnelMu.Lock()
				stillRunning := a.tunnelRunning
				a.tunnelMu.Unlock()
				if stillRunning {
					a.restartTunnel()
				}
				return
			}

			// Зомби: процесс жив но 0 воркеров > 90 секунд
			if workers <= 0 {
				if zeroWorkersSince == 0 {
					zeroWorkersSince = time.Now().UnixMilli()
				} else if time.Now().UnixMilli()-zeroWorkersSince > 90_000 {
					a.log("⚠ Зомби-процесс (0 воркеров 90с). Перезапуск...", "warn")
					a.restartTunnel()
					return
				}
			} else {
				zeroWorkersSince = 0
				a.tunnelMu.Lock()
				a.restartAttempts = 0
				a.tunnelMu.Unlock()
			}
		}
	}
}

func (a *App) calcBackoff() int64 {
	a.tunnelMu.Lock()
	attempts := a.restartAttempts
	a.tunnelMu.Unlock()
	backoff := int64(1000)
	for i := 0; i < attempts && backoff < 30_000; i++ {
		backoff *= 2
	}
	if backoff > 30_000 {
		backoff = 30_000
	}
	return backoff
}

func (a *App) restartTunnel() {
	a.tunnelMu.Lock()
	params := a.lastTunnelParams
	a.restartAttempts++
	a.tunnelMu.Unlock()

	if params == nil {
		return
	}

	a.log("↺ Перезапуск туннеля...", "warn")
	// Убиваем старый процесс
	a.tunnelMu.Lock()
	proc := a.tunnelProc
	a.tunnelMu.Unlock()
	if proc != nil && proc.Process != nil {
		proc.Process.Kill()
	}

	// Ждём завершения
	time.Sleep(1 * time.Second)

	a.tunnelMu.Lock()
	a.tunnelRunning = false
	a.tunnelProc = nil
	a.tunnelStdin = nil
	a.tunnelMu.Unlock()

	// Перезапускаем с теми же параметрами
	if err := a.TunnelStart(params.vk, params.srv, params.sec, params.n,
		params.listen, params.captchaMode, params.deviceID, params.fingerprint, params.obfsMode); err != "" {
		a.log("✗ Ошибка перезапуска: "+err, "error")
	}
}

func (a *App) pingLoop(stop chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !WGTunnelActive() {
				continue // туннель не поднят — пинг бессмыслен
			}
			start := time.Now()
			c, err := wgDial("tcp", "cp.cloudflare.com:80")
			if err == nil {
				ms := time.Since(start).Milliseconds()
				c.Close()
				runtime.EventsEmit(a.ctx, "tunnel:ping", ms)
			}
		}
	}
}

func (a *App) TunnelPause() {
	a.tunnelSend("PAUSE")
	a.tunnelMu.Lock()
	a.tunnelPaused = true
	a.tunnelMu.Unlock()
	runtime.EventsEmit(a.ctx, "tunnel:status", map[string]interface{}{
		"running": true, "paused": true,
	})
}

func (a *App) TunnelResume() {
	a.tunnelSend("RESUME")
	a.tunnelMu.Lock()
	a.tunnelPaused = false
	a.tunnelMu.Unlock()
	runtime.EventsEmit(a.ctx, "tunnel:status", map[string]interface{}{
		"running": true, "paused": false,
	})
}

func (a *App) TunnelStop() {
	a.tunnelSend("STOP")
	a.tunnelMu.Lock()
	proc := a.tunnelProc
	a.tunnelMu.Unlock()

	if proc != nil {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		<-timer.C
		a.tunnelMu.Lock()
		stillRunning := a.tunnelRunning
		a.tunnelMu.Unlock()
		if stillRunning {
			proc.Process.Kill()
		}
	}
	// Останавливаем WireGuard userspace устройство
	StopWGTunnel()
}

func (a *App) TunnelSendCaptcha(token string) {
	a.tunnelSend("CAPTCHA_RESULT|" + token)
}

func (a *App) TunnelStatus() map[string]interface{} {
	a.tunnelMu.Lock()
	defer a.tunnelMu.Unlock()
	return map[string]interface{}{
		"running":       a.tunnelRunning,
		"paused":        a.tunnelPaused,
		"activeWorkers": a.activeWorkers,
		"totalWorkers":  a.totalWorkers,
	}
}

// ── SOCKS5 API ────────────────────────────────────────────────────────────────

// ── Прокси API (SOCKS5 + HTTP) ───────────────────────────────────────────────

type ProxyStartParams struct {
	Host       string `json:"host"`
	Socks5Port string `json:"socks5_port"`
	HTTPPort   string `json:"http_port"`
	UseAuth    bool   `json:"use_auth"`
	User       string `json:"user"`
	Pass       string `json:"pass"`
}

type SocksStatsResult struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

func (a *App) SocksStart(host, port, user, pwd string) string {
	// Обратная совместимость — запускаем только SOCKS5
	// HTTP порт = SOCKS5 порт + 1
	socks5Port := port
	httpPort := "1081"
	if port == "1081" { httpPort = "1082" }
	if err := a.proxy.Start(host, socks5Port, httpPort, user != "", user, pwd); err != nil {
		return err.Error()
	}
	runtime.EventsEmit(a.ctx, "socks:status", true)
	return ""
}

func (a *App) ProxyStart(host, socks5Port, httpPort, user, pwd string, useAuth bool) string {
	if err := a.proxy.Start(host, socks5Port, httpPort, useAuth, user, pwd); err != nil {
		return err.Error()
	}
	runtime.EventsEmit(a.ctx, "socks:status", true)
	return ""
}

func (a *App) SocksStop() {
	a.proxy.Stop()
	runtime.EventsEmit(a.ctx, "socks:status", false)
}

func (a *App) SocksStatus() bool {
	return a.proxy.Running()
}

func (a *App) SocksStats() SocksStatsResult {
	stats := ProxyStats{Active: int(atomic.LoadInt32(&a.proxy.active))}
	return SocksStatsResult{Active: stats.Active}
}


// ── Deploy API ────────────────────────────────────────────────────────────────

type FingerprintResult struct {
	OK          bool   `json:"ok"`
	Fingerprint string `json:"fingerprint"`
	Error       string `json:"error"`
}

// sshExecStream запускает команду и читает вывод построчно в реальном времени
func sshExecStream(client *ssh.Client, cmd string, onLine func(string)) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		var line strings.Builder
		for {
			n, readErr := pr.Read(buf)
			for _, b := range buf[:n] {
				if b == '\n' {
					onLine(line.String())
					line.Reset()
				} else {
					line.WriteByte(b)
				}
			}
			if readErr != nil {
				if line.Len() > 0 {
					onLine(line.String())
				}
				break
			}
		}
	}()

	runErr := sess.Run(cmd)
	pw.Close()
	<-done
	return runErr
}

func (a *App) DeployGetFingerprint(ip, port string) FingerprintResult {
	portN, _ := strconv.Atoi(port)
	if portN == 0 {
		portN = 22
	}

	var hostKey ssh.PublicKey
	config := &ssh.ClientConfig{
		User: "probe",
		Auth: []ssh.AuthMethod{},
		HostKeyCallback: func(h string, remote net.Addr, key ssh.PublicKey) error {
			hostKey = key
			return fmt.Errorf("key captured")
		},
		Timeout: 10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", ip, portN)
	c, _ := ssh.Dial("tcp", addr, config)
	if c != nil {
		c.Close()
	}

	if hostKey == nil {
		return FingerprintResult{Error: "не удалось получить ключ хоста"}
	}

	raw := hostKey.Marshal()
	digest := sha256.Sum256(raw)
	pairs := make([]string, 32)
	for i, b := range digest {
		pairs[i] = fmt.Sprintf("%02X", b)
	}
	fp := strings.Join(pairs, ":")
	// Сохраняем — undeploy и будущие деплои смогут использовать
	a.cfg.Fingerprint = fp
	a.persistConfig()
	return FingerprintResult{OK: true, Fingerprint: fp}
}

func (a *App) DeployRun(ip, port, user, pwd, wgPort, wdttPort, tunnelPwd, adminID, botToken, fingerprint string) {
	portN, _ := strconv.Atoi(port)
	if portN == 0 {
		portN = 22
	}

	fail := func(msg string) {
		a.deployLog("      ✗ "+msg, "error")
		a.deployLog("■  Деплой прерван.", "error")
		a.deployLog("─────────────────────────────────────────────────", "dim")
	}

	a.deployLog("─────────────────────────────────────────────────", "dim")
	a.deployLog(fmt.Sprintf("▶  SSH-деплой  %s:%d", ip, portN), "info")

	// Fail-closed: без отпечатка не подключаемся (защита от MITM).
	if strings.TrimSpace(fingerprint) == "" {
		fail("сначала получите fingerprint сервера (кнопка проверки)")
		return
	}

	// ── Шаг 1: SSH подключение ───────────────────────────────────────────────
	a.deployLog(fmt.Sprintf("[1/4] Подключаюсь %s@%s:%d...", user, ip, portN), "info")
	sshCfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pwd)},
		HostKeyCallback: sshHostKeyCallback(fingerprint),
		Timeout:         15 * time.Second,
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, portN), sshCfg)
	if err != nil {
		fail("SSH: " + err.Error())
		return
	}
	defer client.Close()
	a.deployLog("      ✔ Подключено.", "success")

	// Загрузка через stdin SSH
	uploadData := func(data []byte, remotePath string) bool {
		sess, e := client.NewSession()
		if e != nil {
			fail("SSH сессия: " + e.Error())
			return false
		}
		defer sess.Close()
		sess.Stdin = strings.NewReader(string(data))
		var eb strings.Builder
		sess.Stderr = &eb
		if e := sess.Run("cat > " + remotePath); e != nil {
			fail(fmt.Sprintf("Загрузка %s: %s %s", remotePath, e.Error(), eb.String()))
			return false
		}
		return true
	}

	// ── Шаг 2: Получаем бинарник сервера ─────────────────────────────────────
	a.deployLog("[2/4] Подготовка wdtt-server...", "info")
	var serverData []byte
	if len(a.serverBinary) > 64 {
		// Используем встроенный бинарник (amd64)
		serverData = a.serverBinary
		a.deployLog(fmt.Sprintf("      ✔ Встроенный бинарник (%d KB).", len(serverData)/1024), "success")
	} else {
		fail("Встроенный wdtt-server не найден — пересоберите приложение")
		return
	}

	// ── Шаг 3: Загружаем файлы на VPS ───────────────────────────────────────
	a.deployLog("[3/4] Загружаю файлы на VPS...", "info")

	// Берём встроенный deploy.sh — сеть наружу для этого больше не нужна
	if len(a.deployScript) < 64 {
		fail("Встроенный deploy.sh не найден — пересоберите приложение")
		return
	}
	scriptData := a.deployScript

	if !uploadData(serverData, "/tmp/wdtt-server") {
		return
	}
	a.deployLog("      ✔ wdtt-server → /tmp/wdtt-server", "success")

	if !uploadData(scriptData, "/tmp/deploy.sh") {
		return
	}
	a.deployLog("      ✔ deploy.sh → /tmp/deploy.sh", "success")

	// ── Шаг 4: Запускаем deploy.sh ──────────────────────────────────────────
	a.deployLog("[4/4] Запускаю deploy.sh...", "info")

	// Валидация — пароль туннеля обязателен
	if tunnelPwd == "" {
		a.deployLog("      ✗ Укажите пароль туннеля!", "error")
		a.deployLog("■  Деплой прерван.", "error")
		a.deployLog("─────────────────────────────────────────────────", "dim")
		return
	}

	// Валидация портов и аргументов — защита от инъекции в bash-команду.
	wgP, ok1 := validPort(wgPort)
	wdttP, ok2 := validPort(wdttPort)
	sshP, ok3 := validPort(strconv.Itoa(portN))
	if !ok1 || !ok2 || !ok3 {
		fail("некорректный порт (ожидается 1–65535)")
		return
	}
	if adminID != "" && !reAdminID.MatchString(adminID) {
		fail("admin ID должен быть числом")
		return
	}
	if botToken != "" && !reBotToken.MatchString(botToken) {
		fail("bot token имеет неверный формат")
		return
	}

	// Формируем WDTT_ARGS — только флаги поддерживаемые сервером
	// -dns не поддерживается текущей версией wdtt-server
	wdttArgParts := []string{"-password " + tunnelPwd}
	if adminID != "" {
		wdttArgParts = append(wdttArgParts, "-admin "+adminID)
	}
	if botToken != "" {
		wdttArgParts = append(wdttArgParts, "-bot-token "+botToken)
	}
	wdttArgs := strings.Join(wdttArgParts, " ")

	deployCmd := fmt.Sprintf(
		"chmod +x /tmp/wdtt-server /tmp/deploy.sh && WDTT_ARGS=%s WDTT_DTLS_PORT=%s WDTT_WG_PORT=%s WDTT_SSH_PORT=%s bash /tmp/deploy.sh 2>&1",
		shellQuote(wdttArgs), wdttP, wgP, sshP,
	)

	runErr := sshExecStream(client, deployCmd, func(line string) {
		ll := strings.ToLower(line)
		switch {
		case strings.Contains(line, "✅") || strings.Contains(line, "✓") ||
			strings.Contains(ll, "успешно") || strings.Contains(ll, "active"):
			a.deployLog("  "+line, "success")
		case strings.Contains(line, "✗") || strings.Contains(ll, "error") ||
			strings.Contains(ll, "failed"):
			a.deployLog("  "+line, "error")
		case strings.Contains(line, "⚠") || strings.Contains(ll, "warn"):
			a.deployLog("  "+line, "warn")
		default:
			a.deployLog("  "+line, "dim")
		}
	})

	if runErr != nil {
		a.deployLog("■  Деплой завершён с ошибками.", "error")
	} else {
		a.deployLog(fmt.Sprintf("■  Деплой успешно завершён  →  %s:%s", ip, wdttPort), "success")
	}
	a.deployLog("─────────────────────────────────────────────────", "dim")
}

func (a *App) UndeployRun(ip, port, user, pwd, wgPort, wdttPort, fingerprint string) {
	portN, _ := strconv.Atoi(port)
	if portN == 0 {
		portN = 22
	}

	a.deployLog("─────────────────────────────────────────────────", "dim")
	a.deployLog(fmt.Sprintf("▶  Удаление wdtt-server  %s:%d", ip, portN), "warn")

	if strings.TrimSpace(fingerprint) == "" {
		a.deployLog("      ✗ Нет fingerprint сервера — операция прервана (защита от MITM).", "error")
		return
	}

	sshCfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pwd)},
		HostKeyCallback: sshHostKeyCallback(fingerprint),
		Timeout:         15 * time.Second,
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, portN), sshCfg)
	if err != nil {
		a.deployLog("      ✗ SSH: "+err.Error(), "error")
		return
	}
	defer client.Close()
	a.deployLog("      ✔ Подключено.", "success")

	// Берём встроенный deploy.sh — сеть наружу для этого больше не нужна
	if len(a.deployScript) < 64 {
		a.deployLog("      ✗ Встроенный deploy.sh не найден — пересоберите приложение", "error")
		return
	}
	scriptData := a.deployScript

	sess, _ := client.NewSession()
	sess.Stdin = strings.NewReader(string(scriptData))
	sess.Run("cat > /tmp/deploy.sh")
	sess.Close()

	wgP, ok1 := validPort(wgPort)
	wdttP, ok2 := validPort(wdttPort)
	sshP, ok3 := validPort(strconv.Itoa(portN))
	if !ok1 || !ok2 || !ok3 {
		a.deployLog("      ✗ некорректный порт (ожидается 1–65535)", "error")
		return
	}

	uninstallCmd := fmt.Sprintf(
		"WDTT_DTLS_PORT=%s WDTT_WG_PORT=%s WDTT_SSH_PORT=%s bash /tmp/deploy.sh uninstall 2>&1",
		wdttP, wgP, sshP,
	)
	sshExecStream(client, uninstallCmd, func(line string) {
		a.deployLog("  "+line, "dim")
	})

	a.deployLog("■  Удаление завершено.", "warn")
	a.deployLog("─────────────────────────────────────────────────", "dim")
}

func (a *App) PatchWgConfig(conf, endpoint string) string {
	lines := strings.Split(conf, "\n")
	out := make([]string, 0, len(lines))
	ef, mf := false, false
	for _, line := range lines {
		low := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(low, "endpoint") {
			out = append(out, "Endpoint = "+endpoint)
			ef = true
		} else if strings.HasPrefix(low, "mtu") {
			out = append(out, "MTU = 1280")
			mf = true
		} else {
			out = append(out, line)
		}
	}
	if !ef {
		out = append(out, "Endpoint = "+endpoint)
	}
	result := strings.Join(out, "\n")
	if !mf {
		result = strings.Replace(result, "[Interface]", "[Interface]\nMTU = 1280", 1)
	}
	return result
}

// ── Диалог открытия файла ─────────────────────────────────────────────────────

func (a *App) OpenFileDialog(title string) string {
	path, _ := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
		Filters: []runtime.FileFilter{
			{DisplayName: "WireGuard", Pattern: "*.conf"},
			{DisplayName: "All Files", Pattern: "*.*"},
		},
	})
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func (a *App) SaveWgConfig(content string) bool {
	path, _ := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Сохранить WireGuard конфиг",
		DefaultFilename: "wg-turn.conf",
		Filters: []runtime.FileFilter{
			{DisplayName: "WireGuard", Pattern: "*.conf"},
		},
	})
	if path == "" {
		return false
	}
	return os.WriteFile(path, []byte(content), 0600) == nil
}

func (a *App) SaveFileDialog(content string) bool {
	path, _ := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Сохранить лог",
		DefaultFilename: "wdtt_log.txt",
		Filters: []runtime.FileFilter{
			{DisplayName: "Text", Pattern: "*.txt"},
		},
	})
	if path == "" {
		return false
	}
	return os.WriteFile(path, []byte(content), 0644) == nil
}

// ── Platform-specific ─────────────────────────────────────────────────────────

// sysProcAttr определён в отдельных файлах: syscall_windows.go / syscall_unix.go
// (используем build tags для разделения платформ)
