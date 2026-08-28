//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// sysProxySupported сообщает фронтенду, доступна ли фича на этой платформе.
func sysProxySupported() bool { return true }

// ── System proxy через networksetup (macOS) ──────────────────────────────────
//
// Включаем системный прокси (HTTP/HTTPS) настройкой сетевых сервисов через
// встроенную утилиту /usr/sbin/networksetup, вызываемую как внешний процесс —
// без Objective-C/SystemConfiguration.framework и без прав администратора
// (как это делает upstream qv2ray/proxy-turn-vk). Каждый вызов запускается
// отдельным процессом с обнулённым stdout/stderr, т.к. networksetup пишет в
// stderr при бэкапе настроек в plist.
//
// Как и у upstream, force-quit при включённом прокси НЕ сбрасывает настройки —
// восстановление происходит только повторным включением/выключением через
// приложение (SystemProxyDisable). Крэш-бэкап на диск сохраняется, но на macOS
// автоматический рестор на старте не выполняется.

const networksetupBin = "/usr/sbin/networksetup"

// darwinServiceSnapshot хранит состояние прокси одного сетевого сервиса.
// Поле Darwin в sysProxySnapshot (system_proxy.go) хранит их все как JSON.
type darwinServiceSnapshot struct {
	Service    string   `json:"service"`
	WebProxyOn bool     `json:"web_proxy_on"`
	WebHost    string   `json:"web_host"`
	WebPort    string   `json:"web_port"`
	SecureOn   bool     `json:"secure_on"`
	SecureHost string   `json:"secure_host"`
	SecurePort string   `json:"secure_port"`
	Bypass     []string `json:"bypass"`
	HadWeb     bool     `json:"had_web"`
	HadSecure  bool     `json:"had_secure"`
	HadBypass  bool     `json:"had_bypass"`
}

// netServices возвращает список активных сетевых сервисов.
// Сервисы, помеченные '*' (отключённые), пропускаются.
func netServices() ([]string, error) {
	cmd := exec.Command(networksetupBin, "-listallnetworkservices")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("listallnetworkservices: %w", err)
	}
	var services []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "An asterisk (*) denotes that a network service is disabled." {
			continue
		}
		if strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

// netProxyState снимает текущее состояние web/secure прокси сервиса.
func netProxyState(service string) darwinServiceSnapshot {
	s := darwinServiceSnapshot{Service: service}
	if h, p, on, ok := getWebProxy(service); ok {
		s.HadWeb = true
		s.WebProxyOn = on
		s.WebHost = h
		s.WebPort = p
	}
	if h, p, on, ok := getSecureWebProxy(service); ok {
		s.HadSecure = true
		s.SecureOn = on
		s.SecureHost = h
		s.SecurePort = p
	}
	if b, ok := getBypassDomains(service); ok {
		s.HadBypass = true
		s.Bypass = b
	}
	return s
}

func getWebProxy(service string) (host, port string, on bool, ok bool) {
	cmd := exec.Command(networksetupBin, "-getwebproxy", service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", false, false
	}
	return parseProxyOut(string(out))
}

func getSecureWebProxy(service string) (host, port string, on bool, ok bool) {
	cmd := exec.Command(networksetupBin, "-getsecurewebproxy", service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", false, false
	}
	return parseProxyOut(string(out))
}

func parseProxyOut(out string) (host, port string, on bool, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "Enabled":
			on = strings.EqualFold(val, "Yes")
			ok = true
		case "Server":
			host = val
		case "Port":
			port = val
		}
	}
	return host, port, on, ok
}

func getBypassDomains(service string) ([]string, bool) {
	cmd := exec.Command(networksetupBin, "-getproxybypassdomains", service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, false
	}
	var domains []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			domains = append(domains, line)
		}
	}
	return domains, true
}

// runNS выполняет networksetup с заданными аргументами, подавляя stdout/stderr.
func runNS(args ...string) error {
	cmd := exec.Command(networksetupBin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("networksetup %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// sysProxyRead снимает текущее состояние системного прокси по всем сервисам.
func sysProxyRead() (sysProxySnapshot, error) {
	services, err := netServices()
	if err != nil {
		return sysProxySnapshot{}, err
	}
	states := make([]darwinServiceSnapshot, 0, len(services))
	for _, svc := range services {
		states = append(states, netProxyState(svc))
	}
	data, err := json.Marshal(states)
	if err != nil {
		return sysProxySnapshot{}, err
	}
	return sysProxySnapshot{Darwin: data}, nil
}

// overrideToBypass превращает Windows-формат override (';'-разделённый, с <local>)
// в список доменов для networksetup (запятая-разделённый, без <local>).
func overrideToBypass(override string) []string {
	var out []string
	for _, part := range strings.Split(override, ";") {
		part = strings.TrimSpace(part)
		if part == "" || part == "<local>" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// sysProxyApplyStatic включает web/secure прокси на всех активных сервисах.
func sysProxyApplyStatic(server, override string) error {
	host, port, err := net.SplitHostPort(server)
	if err != nil {
		return fmt.Errorf("неверный адрес прокси %q: %w", server, err)
	}
	bypass := overrideToBypass(override)

	services, err := netServices()
	if err != nil {
		return err
	}
	for _, svc := range services {
		if err := runNS("-setwebproxystate", svc, "on"); err != nil {
			return err
		}
		if err := runNS("-setwebproxy", svc, host, port); err != nil {
			return err
		}
		if err := runNS("-setsecurewebproxystate", svc, "on"); err != nil {
			return err
		}
		if err := runNS("-setsecurewebproxy", svc, host, port); err != nil {
			return err
		}
		if len(bypass) > 0 {
			args := append([]string{"-setproxybypassdomains", svc}, bypass...)
			if err := runNS(args...); err != nil {
				return err
			}
		}
	}
	return nil
}

// sysProxyRestore возвращает сервисы к прежнему состоянию прокси.
func sysProxyRestore(s sysProxySnapshot) error {
	var states []darwinServiceSnapshot
	if len(s.Darwin) > 0 {
		if err := json.Unmarshal(s.Darwin, &states); err != nil {
			return err
		}
	}
	// Состояние, которое снято только что (не из бэкапа), — сервисы без записи
	// просто выключаем.
	for _, st := range states {
		if st.HadWeb {
			if st.WebProxyOn {
				if err := runNS("-setwebproxystate", st.Service, "on"); err != nil {
					return err
				}
				if err := runNS("-setwebproxy", st.Service, st.WebHost, st.WebPort); err != nil {
					return err
				}
			} else {
				if err := runNS("-setwebproxystate", st.Service, "off"); err != nil {
					return err
				}
			}
		} else {
			if err := runNS("-setwebproxystate", st.Service, "off"); err != nil {
				return err
			}
		}

		if st.HadSecure {
			if st.SecureOn {
				if err := runNS("-setsecurewebproxystate", st.Service, "on"); err != nil {
					return err
				}
				if err := runNS("-setsecurewebproxy", st.Service, st.SecureHost, st.SecurePort); err != nil {
					return err
				}
			} else {
				if err := runNS("-setsecurewebproxystate", st.Service, "off"); err != nil {
					return err
				}
			}
		} else {
			if err := runNS("-setsecurewebproxystate", st.Service, "off"); err != nil {
				return err
			}
		}

		if st.HadBypass && len(st.Bypass) > 0 {
			args := append([]string{"-setproxybypassdomains", st.Service}, st.Bypass...)
			if err := runNS(args...); err != nil {
				return err
			}
		}
	}
	return nil
}

func inetNotify() {}
