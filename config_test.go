package main

import (
	"path/filepath"
	"testing"
)

// Импорт легаси-конфига (поля vk/srv/sec без profiles) обязан сразу создать
// профиль — иначе список профилей появился бы только после перезапуска.
func TestApplyImportedConfigMigratesLegacy(t *testing.T) {
	a := &App{configFile: filepath.Join(t.TempDir(), "config.json")}
	a.cfg = Config{DeviceID: "dev-123"}

	a.cfgMu.Lock()
	a.applyImportedConfigLocked(Config{VK: "hash", Srv: "1.2.3.4:56000", Sec: "secret"})
	a.cfgMu.Unlock()

	if a.cfg.DeviceID != "dev-123" {
		t.Errorf("DeviceID = %q, ожидался сохранённый dev-123", a.cfg.DeviceID)
	}
	if len(a.cfg.Profiles) != 1 || a.cfg.Profiles[0].Name != "Профиль 1" {
		t.Fatalf("профиль не создан: %+v", a.cfg.Profiles)
	}
	if a.cfg.ActiveProfile != "Профиль 1" {
		t.Errorf("ActiveProfile = %q, ожидался Профиль 1", a.cfg.ActiveProfile)
	}
	if a.cfg.VK != "" || a.cfg.Srv != "" || a.cfg.Sec != "" {
		t.Errorf("легаси-поля не очищены: vk=%q srv=%q sec=%q", a.cfg.VK, a.cfg.Srv, a.cfg.Sec)
	}
}

// Импорт профильного конфига не трогает профили и активный профиль.
func TestApplyImportedConfigKeepsProfiles(t *testing.T) {
	a := &App{configFile: filepath.Join(t.TempDir(), "config.json")}
	a.cfg = Config{DeviceID: "dev-123"}

	a.cfgMu.Lock()
	a.applyImportedConfigLocked(Config{
		Profiles:      []ConnProfile{{Name: "X", VK: "hash"}},
		ActiveProfile: "X",
	})
	a.cfgMu.Unlock()

	if len(a.cfg.Profiles) != 1 || a.cfg.Profiles[0].Name != "X" {
		t.Fatalf("профили потеряны: %+v", a.cfg.Profiles)
	}
	if a.cfg.ActiveProfile != "X" {
		t.Errorf("ActiveProfile = %q, ожидался X", a.cfg.ActiveProfile)
	}
}
