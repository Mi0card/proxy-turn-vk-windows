//go:build !windows && !darwin

package main

import (
	"errors"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// ── Заглушки системного прокси (Windows и macOS реализованы отдельно) ────────

var errSysProxyUnsupported = errors.New("system proxy поддерживается только на Windows и macOS")

// sysProxySupported сообщает фронтенду, доступна ли фича на этой платформе.
func sysProxySupported() bool { return false }

func inetNotify()                                       {}
func sysProxyRead() (sysProxySnapshot, error)           { return sysProxySnapshot{}, errSysProxyUnsupported }
func sysProxyApplyStatic(server, override string) error { return errSysProxyUnsupported }
func sysProxyRestore(s sysProxySnapshot) error          { return errSysProxyUnsupported }
