package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ── System Proxy (WinINET) ───────────────────────────────────────────────────

const sysProxyOverride = "<local>;127.0.0.1;localhost;*.local" // bypass

// sysProxySnapshot хранит прежнее состояние прокси-настроек реестра.
// Определён здесь (платформонезависимо), чтобы JSON backup/restore
// компилировались везде. Платформенные функции в syscall_*.go заполняют/
// используют поля только на Windows.
type sysProxySnapshot struct {
	ProxyEnable   uint32 `json:"proxy_enable"`
	ProxyServer   string `json:"proxy_server"`
	ProxyOverride string `json:"proxy_override"`
	AutoConfigURL string `json:"auto_config_url"`
	HadEnable     bool   `json:"had_enable"`
	HadServer     bool   `json:"had_server"`
	HadOverride   bool   `json:"had_override"`
	HadACU        bool   `json:"had_acu"`
}

// ── Backup / restore на диск (crash-safe) ────────────────────────────────────

func (a *App) sysProxyBackupPath() string {
	return filepath.Join(a.baseDir, "system_proxy_backup.json")
}

func (a *App) saveSysProxyBackup(s sysProxySnapshot) error {
	data, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(a.sysProxyBackupPath(), data, 0600)
}

func (a *App) loadSysProxyBackup() (sysProxySnapshot, bool) {
	data, err := os.ReadFile(a.sysProxyBackupPath())
	if err != nil {
		return sysProxySnapshot{}, false
	}
	var s sysProxySnapshot
	if json.Unmarshal(data, &s) != nil {
		return sysProxySnapshot{}, false
	}
	return s, true
}

func (a *App) clearSysProxyBackup() {
	os.Remove(a.sysProxyBackupPath())
}

// ── Публичное API (вызывается из фронтенда через Wails) ──────────────────────

// SystemProxyStatus возвращает текущее состояние перенаправления.
func (a *App) SystemProxyStatus() bool {
	return a.sysProxyOn.Load()
}

// SystemProxySupported сообщает фронтенду, доступна ли фича системного прокси
// на текущей ОС (только Windows — WinINET/реестр).
func (a *App) SystemProxySupported() bool {
	return sysProxySupported()
}

// SystemProxyEnable включает системный HTTP-прокси через WinINET.
// Поднимает отдельный HTTP-прокси без аутентификации на случайном порту
// и направляет WinINET на него.
// Возвращает пустую строку при успехе, иначе — текст ошибки для UI.
func (a *App) SystemProxyEnable() string {
	if a.sysProxyOn.Load() {
		return "" // уже включён
	}

	// Поднимаем отдельный листенер без auth на рандомном порту.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "не удалось открыть порт: " + err.Error()
	}
	port := ln.Addr().(*net.TCPAddr).Port
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", port)

	// Запускаем HTTP-прокси (без auth) на этом листенере.
	handler := newSysProxyHandler()
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	a.sysProxyLn = ln
	a.sysProxySrv = srv

	// Снимаем текущее состояние WinINET — для восстановления при отключении/крэше.
	cur, err := sysProxyRead()
	if err != nil {
		srv.Close()
		a.sysProxyLn, a.sysProxySrv = nil, nil
		return "чтение настроек прокси: " + err.Error()
	}
	if err := a.saveSysProxyBackup(cur); err != nil {
		srv.Close()
		a.sysProxyLn, a.sysProxySrv = nil, nil
		return "сохранение бэкапа: " + err.Error()
	}

	if err := sysProxyApplyStatic(proxyAddr, sysProxyOverride); err != nil {
		srv.Close()
		a.sysProxyLn, a.sysProxySrv = nil, nil
		a.clearSysProxyBackup()
		return "применение: " + err.Error()
	}

	a.sysProxyOn.Store(true)

	a.socksLog(fmt.Sprintf("🌐 Системный прокси включён → %s (порт %d, без auth)", proxyAddr, port), "success")

	if !WGTunnelActive() {
		a.socksLog("⚠ Туннель не активен — браузеры не смогут загружать страницы, пока туннель не будет запущен.", "warn")
	}

	runtime.EventsEmit(a.ctx, "sysproxy:status", true)
	return ""
}

// SystemProxyDisable снимает системный прокси и восстанавливает прежние настройки.
// Безопасно вызывать, даже если прокси не был включён (no-op).
func (a *App) SystemProxyDisable() {
	// Закрываем отдельный листенер системного прокси.
	if a.sysProxySrv != nil {
		a.sysProxySrv.Close() // прерывает все in-flight соединения
		a.sysProxySrv = nil
	}
	if a.sysProxyLn != nil {
		a.sysProxyLn.Close()
		a.sysProxyLn = nil
	}

	if s, ok := a.loadSysProxyBackup(); ok {
		sysProxyRestore(s)
		a.clearSysProxyBackup()
	}
	wasOn := a.sysProxyOn.Swap(false)

	if wasOn {
		a.socksLog("Системный прокси выключен, настройки восстановлены.", "warn")
	}
	runtime.EventsEmit(a.ctx, "sysproxy:status", false)
}
