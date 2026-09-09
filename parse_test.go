package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// ── ParseWdtt ─────────────────────────────────────────────────────────────────

func TestParseWdtt(t *testing.T) {
	a := &App{}
	r := a.ParseWdtt("wdtt://1.2.3.4:56000:56001:9000:secret:hash1,hash2")
	if !r.OK {
		t.Fatal("валидная ссылка должна разобраться")
	}
	if r.Server != "1.2.3.4:56000" {
		t.Errorf("Server = %q, ожидалось %q", r.Server, "1.2.3.4:56000")
	}
	if r.Hash != "hash1,hash2" {
		t.Errorf("Hash = %q, ожидалось %q", r.Hash, "hash1,hash2")
	}
	if r.Secret != "secret" {
		t.Errorf("Secret = %q, ожидалось %q", r.Secret, "secret")
	}
}

func TestParseWdttInvalid(t *testing.T) {
	a := &App{}
	cases := []string{
		"http://1.2.3.4:56000:56001:9000:secret:hash", // не wdtt://
		"wdtt://1.2.3.4:56000:56001:9000:secret",      // меньше 6 частей
		"wdtt://1.2.3.4:0:56001:9000:secret:hash",     // порт 0
		"wdtt://1.2.3.4:70000:56001:9000:secret:hash", // порт > 65535
		"wdtt://1.2.3.4:abc:56001:9000:secret:hash",   // не число
		"", // пусто
	}
	for _, in := range cases {
		if r := a.ParseWdtt(in); r.OK {
			t.Errorf("ParseWdtt(%q) должно быть невалидным", in)
		}
	}
}

// ── ParseQwdtt (qwdtt://config, как в Android-апстриме) ────────────────────────

func TestParseQwdtt(t *testing.T) {
	a := &App{}
	r := a.ParseQwdtt("qwdtt://config?name=My+Server&peer=1.2.3.4&hashes=hash1%2Chash2&workers=5&port=9001&pass=secret")
	if !r.OK {
		t.Fatal("валидная qwdtt-ссылка должна разобраться")
	}
	if r.Name != "My Server" {
		t.Errorf("Name = %q, ожидалось %q", r.Name, "My Server")
	}
	if r.Peer != "1.2.3.4:56000" {
		t.Errorf("Peer = %q, ожидалось %q (добавляется порт DTLS)", r.Peer, "1.2.3.4:56000")
	}
	if r.Hashes != "hash1,hash2" {
		t.Errorf("Hashes = %q, ожидалось %q (URL-decoded)", r.Hashes, "hash1,hash2")
	}
	if r.Secret != "secret" {
		t.Errorf("Secret = %q, ожидалось %q", r.Secret, "secret")
	}
	if r.Workers != "5" {
		t.Errorf("Workers = %q, ожидалось 5", r.Workers)
	}
	if r.Listen != "127.0.0.1:9001" {
		t.Errorf("Listen = %q, ожидалось %q", r.Listen, "127.0.0.1:9001")
	}
}

func TestParseQwdttDefaultsAndVariants(t *testing.T) {
	a := &App{}
	cases := []struct {
		in         string
		name, peer string
		workers    string
		listen     string
	}{
		{"qwdtt://config?peer=h", "QR Профиль", "h:56000", "9", "127.0.0.1:9000"},
		{"qwdtt://config?peer=8.8.8.8:7000", "QR Профиль", "8.8.8.8:7000", "9", "127.0.0.1:9000"},
		{"qwdtt://config?peer=h&port=0", "", "", "", ""},
		{"qwdtt://config?peer=h&workers=0", "", "", "", ""},
		{"qwdtt://config?peer=h&workers=abc", "", "", "", ""},
		{"qwdtt://config?peer=h&pass=one&password=two", "QR Профиль", "h:56000", "9", "127.0.0.1:9000"},
		{"qwdtt:config?peer=x:1234&pass=p", "QR Профиль", "x:1234", "9", "127.0.0.1:9000"},
	}
	for _, c := range cases {
		r := a.ParseQwdtt(c.in)
		if c.name == "" {
			if r.OK {
				t.Errorf("ParseQwdtt(%q) должно быть невалидным", c.in)
			}
			continue
		}
		if !r.OK {
			t.Errorf("ParseQwdtt(%q) должно быть валидным", c.in)
			continue
		}
		if r.Name != c.name || r.Peer != c.peer || r.Workers != c.workers || r.Listen != c.listen {
			t.Errorf("ParseQwdtt(%q) = {Name:%q Peer:%q Workers:%q Listen:%q}, ожидалось {Name:%q Peer:%q Workers:%q Listen:%q}",
				c.in, r.Name, r.Peer, r.Workers, r.Listen, c.name, c.peer, c.workers, c.listen)
		}
	}
}

func TestParseQwdttInvalid(t *testing.T) {
	a := &App{}
	cases := []string{
		"",
		"http://x/?peer=1.2.3.4",
		"qwdtt://config?hashes=h",      // нет peer
		"wdtt://1.2.3.4:56000:1:1:s:h", // legacy — не qwdtt
		"qwdtt://other?peer=1.2.3.4",   // не config
	}
	for _, in := range cases {
		if r := a.ParseQwdtt(in); r.OK {
			t.Errorf("ParseQwdtt(%q) должно быть невалидным", in)
		}
	}
}

