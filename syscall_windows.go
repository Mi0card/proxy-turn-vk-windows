//go:build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows/registry"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// ── WinINET system proxy ─────────────────────────────────────────────────────

const inetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

var (
	wininet                = syscall.NewLazyDLL("wininet.dll")
	procInternetSetOptionW = wininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

// inetNotify применяет изменения реестра ко всем WinINET-приложениям.
func inetNotify() {
	procInternetSetOptionW.Call(0, internetOptionSettingsChanged, 0, 0)
	procInternetSetOptionW.Call(0, internetOptionRefresh, 0, 0)
}

// sysProxyRead снимает текущее состояние прокси из реестра.
func sysProxyRead() (sysProxySnapshot, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsPath, registry.QUERY_VALUE)
	if err != nil {
		return sysProxySnapshot{}, err
	}
	defer k.Close()

	var s sysProxySnapshot
	if v, _, err := k.GetIntegerValue("ProxyEnable"); err == nil {
		s.ProxyEnable = uint32(v)
		s.HadEnable = true
	}
	if v, _, err := k.GetStringValue("ProxyServer"); err == nil {
		s.ProxyServer = v
		s.HadServer = true
	}
	if v, _, err := k.GetStringValue("ProxyOverride"); err == nil {
		s.ProxyOverride = v
		s.HadOverride = true
	}
	if v, _, err := k.GetStringValue("AutoConfigURL"); err == nil {
		s.AutoConfigURL = v
		s.HadACU = true
	}
	return s, nil
}

// sysProxyApplyStatic устанавливает статический HTTP-прокси (фаза 1).
func sysProxyApplyStatic(server, override string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	// Убираем PAC, если был, чтобы не конфликтовал со статическим прокси.
	k.DeleteValue("AutoConfigURL") // ошибка «нет значения» допустима

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyServer", server); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", override); err != nil {
		return err
	}
	inetNotify()
	return nil
}

// sysProxyRestore возвращает ровно прежнее состояние реестра.
func sysProxyRestore(s sysProxySnapshot) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if s.HadEnable {
		k.SetDWordValue("ProxyEnable", s.ProxyEnable)
	} else {
		k.SetDWordValue("ProxyEnable", 0)
	}
	if s.HadServer {
		k.SetStringValue("ProxyServer", s.ProxyServer)
	} else {
		k.DeleteValue("ProxyServer")
	}
	if s.HadOverride {
		k.SetStringValue("ProxyOverride", s.ProxyOverride)
	} else {
		k.DeleteValue("ProxyOverride")
	}
	if s.HadACU {
		k.SetStringValue("AutoConfigURL", s.AutoConfigURL)
	} else {
		k.DeleteValue("AutoConfigURL")
	}
	inetNotify()
	return nil
}
