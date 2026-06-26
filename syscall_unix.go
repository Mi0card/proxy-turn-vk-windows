//go:build !windows

package main

import (
	"errors"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// ── Заглушки системного прокси (только Windows) ──────────────────────────────

var errSysProxyUnsupported = errors.New("system proxy поддерживается только на Windows")

func inetNotify()                                       {}
func sysProxyRead() (sysProxySnapshot, error)           { return sysProxySnapshot{}, errSysProxyUnsupported }
func sysProxyApplyStatic(server, override string) error { return errSysProxyUnsupported }
func sysProxyRestore(s sysProxySnapshot) error          { return errSysProxyUnsupported }
