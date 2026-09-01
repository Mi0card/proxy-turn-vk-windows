package main

import (
	"bufio"
	"bytes"
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

const AppVersion = "0.2.11.2"

// ── Config ────────────────────────────────────────────────────────────────────

// ConnProfile хранит набор параметров подключения, который можно сохранить,
// переключить, переименовать и удалить через вкладку «Подключение».
type ConnProfile struct {
	Name        string `json:"name"`
	VK          string `json:"vk"`
	Srv         string `json:"srv"`
	Sec         string `json:"sec"`
	N           string `json:"n"`
	Listen      string `json:"listen"`
	CaptchaMode string `json:"captcha_mode"`
	ObfsMode    string `json:"obfs_mode"`
	Fingerprint string `json:"fingerprint"`
}

type Config struct {
	VK             string          `json:"vk,omitempty"`
	Srv            string          `json:"srv,omitempty"`
	Sec            string          `json:"sec,omitempty"`
	N              string          `json:"n,omitempty"`
	Listen         string          `json:"listen,omitempty"`
	CaptchaMode    string          `json:"captcha_mode,omitempty"`
	ObfsMode       string          `json:"obfs_mode,omitempty"`
	Fingerprint    string          `json:"fingerprint,omitempty"`
	DeviceID       string          `json:"device_id"`
	Profiles       []ConnProfile   `json:"profiles"`
	ActiveProfile  string          `json:"active_profile"`
	PxHost         string          `json:"px_host"`
	PxSocksPort    string          `json:"px_socks_port"`
	PxHttpPort     string          `json:"px_http_port"`
	PxUseAuth      bool            `json:"px_use_auth"`
	PxUser         string          `json:"px_user"`
	PxPass         string          `json:"px_pass"`
	Theme          string          `json:"theme"`
	Rulesets       []RulesetConfig `json:"rulesets"`
	RulesViaTunnel bool            `json:"rules_via_tunnel"`
	RoutingDefault string          `json:"routing_default"`
	DNS            string          `json:"dns"`
	TrayOnExit     bool            `json:"tray_on_exit"`
	TrayOnMinimize bool            `json:"tray_on_minimize"`
	// Автовосстановление соединения: перезапуск туннеля после выхода из сна
	// и при смене сети. По умолчанию включено (см. loadConfig).
	AutoRestoreOnWake      bool `json:"auto_restore_on_wake"`
	AutoRestoreOnNetChange bool `json:"auto_restore_on_net_change"`
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
	wgConfPath    string // путь к wg-turn.conf рядом с wdtt-client.exe
	pingStop      chan struct{}

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
	activeWorkers    int

	// Автовосстановление: wake-монитор + single-flight перезапуска
	wakeStop   chan struct{}
	restartMu  sync.Mutex
	restarting bool

	// Монитор смены сети
	netStop          chan struct{}
	lastNetRestartAt time.Time // под tunnelMu: кулдаун между сетевыми перезапусками

	// Закрытие
	closeAllowed atomic.Bool

	// Видимость окна (для трея): true = окно показано.
	windowVisible atomic.Bool

	// Поллинг сворачивания окна → скрытие в трей (TrayOnMinimize).
	minPollStop chan struct{}

	// Капча: защита от открытия нескольких окон при повторных CAPTCHA_SOLVE
	captchaOpen atomic.Bool

	// Системный прокси (WinINET)
	sysProxyMu  sync.Mutex // защищает sysProxyLn/sysProxySrv от гонки Enable/Disable
	sysProxyOn  atomic.Bool
	sysProxyLn  net.Listener // отдельный HTTP-прокси без auth
	sysProxySrv *http.Server // для graceful close

	// Прокси (SOCKS5 + HTTP)
	proxy      *ProxyServer
	socksStats struct {
		mu     sync.Mutex
		active int
		total  int
	}

	// Маршрутизация по правилам (ruleset)
	ruleset *RulesetManager

	// Конфиг
	cfgMu sync.RWMutex // защищает cfg от конкурентных чтений/записей (Wails вызывает бинды конкурентно)
	cfg   Config
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
// RulesetManager инициализируется один раз в startup (с корректным baseDir).
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
	a.ruleset = NewRulesetManager(a.baseDir)

	if a.overrideClientExe != "" {
		a.clientExe = a.overrideClientExe
	} else {
		a.clientExe = filepath.Join(a.baseDir, "wdtt-client.exe")
	}

	// Загружаем конфиг
	a.loadConfig()

	// Настраиваем маршрутизацию: менеджер правил и текущий список правил прокси.
	a.ruleset = NewRulesetManager(a.baseDir)
	a.ruleset.SetLogFn(a.routingLog)
	a.ruleset.SetProgressFn(a.onRulesetProgress)
	a.ruleset.SetViaTunnel(a.getCfg().RulesViaTunnel)
	if a.proxy != nil {
		a.proxy.SetRulesetManager(a.ruleset)
		a.applyRouting()
	}
	// Асинхронно подгружаем правила (из кеша или сети). Если настроены только
	// встроенные правила (domain:/keyword:/...) или правил нет — скачивание не нужно,
	// но при пустом списке всё равно прогреваем кеш для автоподсказок.
	if len(a.getCfg().Rulesets) == 0 || needsDownloadedRulesets(a.getCfg().Rulesets) {
		go func() {
			if err := a.ruleset.EnsureLoaded(); err != nil {
				a.routingLog("Маршрутизация: правила не загружены: "+err.Error(), "warn")
			}
		}()
	}

	// Крэш-восстановление: если остался бэкап системного прокси, значит прошлый
	// сеанс завершился аварийно с включённым перенаправлением — возвращаем настройки.
	if s, ok := a.loadSysProxyBackup(); ok {
		sysProxyRestore(s)
		a.clearSysProxyBackup()
		a.log("Обнаружен и восстановлен системный прокси от прошлого сеанса.", "warn")
	}

	// Трей: иконка создаётся на всех поддерживаемых платформах (Windows, macOS).
	a.windowVisible.Store(true)
	trayInit(a)
	trayUpdateStatus(a)

	// Следим за сворачиванием окна — при включённой опции прячем в трей.
	a.minPollStop = make(chan struct{})
	go a.watchMinimize()

	// Следим за выходом из сна — при пробуждении перезапускаем туннель.
	a.wakeStop = make(chan struct{})
	go a.startWakeMonitor(a.wakeStop, a.onWake)

	// Следим за сменой сети — при изменении интерфейсов перезапускаем туннель.
	a.netStop = make(chan struct{})
	go a.startNetMonitor(a.netStop)
}

// beforeClose вызывается Wails перед закрытием окна.
func (a *App) beforeClose(ctx context.Context) bool {
	if a.closeAllowed.Load() {
		return false
	}

	// Если в настройках включено сворачивание в трей — прячем окно без диалога.
	if a.getCfg().TrayOnExit && trayAvailable() {
		a.hideWindowToTray()
		return true
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
		Buttons:       []string{"В трей", "Закрыть", "Отмена"},
		DefaultButton: "Закрыть",
		CancelButton:  "Отмена",
	})

	switch btn {
	case "В трей", "Tray":
		// Сворачиваем в трей, сервисы продолжают работать.
		a.hideWindowToTray()
		return true
	case "Отмена", "Cancel", "No", "":
		return true
	default:
		// «Закрыть» — останавливаем сервисы и выходим.
		a.quitApp()
		return true
	}
}

