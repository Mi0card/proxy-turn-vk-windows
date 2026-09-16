//go:build windows

package main

import (
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// sysProxySupported сообщает фронтенду, доступна ли фича на этой платформе.
func sysProxySupported() bool { return true }

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

// sysProxyRestore возвращает ровно прежнее состояние реестра. Ошибки записи
// не проглатываются: возвращается первая из них, но попытки восстановить
// остальные значения продолжаются (частичный откат лучше нулевого).
func sysProxyRestore(s sysProxySnapshot) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// DeleteValue на отсутствующем значении — не ошибка (штатное состояние).
	delValue := func(name string) {
		if err := k.DeleteValue(name); err != nil && err != registry.ErrNotExist {
			record(err)
		}
	}

	if s.HadEnable {
		record(k.SetDWordValue("ProxyEnable", s.ProxyEnable))
	} else {
		record(k.SetDWordValue("ProxyEnable", 0))
	}
	if s.HadServer {
		record(k.SetStringValue("ProxyServer", s.ProxyServer))
	} else {
		delValue("ProxyServer")
	}
	if s.HadOverride {
		record(k.SetStringValue("ProxyOverride", s.ProxyOverride))
	} else {
		delValue("ProxyOverride")
	}
	if s.HadACU {
		record(k.SetStringValue("AutoConfigURL", s.AutoConfigURL))
	} else {
		delValue("AutoConfigURL")
	}
	inetNotify()
	return firstErr
}

// ── Определение процесса-владельца соединения по локальному порту ─────────────

const (
	afINET              = 2
	afINET6             = 23
	tcpTableOwnerPIDAll = 5
	processQueryLimited = 0x1000
	errInsufficientBuf  = 122
)

var (
	iphlpapi                  = syscall.NewLazyDLL("iphlpapi.dll")
	procGetExtendedTcpTable   = iphlpapi.NewProc("GetExtendedTcpTable")
	procOpenProcess           = kernel32.NewProc("OpenProcess")
	procCloseHandle           = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImage = kernel32.NewProc("QueryFullProcessImageNameW")
)

// lookupProcessByPort возвращает basename процесса, владеющего TCP-соединением
// с заданным локальным (клиентским) портом. "" — если определить не удалось.
func lookupProcessByPort(port int) string {
	if port <= 0 || port > 65535 {
		return ""
	}
	// Геометрия строк MIB_TCP*ROW_OWNER_PID: размер, смещение порта, смещение PID.
	pid := tcpOwnerPID(afINET, 24, 8, 20, uint16(port))
	if pid == 0 {
		pid = tcpOwnerPID(afINET6, 56, 20, 52, uint16(port))
	}
	if pid == 0 {
		return ""
	}
	return processNameByPID(pid)
}

// tcpOwnerPID перебирает TCP_TABLE_OWNER_PID_ALL для указанного семейства и
// возвращает PID процесса, чей локальный порт равен port.
func tcpOwnerPID(family uint32, rowSize, portOff, pidOff int, port uint16) uint32 {
	var size uint32
	procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(family), tcpTableOwnerPIDAll, 0)
	if size == 0 {
		return 0
	}
	// Таблица может вырасти между запросом размера и чтением (API тогда
	// перезаписывает size и возвращает ERROR_INSUFFICIENT_BUFFER) — повторяем.
	var buf []byte
	fetched := false
	for attempt := 0; attempt < 2; attempt++ {
		buf = make([]byte, size)
		r, _, _ := procGetExtendedTcpTable.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			0, uintptr(family), tcpTableOwnerPIDAll, 0)
		if r == 0 {
			fetched = true
			break
		}
		if r != errInsufficientBuf {
			return 0
		}
	}
	if !fetched {
		return 0
	}
	n := *(*uint32)(unsafe.Pointer(&buf[0]))
	for i := 0; i < int(n); i++ {
		off := 4 + i*rowSize
		if off+rowSize > len(buf) {
			break
		}
		// dwLocalPort хранится в сетевом порядке в младших 16 битах.
		if uint16(buf[off+portOff])<<8|uint16(buf[off+portOff+1]) != port {
			continue
		}
		return *(*uint32)(unsafe.Pointer(&buf[off+pidOff]))
	}
	return 0
}

// processNameByPID открывает процесс и возвращает basename его образа.
func processNameByPID(pid uint32) string {
	h, _, _ := procOpenProcess.Call(processQueryLimited, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImage.Call(
		h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 || size == 0 {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:size]))
}
