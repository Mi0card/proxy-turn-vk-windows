# Project — stable facts beyond CLAUDE.md. Read before feature work or when confused. Cap: 80 lines.

## Architecture (≤10 lines — how the pieces talk; MAP.md owns the directory list)
Wails v2 desktop app (`main.go`) embeds `frontend/` (vanilla JS), `wdtt-client.exe` (tunnel engine,
go:embed), `assets/server/wdtt-server` (Linux binary) and `assets/deploy.sh`. The Go backend (`app.go`)
spawns `wdtt-client.exe` as a child process (WireGuard-over-VK-TURN tunnel, pion TURN), reads its
stdout for events, and wraps it with SOCKS5/HTTP proxies (`proxy.go`, gvisor netstack, no admin).
Rule-based routing (`ruleset_manager.go`) filters traffic; Windows system proxy (`system_proxy.go` +
`syscall_windows.go`) redirects WinINET traffic. Deploy/remove push the embedded `wdtt-server` to a
VPS over SSH (`golang.org/x/crypto/ssh`) with fingerprint verification. VK captcha is solved by the
user in a native WebView2 (Windows) / WKWebView (macOS) window, then fed back to the engine.
`go_client/` and `server_src/` are two separate Go modules auto-synced from the upstream Android repo.

## Constraints & non-goals (hard requirements; things deliberately unsupported)
- NO proxy/feature requires admin rights — deliberate architecture; do not break in proxy.go/system_proxy.go.
- System proxy settings must auto-restore on app close/crash (WinINET HKCU) — defer/recover is load-bearing.
- `go_client/` and `server_src/` are auto-synced: NEVER edit locally; bugs there are upstream fixes
  (amurcanov/proxy-turn-vk-android), not local patches.
- Frontend stays vanilla JS/HTML/CSS — no bundlers/frameworks without an explicit request.
- Educational project; no production guarantees. License GPL-3.0 — check compat before adding deps.
- macOS: no system-proxy feature (checkbox hidden); SOCKS5 127.0.0.1:1080 / HTTP 127.0.0.1:1081 manual.
- server_src compiles for linux/amd64 only; its `go test` cannot run on a Windows host.

## Glossary (domain terms with exact meanings — misreading one produces wrong code)
- wdtt:// link — deeplink carrying connection params (vk/srv/sec/n) for the tunnel.
- TURN — relay protocol (pion/turn) used to bridge to VK; "WireGuard over VK TURN" = the tunnel.
- netstack — gvisor userspace TCP/IP stack; lets proxies run without OS admin/privileges.
- WinINET — Windows internet settings store (registry HKCU) that browsers/Office honor.
- ruleset — routing rule set: `domain:`, `domain-suffix:`, `keyword:`, `regex:`, `cidr:`, `ip:` and
  `ruleset:geosite-<g>`/`geoip-<g>` from v2ray protobuf dat files; policies block/direct/proxy, first match wins.
- AppVersion — version const in app.go:29 (`0.2.10.0`); bumping it triggers the CI build.
- CaptchaMode / ObfsMode — per-profile tunnel options surfaced in the UI and passed to the engine.

## Landmines (cross-cutting gotchas, ≤15; area-specific ones belong in .agent/areas/)
<!-- format: symptom → actual cause → what to do instead -->
- go:embed requires wdtt-client.exe & assets/server/wdtt-server to exist → they're byte-stub
  (MZ/ELF magic) placeholders in the repo; build.ps1/CI overwrite with real binaries before wails build.
- server_src `go build ./...` fails on Windows host (`ipc.UAPIOpen` undefined) → it's Linux-only;
  always cross-compile with GOOS=linux GOARCH=amd64 CGO_ENABLED=0.
- Dependency downloads are slow on this box (default proxy) → build.ps1 sets GOPROXY=goproxy.io,direct;
  use the same for engine modules to make builds finish.
- `AppVersion` is read by both build.ps1 and build.yml (git diff on app.go) → bumping version is how
  you trigger a CI build; a commit that touches app.go without a bump won't build.
- Deploy flow depends on the embedded wdtt-server binary & deploy.sh being fresh → they are refreshed
  by sync.yml only when upstream files change; stale engine = stale embedded server.
- `.agent/`, `CLAUDE.md`, `AGENTS.md` are git-ignored (user's choice) → agent state is local-only,
  not persisted through git; crash-recovery across clones won't work (journal still exists locally).
- Rules dat-files are cached on disk (v2ray protobuf format, parsed without deps) → cache staleness
  shows up as wrong routing until "Обновить правила"; parsers live in ruleset_manager.go.
- Cross-platform build tags matter: syscall_/captcha_/tray_ files are per-OS — keep all buildable
  (syscall_unix.go & captcha_webview_other.go & tray_stub.go are the non-Win/mac fallbacks).
- Manual captcha on macOS goes through darwinkit (WKWebView) — heavier dep; Windows uses go-webview2.