// hideWindowToTray прячет главное окно (сервисы продолжают работать).
func (a *App) hideWindowToTray() {
	a.windowVisible.Store(false)
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
	}
	trayUpdateStatus(a)
}

// showWindowFromTray разворачивает и показывает главное окно.
func (a *App) showWindowFromTray() {
	a.windowVisible.Store(true)
	trayActivateApp()
	if a.ctx != nil {
		runtime.WindowUnminimise(a.ctx)
		runtime.WindowShow(a.ctx)
	}
	trayUpdateStatus(a)
}

// watchMinimize следит за состоянием окна и прячет его в трей при
// сворачивании, если включена опция TrayOnMinimize. Wails не эмитит событий
// о сворачивании окна, поэтому используем поллинг runtime.WindowIsMinimised
// (на Windows — IsIconic, на macOS — isMiniaturized).
func (a *App) watchMinimize() {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	wasMin := false
	for {
		select {
		case <-ticker.C:
			min := runtime.WindowIsMinimised(a.ctx)
			if min && !wasMin && a.windowVisible.Load() && a.getCfg().TrayOnMinimize && trayAvailable() {
				a.hideWindowToTray()
			}
			wasMin = min
		case <-a.minPollStop:
			return
		}
	}
}

// quitApp останавливает сервисы и завершает приложение. Вызывается из трея
// и из диалога закрытия. Guard через closeAllowed — второй вызов игнорируется.
func (a *App) quitApp() {
	if !a.closeAllowed.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer func() { recover() }()
		// Системный прокси снимаем первым — восстановить настройки до выхода.
		if a.sysProxyOn.Load() {
			a.SystemProxyDisable()
		}
		a.tunnelMu.Lock()
		tunnelActive := a.tunnelRunning
		a.tunnelMu.Unlock()
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
		if a.proxy.Running() {
			a.log("Останавливаю локальный прокси...", "warn")
			a.SocksStop()
			a.log("Локальный прокси остановлен.", "warn")
		}
		if a.ctx != nil {
			runtime.Quit(a.ctx)
		}
	}()
}