// ── validPort ─────────────────────────────────────────────────────────────────

func TestValidPort(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"22", "22", true},
		{" 22 ", "22", true},
		{"56000", "56000", true},
		{"65535", "65535", true},
		{"0", "", false},
		{"65536", "", false},
		{"abc", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := validPort(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("validPort(%q) = (%q, %v), ожидалось (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// ── parseWGConf ───────────────────────────────────────────────────────────────

const testWGConf = `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.0.0.2/32
MTU = 1280
DNS = 1.1.1.1, 8.8.8.8

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = 127.0.0.1:9000
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
`

func TestParseWGConf(t *testing.T) {
	p, err := parseWGConf(testWGConf)
	if err != nil {
		t.Fatalf("parseWGConf: %v", err)
	}
	if len(p.addresses) != 1 || p.addresses[0].String() != "10.0.0.2" {
		t.Errorf("addresses = %v, ожидалось [10.0.0.2]", p.addresses)
	}
	if len(p.dns) != 2 || p.dns[0].String() != "1.1.1.1" || p.dns[1].String() != "8.8.8.8" {
		t.Errorf("dns = %v, ожидалось [1.1.1.1 8.8.8.8]", p.dns)
	}
	if p.mtu != 1280 {
		t.Errorf("mtu = %d, ожидалось 1280", p.mtu)
	}
	for _, want := range []string{
		"private_key=",
		"public_key=",
		"endpoint=127.0.0.1:9000",
		"allowed_ip=0.0.0.0/0",
		"persistent_keepalive_interval=25",
	} {
		if !strings.Contains(p.ipc, want) {
			t.Errorf("ipc не содержит %q", want)
		}
	}
}

func TestParseWGConfErrors(t *testing.T) {
	// Нет [Peer]
	if _, err := parseWGConf("[Interface]\nPrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nAddress = 10.0.0.2/32\n"); err == nil {
		t.Error("конфиг без [Peer] должен давать ошибку")
	}
	// Нет Address
	if _, err := parseWGConf("[Interface]\nPrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n\n[Peer]\nPublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n"); err == nil {
		t.Error("конфиг без Address должен давать ошибку")
	}
	// Пустой конфиг
	if _, err := parseWGConf(""); err == nil {
		t.Error("пустой конфиг должен давать ошибку")
	}
}

func TestParseWGConfDefaultDNSAndMTU(t *testing.T) {
	// Без DNS и MTU — подставляются дефолты.
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.0.0.2/32

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
AllowedIPs = 0.0.0.0/0
`
	p, err := parseWGConf(conf)
	if err != nil {
		t.Fatal(err)
	}
	if p.mtu != 1280 {
		t.Errorf("mtu = %d, ожидалось дефолтное 1280", p.mtu)
	}
	if len(p.dns) != 1 || p.dns[0].String() != "1.1.1.1" {
		t.Errorf("dns = %v, ожидался дефолтный [1.1.1.1]", p.dns)
	}
}

// ── sshHostKeyCallback ────────────────────────────────────────────────────────

func TestSSHHostKeyCallback(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256(sshPub.Marshal())
	pairs := make([]string, 32)
	for i, b := range digest {
		pairs[i] = fmt.Sprintf("%02X", b)
	}
	fp := strings.Join(pairs, ":")

	// Корректный fingerprint — проходит.
	if err := sshHostKeyCallback(fp)("", nil, sshPub); err != nil {
		t.Errorf("правильный fingerprint не прошёл: %v", err)
	}

	// Неверный fingerprint — ошибка (fail-closed).
	if err := sshHostKeyCallback("AA:BB:CC")("", nil, sshPub); err == nil {
		t.Error("неверный fingerprint должен давать ошибку")
	}

	// Пустой — тоже ошибка.
	if err := sshHostKeyCallback("")("", nil, sshPub); err == nil {
		t.Error("пустой fingerprint должен давать ошибку")
	}
}

// ── PatchWgConfig ─────────────────────────────────────────────────────────────

func TestPatchWgConfig(t *testing.T) {
	a := &App{}
	conf := `[Interface]
PrivateKey = x
Address = 10.0.0.2/32

[Peer]
PublicKey = y
Endpoint = old:9000
AllowedIPs = 0.0.0.0/0
`
	out := a.PatchWgConfig(conf, "1.2.3.4:9000")
	if !strings.Contains(out, "Endpoint = 1.2.3.4:9000") {
		t.Error("Endpoint не заменён")
	}
	if strings.Contains(out, "old:9000") {
		t.Error("старый Endpoint остался")
	}
	if !strings.Contains(out, "MTU = 1280") {
		t.Error("MTU не добавлен")
	}
}

func TestPatchWgConfigAddsEndpoint(t *testing.T) {
	a := &App{}
	conf := "[Interface]\nPrivateKey = x\nAddress = 10.0.0.2/32\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n"
	out := a.PatchWgConfig(conf, "1.2.3.4:9000")
	if !strings.Contains(out, "Endpoint = 1.2.3.4:9000") {
		t.Error("Endpoint должен добавляться при отсутствии")
	}
}
