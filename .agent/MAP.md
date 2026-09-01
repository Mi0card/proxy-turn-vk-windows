# Map — where things live. First stop when locating code; grep comes after, wholesale reading never.
<!-- One line per module: `path — what it is; entry: <file>`. Update on any structure change.
     Cap 120 lines: when over, collapse a subtree into .agent/areas/<x>.md and keep one line here
     pointing at it. `(?)` marks unverified bootstrap guesses — verify on first visit, then remove. -->

/ (root module `wdtt`) — Wails GUI app: backend + go:embed of frontend & binaries
  - app.go — backend: tunnel lifecycle, config/profiles, deploy, watchdog; entry: main.go; version const AppVersion
  - proxy.go — SOCKS5 + HTTP proxies over WireGuard netstack (gvisor); no admin rights
  - ruleset_manager.go — rule-based routing (domain/cidr/ip + geosite/geoip dat files)
  - system_proxy.go — Windows system proxy wrapper over WinINET (macOS: checkbox hidden)
  - syscall_windows.go / syscall_unix.go / syscall_darwin.go — WinINET & cross-compile stubs
  - captcha_interceptor.go + captcha_webview_{windows,darwin,other}.go — manual VK captcha window
  - tray_{common,windows,darwin,stub,icon}.go — system tray (platform variants + stub)
  - parse_test.go, ruleset_manager_test.go — unit tests
frontend/ — vanilla HTML/CSS/JS UI (no framework, no bundler); entry: index.html
go_client/ — tunnel engine, AUTO-SYNCED from upstream — DO NOT EDIT; entry: main.go
server_src/ — Linux server (wdtt-server), AUTO-SYNCED — DO NOT EDIT; entry: main.go; Linux-only
assets/ — embedded runtime assets: deploy.sh (synced), server/wdtt-server (build artifact stub in repo)
build/ — Wails build scaffolding (windows/, appicon.png) + build/bin output
tools/icon — icon generator (module, no tests)
.github/workflows/ — build.yml (Windows+macOS, version-gated) · sync.yml (upstream auto-sync → PR)
.agent/ — agent harness: state, workflows, designs, journal (human manual: .agent/README.md)