// trayStatusText формирует строку статуса для меню трея.
func (a *App) trayStatusText() string {
	a.tunnelMu.Lock()
	running := a.tunnelRunning
	paused := a.tunnelPaused
	workers := a.activeWorkers
	a.tunnelMu.Unlock()
	switch {
	case paused:
		return "Туннель: пауза"
	case running:
		return fmt.Sprintf("Туннель: активен (%d воркеров)", workers)
	default:
		return "Туннель: выключен"
	}
}

func (a *App) shutdown(ctx context.Context) {
	if a.minPollStop != nil {
		close(a.minPollStop)
		a.minPollStop = nil
	}
	if a.wakeStop != nil {
		close(a.wakeStop)
		a.wakeStop = nil
	}
	if a.netStop != nil {
		close(a.netStop)
		a.netStop = nil
	}
	trayRemove(a)
	a.SystemProxyDisable() // снять перенаправление и восстановить настройки
	a.TunnelStop()
	StopWGTunnel()
	a.SocksStop()
}

// ── Config API ────────────────────────────────────────────────────────────────

func (a *App) loadConfig() {
	// Дефолты автовосстановления — включено: старые конфиги (без этих ключей),
	// свежий конфиг без полей и отсутствующий файл не должны молча менять
	// поведение существующих пользователей. Присутствующие в файле ключи
	// перезапишут эти значения ниже.
	a.cfg.AutoRestoreOnWake = true
	a.cfg.AutoRestoreOnNetChange = true

	data, err := os.ReadFile(a.configFile)
	if err != nil {
		return
	}
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	json.Unmarshal(data, &a.cfg)
	a.migrateLegacyProfileLocked()
}

// migrateLegacyProfileLocked: если профили подключения ещё не созданы, а в конфиге
// есть ранее сохранённые параметры подключения (без имени профиля) — сохраняем их
// как «Профиль 1» и делаем активным. Вызывается при загрузке, cfgMu уже взят.
func (a *App) migrateLegacyProfileLocked() {
	if len(a.cfg.Profiles) > 0 {
		return
	}
	if strings.TrimSpace(a.cfg.VK) == "" && strings.TrimSpace(a.cfg.Srv) == "" && strings.TrimSpace(a.cfg.Sec) == "" {
		return
	}
	p := ConnProfile{
		Name:        "Профиль 1",
		VK:          a.cfg.VK,
		Srv:         a.cfg.Srv,
		Sec:         a.cfg.Sec,
		N:           a.cfg.N,
		Listen:      a.cfg.Listen,
		CaptchaMode: a.cfg.CaptchaMode,
		ObfsMode:    a.cfg.ObfsMode,
		Fingerprint: a.cfg.Fingerprint,
	}
	a.cfg.Profiles = []ConnProfile{p}
	a.cfg.ActiveProfile = p.Name
	a.clearLegacyConnFieldsLocked()
	a.persistConfig()
}

// clearLegacyConnFieldsLocked убирает из конфига старые «безымянные» записи
// параметров подключения — после миграции они живут только в профилях.
// Вызывается при взятой cfgMu.
func (a *App) clearLegacyConnFieldsLocked() {
	a.cfg.VK = ""
	a.cfg.Srv = ""
	a.cfg.Sec = ""
	a.cfg.N = ""
	a.cfg.Listen = ""
	a.cfg.CaptchaMode = ""
	a.cfg.ObfsMode = ""
	a.cfg.Fingerprint = ""
}

// getCfg возвращает копию конфига под RLock.
func (a *App) getCfg() Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg
}

func (a *App) GetConfig() Config {
	return a.getCfg()
}

