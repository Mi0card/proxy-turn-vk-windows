package main

import (
	"net"
	"testing"
	"time"
)

// ── тестовые помощники ─────────────────────────────────────────────────────────

type fakeAddr string

func (a fakeAddr) Network() string { return "ip" }
func (a fakeAddr) String() string  { return string(a) }

type fakeIfc struct {
	name   string
	flags  net.Flags
	addrs  []net.Addr
	errAdd bool
}

func ifc(name string, up, loop bool, addrs ...string) fakeIfc {
	f := net.FlagUp
	if !up {
		f = 0
	}
	if loop {
		f |= net.FlagLoopback
	}
	a := make([]net.Addr, 0, len(addrs))
	for _, s := range addrs {
		a = append(a, fakeAddr(s))
	}
	return fakeIfc{name: name, flags: f | net.FlagRunning, addrs: a}
}

func sigFor(ifs []fakeIfc) string {
	oldIfcs, oldAddrs := netInterfacesFn, netAddrsFn
	defer func() { netInterfacesFn, netAddrsFn = oldIfcs, oldAddrs }()

	netInterfacesFn = func() ([]net.Interface, error) {
		out := make([]net.Interface, 0, len(ifs))
		for _, f := range ifs {
			out = append(out, net.Interface{Name: f.name, Flags: f.flags})
		}
		return out, nil
	}
	netAddrsFn = func(i net.Interface) ([]net.Addr, error) {
		for _, f := range ifs {
			if f.name == i.Name {
				if f.errAdd {
					return nil, net.ErrClosed
				}
				return f.addrs, nil
			}
		}
		return nil, nil
	}
	s, err := netSignature()
	if err != nil {
		panic(err)
	}
	return s
}

// ── netSignature ───────────────────────────────────────────────────────────────

func TestNetSignatureStable(t *testing.T) {
	a := []fakeIfc{ifc("eth0", true, false, "192.168.1.5"), ifc("wlan0", false, false, "10.0.0.7")}
	b := []fakeIfc{ifc("eth0", true, false, "192.168.1.5"), ifc("wlan0", false, false, "10.0.0.7")}
	if sigFor(a) != sigFor(b) {
		t.Fatal("identical interfaces must produce identical signature")
	}
}

func TestNetSignatureIgnoresOrder(t *testing.T) {
	a := []fakeIfc{ifc("eth0", true, false, "192.168.1.5"), ifc("wlan0", false, false, "10.0.0.7")}
	b := []fakeIfc{ifc("wlan0", false, false, "10.0.0.7"), ifc("eth0", true, false, "192.168.1.5")}
	if sigFor(a) != sigFor(b) {
		t.Fatal("interface order must not change signature")
	}
}

func TestNetSignatureIgnoresLoopback(t *testing.T) {
	a := []fakeIfc{ifc("eth0", true, false, "192.168.1.5")}
	b := []fakeIfc{ifc("eth0", true, false, "192.168.1.5"), ifc("lo", true, true, "127.0.0.1")}
	if sigFor(a) != sigFor(b) {
		t.Fatal("loopback change must not affect signature")
	}
}

func TestNetSignatureDetectsUpDown(t *testing.T) {
	up := []fakeIfc{ifc("eth0", true, false, "192.168.1.5")}
	down := []fakeIfc{ifc("eth0", false, false, "192.168.1.5")}
	if sigFor(up) == sigFor(down) {
		t.Fatal("up→down must change signature")
	}
}

func TestNetSignatureDetectsIPChange(t *testing.T) {
	a := []fakeIfc{ifc("eth0", true, false, "192.168.1.5")}
	b := []fakeIfc{ifc("eth0", true, false, "192.168.1.9")}
	if sigFor(a) == sigFor(b) {
		t.Fatal("IP add/remove must change signature")
	}
}

func TestNetSignatureSkipsBadAddrIfc(t *testing.T) {
	good := []fakeIfc{ifc("eth0", true, false, "192.168.1.5")}
	withBad := []fakeIfc{ifc("eth0", true, false, "192.168.1.5"), {name: "broken", flags: net.FlagUp, errAdd: true}}
	if sigFor(good) != sigFor(withBad) {
		t.Fatal("interface with Addrs() error must be skipped, rest still hashed")
	}
}

// ── networkChangeAllowed ───────────────────────────────────────────────────────

func TestNetworkChangeAllowed(t *testing.T) {
	stopped := &App{}
	if stopped.networkChangeAllowed() {
		t.Fatal("stopped tunnel must not allow net restart")
	}

	paused := &App{tunnelRunning: true, tunnelPaused: true}
	if paused.networkChangeAllowed() {
		t.Fatal("paused tunnel must not allow net restart")
	}

	running := &App{tunnelRunning: true}
	if !running.networkChangeAllowed() {
		t.Fatal("running tunnel outside cooldown must allow net restart")
	}

	// Кулдаун: 29s назад — заблокировано; ровно 30s — разрешено (>=).
	recent := &App{tunnelRunning: true, lastNetRestartAt: time.Now().Add(-(netCooldown - time.Second))}
	if recent.networkChangeAllowed() {
		t.Fatal("within cooldown must block net restart")
	}
	atBoundary := &App{tunnelRunning: true, lastNetRestartAt: time.Now().Add(-netCooldown)}
	if !atBoundary.networkChangeAllowed() {
		t.Fatal("at cooldown boundary must allow net restart")
	}
}

func TestOnNetworkChangeCooldownStamp(t *testing.T) {
	a := &App{tunnelRunning: true, cfg: Config{AutoRestoreOnNetChange: true}}
	a.onNetworkChange()
	a.tunnelMu.Lock()
	stamp := a.lastNetRestartAt
	a.tunnelMu.Unlock()
	if stamp.IsZero() {
		t.Fatal("allowed net change must stamp lastNetRestartAt")
	}
	if !a.networkChangeAllowed() {
		// после срабатывания последующий вызов сразу попал в кулдаун
	} else {
		t.Fatal("immediately after trigger must be in cooldown")
	}
}

func TestOnNetworkChangeDisabled(t *testing.T) {
	// Автовосстановление выключено — смена сети не должна трогать туннель
	// и не должна штамповать кулдаун.
	a := &App{tunnelRunning: true, cfg: Config{AutoRestoreOnNetChange: false}}
	a.onNetworkChange()
	a.tunnelMu.Lock()
	stamp := a.lastNetRestartAt
	a.tunnelMu.Unlock()
	if !stamp.IsZero() {
		t.Fatal("disabled net restore must not stamp cooldown")
	}
}
