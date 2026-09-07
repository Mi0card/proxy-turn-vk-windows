package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	return &App{baseDir: dir}
}

// ── sysProxyBackupPath ──────────────────────────────────────────────────────

func TestSysProxyBackupPath(t *testing.T) {
	a := newTestApp(t)
	got := a.sysProxyBackupPath()
	want := filepath.Join(a.baseDir, "system_proxy_backup.json")
	if got != want {
		t.Fatalf("sysProxyBackupPath() = %q, want %q", got, want)
	}
}

// ── save/load round-trip ────────────────────────────────────────────────────

func TestSysProxyBackupRoundTrip(t *testing.T) {
	a := newTestApp(t)

	snap := sysProxySnapshot{
		ProxyEnable:   1,
		ProxyServer:   "127.0.0.1:8080",
		ProxyOverride: "<local>;127.0.0.1",
		AutoConfigURL: "",
		HadEnable:     true,
		HadServer:     true,
		HadOverride:   true,
		HadACU:        false,
	}

	if err := a.saveSysProxyBackup(snap); err != nil {
		t.Fatalf("saveSysProxyBackup: %v", err)
	}

	got, ok := a.loadSysProxyBackup()
	if !ok {
		t.Fatal("loadSysProxyBackup returned false, want true")
	}
	if got.ProxyEnable != snap.ProxyEnable {
		t.Errorf("ProxyEnable = %d, want %d", got.ProxyEnable, snap.ProxyEnable)
	}
	if got.ProxyServer != snap.ProxyServer {
		t.Errorf("ProxyServer = %q, want %q", got.ProxyServer, snap.ProxyServer)
	}
	if got.ProxyOverride != snap.ProxyOverride {
		t.Errorf("ProxyOverride = %q, want %q", got.ProxyOverride, snap.ProxyOverride)
	}
	if got.HadEnable != snap.HadEnable {
		t.Errorf("HadEnable = %v, want %v", got.HadEnable, snap.HadEnable)
	}
	if got.HadServer != snap.HadServer {
		t.Errorf("HadServer = %v, want %v", got.HadServer, snap.HadServer)
	}
	if got.HadOverride != snap.HadOverride {
		t.Errorf("HadOverride = %v, want %v", got.HadOverride, snap.HadOverride)
	}
	if got.HadACU != snap.HadACU {
		t.Errorf("HadACU = %v, want %v", got.HadACU, snap.HadACU)
	}
}

// ── load no file ────────────────────────────────────────────────────────────

func TestSysProxyBackupLoadNoFile(t *testing.T) {
	a := newTestApp(t)
	_, ok := a.loadSysProxyBackup()
	if ok {
		t.Fatal("loadSysProxyBackup on missing file returned true, want false")
	}
}

// ── load corrupt file ───────────────────────────────────────────────────────

func TestSysProxyBackupLoadCorrupt(t *testing.T) {
	a := newTestApp(t)
	path := a.sysProxyBackupPath()
	if err := os.WriteFile(path, []byte("{not valid json"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, ok := a.loadSysProxyBackup()
	if ok {
		t.Fatal("loadSysProxyBackup on corrupt file returned true, want false")
	}
}

// ── clear ───────────────────────────────────────────────────────────────────

func TestSysProxyBackupClear(t *testing.T) {
	a := newTestApp(t)

	snap := sysProxySnapshot{ProxyEnable: 1, HadEnable: true}
	if err := a.saveSysProxyBackup(snap); err != nil {
		t.Fatalf("saveSysProxyBackup: %v", err)
	}
	if _, err := os.Stat(a.sysProxyBackupPath()); err != nil {
		t.Fatalf("backup file should exist after save: %v", err)
	}

	a.clearSysProxyBackup()
	if _, err := os.Stat(a.sysProxyBackupPath()); !os.IsNotExist(err) {
		t.Fatalf("backup file should not exist after clear, got err: %v", err)
	}
}

// ── save overwrite ──────────────────────────────────────────────────────────

func TestSysProxyBackupOverwrite(t *testing.T) {
	a := newTestApp(t)

	s1 := sysProxySnapshot{ProxyEnable: 1, ProxyServer: "first:80", HadEnable: true}
	if err := a.saveSysProxyBackup(s1); err != nil {
		t.Fatalf("save 1: %v", err)
	}

	s2 := sysProxySnapshot{ProxyEnable: 0, ProxyServer: "second:80", HadEnable: false}
	if err := a.saveSysProxyBackup(s2); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	got, ok := a.loadSysProxyBackup()
	if !ok {
		t.Fatal("loadSysProxyBackup returned false")
	}
	if got.ProxyServer != "second:80" {
		t.Errorf("ProxyServer = %q, want %q (overwrite failed)", got.ProxyServer, "second:80")
	}
	if got.ProxyEnable != 0 {
		t.Errorf("ProxyEnable = %d, want 0", got.ProxyEnable)
	}
}

// ── file is valid JSON ─────────────────────────────────────────────────────

func TestSysProxyBackupFileIsValidJSON(t *testing.T) {
	a := newTestApp(t)

	snap := sysProxySnapshot{
		ProxyEnable:   1,
		ProxyServer:   "127.0.0.1:9090",
		ProxyOverride: "<local>",
		HadEnable:     true,
		HadServer:     true,
	}
	if err := a.saveSysProxyBackup(snap); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(a.sysProxyBackupPath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var parsed sysProxySnapshot
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("backup file is not valid JSON: %v", err)
	}
}

// ── SystemProxyStatus ───────────────────────────────────────────────────────

func TestSystemProxyStatus(t *testing.T) {
	a := newTestApp(t)

	if a.SystemProxyStatus() {
		t.Fatal("SystemProxyStatus should be false initially")
	}
	a.sysProxyOn.Store(true)
	if !a.SystemProxyStatus() {
		t.Fatal("SystemProxyStatus should be true after Store(true)")
	}
	a.sysProxyOn.Store(false)
	if a.SystemProxyStatus() {
		t.Fatal("SystemProxyStatus should be false after Store(false)")
	}
}

// ── SystemProxySupported ────────────────────────────────────────────────────

func TestSystemProxySupported(t *testing.T) {
	a := newTestApp(t)
	// On Windows this returns true; on other platforms (test stub) it returns false.
	// We just verify the call doesn't panic and returns a bool.
	_ = a.SystemProxySupported()
}

// ── save with all Had* false ────────────────────────────────────────────────

func TestSysProxyBackupAllHadFalse(t *testing.T) {
	a := newTestApp(t)

	snap := sysProxySnapshot{
		ProxyEnable: 0,
		ProxyServer: "",
		ProxyOverride: "",
		AutoConfigURL: "",
		HadEnable:   false,
		HadServer:   false,
		HadOverride: false,
		HadACU:      false,
	}
	if err := a.saveSysProxyBackup(snap); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, ok := a.loadSysProxyBackup()
	if !ok {
		t.Fatal("load returned false")
	}
	if got.HadEnable || got.HadServer || got.HadOverride || got.HadACU {
		t.Errorf("expected all Had* false, got HadEnable=%v HadServer=%v HadOverride=%v HadACU=%v",
			got.HadEnable, got.HadServer, got.HadOverride, got.HadACU)
	}
}