func (a *App) SaveConfig(cfg Config) {
	a.cfgMu.Lock()
	// Фронтенд не отправляет rulesets/device_id через SaveConfig —
	// сохраняем текущие значения, иначе они сбросятся на каждом сохранении.
	if cfg.Rulesets == nil {
		cfg.Rulesets = a.cfg.Rulesets
	}
	if cfg.DeviceID == "" {
		cfg.DeviceID = a.cfg.DeviceID
	}
	if cfg.Profiles == nil {
		cfg.Profiles = a.cfg.Profiles
	}
	if cfg.ActiveProfile == "" {
		cfg.ActiveProfile = a.cfg.ActiveProfile
	}
	// Параметры подключения живут только в профилях — не позволяем фронтенду
	// вернуть в конфиг старые «безымянные» записи.
	cfg.VK = ""
	cfg.Srv = ""
	cfg.Sec = ""
	cfg.N = ""
	cfg.Listen = ""
	cfg.CaptchaMode = ""
	cfg.ObfsMode = ""
	cfg.Fingerprint = ""
	a.cfg = cfg
	a.cfgMu.Unlock()
	a.persistConfig()
}

func (a *App) GetVersion() string {
	return AppVersion
}

// ── Профили подключения ────────────────────────────────────────────────────────

// GetProfiles возвращает список сохранённых профилей подключения.
func (a *App) GetProfiles() []ConnProfile {
	return a.getCfg().Profiles
}

// GetActiveProfile возвращает имя активного профиля подключения ("" если нет).
func (a *App) GetActiveProfile() string {
	return a.getCfg().ActiveProfile
}

// SaveProfile сохраняет (создаёт или перезаписывает по имени) профиль подключения.
func (a *App) SaveProfile(p ConnProfile) {
	a.cfgMu.Lock()
	name := strings.TrimSpace(p.Name)
	if name == "" {
		a.cfgMu.Unlock()
		return
	}
	p.Name = name
	idx := -1
	for i, pr := range a.cfg.Profiles {
		if pr.Name == name {
			idx = i
			break
		}
	}
	if idx >= 0 {
		a.cfg.Profiles[idx] = p
	} else {
		a.cfg.Profiles = append(a.cfg.Profiles, p)
	}
	a.cfgMu.Unlock()
	a.persistConfig()
}

// DeleteProfile удаляет профиль подключения по имени. Если удаляется активный
// профиль — активный сбрасывается.
func (a *App) DeleteProfile(name string) {
	a.cfgMu.Lock()
	out := a.cfg.Profiles[:0]
	for _, pr := range a.cfg.Profiles {
		if pr.Name != name {
			out = append(out, pr)
		}
	}
	a.cfg.Profiles = out
	if a.cfg.ActiveProfile == name {
		a.cfg.ActiveProfile = ""
	}
	a.cfgMu.Unlock()
	a.persistConfig()
}

// RenameProfile переименовывает профиль подключения.
func (a *App) RenameProfile(oldName, newName string) {
	a.cfgMu.Lock()
	newName = strings.TrimSpace(newName)
	if newName == "" {
		a.cfgMu.Unlock()
		return
	}
	for i := range a.cfg.Profiles {
		if a.cfg.Profiles[i].Name == oldName {
			a.cfg.Profiles[i].Name = newName
			if a.cfg.ActiveProfile == oldName {
				a.cfg.ActiveProfile = newName
			}
			break
		}
	}
	a.cfgMu.Unlock()
	a.persistConfig()
}

// SetActiveProfile устанавливает активный профиль подключения.
func (a *App) SetActiveProfile(name string) {
	a.cfgMu.Lock()
	a.cfg.ActiveProfile = name
	a.cfgMu.Unlock()
	a.persistConfig()
}

// GetTheme возвращает сохранённую тему, либо "" если пользователь ещё
// не выбирал — фронтенд в этом случае определяет тему по prefers-color-scheme ОС.
func (a *App) GetTheme() string {
	return a.getCfg().Theme
}

func (a *App) SetTheme(theme string) {
	a.cfgMu.Lock()
	a.cfg.Theme = theme
	a.cfgMu.Unlock()
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
// Ошибка записи логируется — раньше она молча проглатывалась, из-за чего
// секреты могли не сохраниться (например, exe в защищённой папке).
func (a *App) persistConfig() error {
	data, err := json.MarshalIndent(a.cfg, "", "  ")
	if err != nil {
		a.log("⚠ Не удалось сериализовать конфиг: "+err.Error(), "warn")
		return err
	}
	if err := os.WriteFile(a.configFile, data, 0600); err != nil {
		a.log("⚠ Не удалось сохранить конфиг: "+err.Error(), "warn")
		return err
	}
	return nil
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

// socksLog разделяет события прокси на два потока:
//   - строки соединений (начинаются с "→ ") → событие socks:log (вкладка «Подключения»);
//   - события жизни прокси (старт/стоп/системный прокси) → событие log (вкладка «Общий»).
func (a *App) socksLog(msg, lv string) {
	ts := time.Now().Format("15:04:05")
	if strings.HasPrefix(msg, "→ ") {
		runtime.EventsEmit(a.ctx, "socks:log", LogEntry{Ts: ts, Msg: msg, Lv: lv})
		return
	}
	runtime.EventsEmit(a.ctx, "log", LogEntry{Ts: ts, Msg: msg, Lv: lv})
}

func (a *App) onProxyStats(stats ProxyStats) {
	runtime.EventsEmit(a.ctx, "socks:stats", stats)
}

func (a *App) deployLog(msg, lv string) {
	ts := time.Now().Format("15:04:05")
	runtime.EventsEmit(a.ctx, "deploy:log", LogEntry{Ts: ts, Msg: msg, Lv: lv})
}

// routingLog пишет события маршрутизации в общий лог (вкладка «Общий»).
func (a *App) routingLog(msg, lv string) {
	ts := time.Now().Format("15:04:05")
	runtime.EventsEmit(a.ctx, "log", LogEntry{Ts: ts, Msg: msg, Lv: lv})
}

// onRulesetProgress транслирует прогресс скачивания правил в UI.
// pct == -1 означает неопределённый прогресс (объём неизвестен).
func (a *App) onRulesetProgress(stage string, pct int) {
	runtime.EventsEmit(a.ctx, "ruleset:progress", map[string]interface{}{
		"stage": stage,
		"pct":   pct,
	})
}

// ── Tunnel API ────────────────────────────────────────────────────────────────

func (a *App) log(msg, lv string) {
	if a.ctx == nil {
		return
	}
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
		// Ретрим один раз — crypto/rand практически всегда доступен.
		if _, err2 := io.ReadFull(cryptoReader(), b); err2 != nil {
			// Крайний fallback: детерминированный хеш времени+PID процесса.
			h := sha256.Sum256([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())))
			copy(b, h[:16])
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
		a.cfgMu.Lock()
		a.cfg.DeviceID = deviceID
		a.cfgMu.Unlock()
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
		a.finalizeTunnel(cmd, startTs)
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

	// WireGuard конфиг печатается блоком (рамка ╔…╚). Буферизуем его целиком
	// и отдаём одной записью, чтобы сообщения из параллельного потока (stderr)
	// не вклинивались внутрь конфига в общем логе.
	var wgBoxBuf []string
	inWgBox := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		ll := strings.ToLower(line)

		// Блок WireGuard конфига — копим строки рамки, сбрасываем по нижней.
		// Рамка ╔…║…╚ украшательская; убираем её: логируем чистые строки конфига.
		if inWgBox || strings.Contains(line, "WireGuard Конфиг") {
			clean := strings.Trim(line, " ║╔╚═")
			if clean != "" {
				wgBoxBuf = append(wgBoxBuf, clean)
			}
			if strings.Contains(line, "WireGuard Конфиг") {
				inWgBox = true
			}
			if strings.HasPrefix(line, "╚") {
				inWgBox = false
				if len(wgBoxBuf) > 0 {
					a.log("   "+strings.Join(wgBoxBuf, "\n"), "info")
				}
				wgBoxBuf = nil
			} else if len(wgBoxBuf) > 64 {
				// Страховка: блок оборвался — не глотаем остальной лог.
				inWgBox = false
				a.log("   "+strings.Join(wgBoxBuf, "\n"), "info")
				wgBoxBuf = nil
			}
			continue
		}

		// Статистика — парсим для статус-бара, но в лог не выводим
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
				trayUpdateStatus(a)
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
					if diff < 0 {
						diff = 0
					}
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
			continue
		}

		// Капча — WebView-протокол: go_client печатает CAPTCHA_SOLVE|mode|redirectURI|sessionToken
		// и ждёт ответ CAPTCHA_RESULT|<результат> через stdin (см. go_client/vk_auth.go).
		// На Windows решаем автоматически через нативный WebView2-попап с перехватом
		// сети (см. captcha_webview_windows.go) — так же, как это делает апстрим
		// Android-приложение через свой нативный WebView.
		if strings.HasPrefix(line, "CAPTCHA_SOLVE|") {
			parts := strings.SplitN(strings.TrimPrefix(line, "CAPTCHA_SOLVE|"), "|", 3)
			if len(parts) < 2 || parts[1] == "" {
				a.log("⚠  Капча: некорректный формат запроса", "warn")
				continue
			}
			redirectURI := parts[1]
			a.log("⚠  Требуется капча — открываю окно подтверждения VK...", "warn")
			// Не открываем второе окно, пока предыдущее не закрыто.
			if !a.captchaOpen.CompareAndSwap(false, true) {
				a.log("⚠  Капча уже решается в открытом окне — повторный запрос проигнорирован", "warn")
				continue
			}
			runtime.EventsEmit(a.ctx, "tunnel:captcha", "webview")
			baseDir := a.baseDir
			go openCaptchaWebView(redirectURI, baseDir, func(result string) {
				a.captchaOpen.Store(false)
				a.TunnelSendCaptcha(result)
				runtime.EventsEmit(a.ctx, "tunnel:captcha:done", nil)
			})
			continue
		}

		// Капча — прочие информационные строки (прогресс автоматического решения и т.п.)
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
				trayUpdateStatus(a)
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
					if err := StartWGTunnel(conf, ParseDNSOverride(a.getCfg().DNS)); err != nil {
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
// (guard через a.tunnelRunning и идентичность proc). Освобождает ресурсы и
// снимает системный прокси. Сверка proc с a.tunnelProc защищает от гонки с
// restartTunnel: если пока читались завершающиеся пайпы старого процесса
// watchdog уже запустил новый, эта финализация не должна затирать его состояние.
func (a *App) finalizeTunnel(proc *exec.Cmd, startTs time.Time) {
	a.tunnelMu.Lock()
	if !a.tunnelRunning || a.tunnelProc != proc { // уже финализировано или это уже другой (перезапущенный) процесс
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
	totalWorkers := a.totalWorkers
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
	runtime.EventsEmit(a.ctx, "tunnel:workers", WorkerStats{Active: 0, Total: totalWorkers})
	trayUpdateStatus(a)
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
	if a.lastCBReset == 0 {
		a.lastCBReset = now
	}
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
			go a.ensureRestart("⚠ Flood Control — VK ограничил IP. Перезапуск...")
		}
	case strings.Contains(ll, "ip mismatch"):
		a.mismatchCount++
		if a.mismatchCount >= 5 {
			go a.ensureRestart("⚠ IP Mismatch — IP изменился. Перезапуск...")
		}
	case strings.Contains(ll, "connection refused") || strings.Contains(ll, "i/o timeout"):
		a.refusedCount++
		if a.refusedCount >= 400 {
			go a.ensureRestart("⚠ 400+ таймаутов — нет сети. Перезапуск...")
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
				sameProc := a.tunnelProc == proc
				a.tunnelMu.Unlock()
				// sameProc: если за время бэкоффа уже случился перезапуск (wake/breaker) —
				// не дублируем его, процесс под нашим наблюдением уже заменился.
				if stillRunning && sameProc {
					a.ensureRestart("⚠ Процесс упал. Перезапуск...")
				}
				return
			}

			// Зомби: процесс жив но 0 воркеров > 90 секунд
			if workers <= 0 {
				if zeroWorkersSince == 0 {
					zeroWorkersSince = time.Now().UnixMilli()
				} else if time.Now().UnixMilli()-zeroWorkersSince > 90_000 {
					a.log("⚠ Зомби-процесс (0 воркеров 90с). Перезапуск...", "warn")
					a.tunnelMu.Lock()
					sameProc := a.tunnelProc == proc
					a.tunnelMu.Unlock()
					if sameProc {
						a.ensureRestart("Зомби-процесс (0 воркеров 90с)")
					}
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
	ps := a.pingStop
	a.pingStop = nil
	ws := a.watchdogStop
	a.watchdogStop = nil
	a.tunnelMu.Unlock()
	if ps != nil {
		close(ps) // останавливаем pingLoop старого процесса, иначе он утечёт
	}
	if ws != nil {
		close(ws) // останавливаем старый watchdog — перезапуск может прийти не из него
	}

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
	trayUpdateStatus(a)
}

func (a *App) TunnelResume() {
	a.tunnelSend("RESUME")
	a.tunnelMu.Lock()
	a.tunnelPaused = false
	a.tunnelMu.Unlock()
	runtime.EventsEmit(a.ctx, "tunnel:status", map[string]interface{}{
		"running": true, "paused": false,
	})
	trayUpdateStatus(a)
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

func (a *App) ProxyStart(host, socks5Port, httpPort, user, pwd string, useAuth bool) string {
	// Применяем текущие правила маршрутизации к прокси.
	a.applyRouting()
	if err := a.proxy.Start(host, socks5Port, httpPort, useAuth, user, pwd); err != nil {
		return err.Error()
	}
	runtime.EventsEmit(a.ctx, "socks:status", true)
	trayUpdateStatus(a)
	return ""
}

func (a *App) SocksStop() {
	a.proxy.Stop()
	runtime.EventsEmit(a.ctx, "socks:status", false)
	trayUpdateStatus(a)
}

func (a *App) SocksStatus() bool {
	return a.proxy.Running()
}

func (a *App) SocksStats() SocksStatsResult {
	stats := ProxyStats{Active: int(atomic.LoadInt32(&a.proxy.active))}
	return SocksStatsResult{Active: stats.Active}
}

// ── Ruleset API (маршрутизация) ─────────────────────────────────────────────

type RulesetStatus struct {
	Loaded     bool   `json:"loaded"`
	LastUpdate int64  `json:"last_update"` // unix ms, 0 если никогда
	Error      string `json:"error"`
}

// GetRulesets возвращает текущий список правил маршрутизации.
func (a *App) GetRulesets() []RulesetConfig {
	cfg := a.getCfg()
	if cfg.Rulesets == nil {
		return []RulesetConfig{}
	}
	return cfg.Rulesets
}

// applyRouting применяет правила маршрутизации и политику по умолчанию к прокси.
func (a *App) applyRouting() {
	if a.proxy == nil {
		return
	}
	cfg := a.getCfg()
	a.proxy.SetRulesets(cfg.Rulesets)
	a.proxy.SetDefaultPolicy(cfg.RoutingDefault)
}

// SetRulesets сохраняет список правил (с валидацией) и политику по умолчанию
// в конфиг. Возвращает "" при успехе, иначе текст ошибки.
func (a *App) SetRulesets(configs []RulesetConfig, defaultPolicy string) string {
	if !validPolicy(defaultPolicy) {
		defaultPolicy = PolicyProxy
	}
	filtered := make([]RulesetConfig, 0, len(configs))
	for _, rc := range configs {
		if rc.Rule == "" {
			continue
		}
		// Нормализуем правило: добавляем префикс ruleset: если его нет
		rc.Rule = normalizeRule(rc.Rule)
		if !validateRule(rc.Rule) {
			return "Некорректное правило: " + rc.Rule + " (ожидается ruleset:geosite-..., ruleset:geoip-..., domain:<домен>, domain-suffix:<домен>, keyword:<текст>, regex:<шаблон>, cidr:<подсеть> или ip:<адрес>)"
		}
		if !validPolicy(rc.Policy) {
			return "Некорректная политика: " + rc.Policy + " (допустимо: block, direct, proxy)"
		}
		if rc.Policy == "" {
			rc.Policy = PolicyProxy
		}
		filtered = append(filtered, rc)
	}
	a.cfgMu.Lock()
	a.cfg.Rulesets = filtered
	a.cfg.RoutingDefault = defaultPolicy
	a.cfgMu.Unlock()
	a.persistConfig()
	if a.proxy != nil {
		a.proxy.SetDefaultPolicy(defaultPolicy)
	}
	a.routingLog(fmt.Sprintf("Маршрутизация: сохранено %d правил.", len(filtered)), "info")
	return ""
}

// RulesetGroups возвращает список доступных групп правил для автоподсказок.
type RulesetGroups struct {
	Geosite []string `json:"geosite"`
	Geoip   []string `json:"geoip"`
}

// GetRulesetStatus возвращает статус загруженных правил.
func (a *App) GetRulesetStatus() RulesetStatus {
	if a.ruleset == nil {
		return RulesetStatus{}
	}
	last := a.ruleset.LastUpdate()
	return RulesetStatus{
		Loaded:     a.ruleset.Loaded(),
		LastUpdate: last.UnixMilli(),
	}
}

// GetRulesetGroups возвращает список доступных групп правил (геосайтов и геоIP).
// Возвращает пустые списки если правила ещё не загружены.
func (a *App) GetRulesetGroups() RulesetGroups {
	if a.ruleset == nil {
		return RulesetGroups{}
	}
	geosite, geoip := a.ruleset.ListGroups()
	return RulesetGroups{
		Geosite: geosite,
		Geoip:   geoip,
	}
}

// UpdateRulesets скачивает и перечитывает geosite.dat / geoip.dat.
// Возвращает "" при успехе, иначе текст ошибки.
func (a *App) UpdateRulesets() string {
	if a.ruleset == nil {
		return "роутинг не инициализирован"
	}
	cfg := a.getCfg()
	// Опция «через туннель» — применяем текущее значение и проверяем туннель.
	a.ruleset.SetViaTunnel(cfg.RulesViaTunnel)
	if cfg.RulesViaTunnel && !WGTunnelActive() {
		return "Опция «через туннель» включена, но туннель не активен. Подключите туннель или отключите опцию."
	}
	if err := a.ruleset.UpdateRulesets(); err != nil {
		a.routingLog("Маршрутизация: ошибка обновления правил: "+err.Error(), "error")
		return err.Error()
	}
	// Применяем правила к работающему прокси.
	if a.proxy != nil {
		a.applyRouting()
		a.routingLog(fmt.Sprintf("Маршрутизация: применено %d правил к прокси.", len(a.getCfg().Rulesets)), "info")
	}
	a.routingLog("Маршрутизация: правила обновлены.", "success")
	return ""
}

// EnsureRulesetsLoaded гарантирует, что правила загружены (из кеша или сети).
// Возвращает "" при успехе или когда роутинг не настроен.
// Если настроены только встроенные (inline) правила — domain:/keyword:/regex:/
// cidr:/ip: — скачивание дата-файлов не требуется и не выполняется.
func (a *App) EnsureRulesetsLoaded() string {
	if a.ruleset == nil {
		return ""
	}
	configs := a.getCfg().Rulesets
	if len(configs) == 0 {
		return ""
	}
	if needsDownloadedRulesets(configs) {
		if err := a.ruleset.EnsureLoaded(); err != nil {
			return err.Error()
		}
	}
	if a.proxy != nil {
		a.applyRouting()
	}
	if n := len(configs); n > 0 {
		a.routingLog(fmt.Sprintf("Маршрутизация: правила загружены, применено %d правил к прокси.", n), "info")
	}
	return ""
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
	a.cfgMu.Lock()
	a.cfg.Fingerprint = fp
	a.cfgMu.Unlock()
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
	scriptData := bytes.ReplaceAll(a.deployScript, []byte("\r\n"), []byte("\n")) // страховка от CRLF на случай кривого checkout

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
	scriptData := bytes.ReplaceAll(a.deployScript, []byte("\r\n"), []byte("\n")) // страховка от CRLF на случай кривого checkout

	sess, err := client.NewSession()
	if err != nil {
		a.deployLog("      ✗ SSH-сессия: "+err.Error(), "error")
		return
	}
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

// ── Импорт / экспорт конфигурации ─────────────────────────────────────────────

// ExportConfig возвращает текущую конфигурацию в виде JSON-строки
// (pretty-printed, как в windtt_config.json).
func (a *App) ExportConfig() string {
	data, err := json.MarshalIndent(a.getCfg(), "", "  ")
	if err != nil {
		a.log("⚠ Не удалось сериализовать конфиг для экспорта: "+err.Error(), "warn")
		return ""
	}
	return string(data)
}

// SaveConfigDialog сохраняет JSON-конфигурацию через системный диалог
// (права 0600 — файл содержит секреты).
func (a *App) SaveConfigDialog(content string) bool {
	path, _ := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Экспорт конфигурации",
		DefaultFilename: "windtt_config.json",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON", Pattern: "*.json"},
		},
	})
	if path == "" {
		return false
	}
	return os.WriteFile(path, []byte(content), 0600) == nil
}

// ImportResult — результат импорта конфигурации для фронтенда.
type ImportResult struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
	Config Config `json:"config"`
}

// ImportConfig открывает диалог выбора JSON-файла, парсит его и заменяет
// текущую конфигурацию. Текущий DeviceID сохраняется — он присваивается
// сервером и вряд ли подходит из импортированного файла. После импорта
// правила маршрутизации применяются к работающему прокси.
func (a *App) ImportConfig() ImportResult {
	path, _ := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Импорт конфигурации",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON", Pattern: "*.json"},
			{DisplayName: "All Files", Pattern: "*.*"},
		},
	})
	if path == "" {
		return ImportResult{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ImportResult{OK: false, Error: "Не удалось прочитать файл: "+err.Error()}
	}
	var imported Config
	if err := json.Unmarshal(data, &imported); err != nil {
		return ImportResult{OK: false, Error: "Некорректный JSON конфигурации: "+err.Error()}
	}
	a.cfgMu.Lock()
	imported.DeviceID = a.cfg.DeviceID
	a.cfg = imported
	a.cfgMu.Unlock()
	if err := a.persistConfig(); err != nil {
		return ImportResult{OK: false, Error: err.Error()}
	}
	a.applyRouting()
	a.routingLog("Конфигурация импортирована.", "info")
	return ImportResult{OK: true, Config: a.getCfg()}
}

// ── Platform-specific ─────────────────────────────────────────────────────────

// sysProcAttr определён в отдельных файлах: syscall_windows.go / syscall_unix.go
// (используем build tags для разделения платформ)
