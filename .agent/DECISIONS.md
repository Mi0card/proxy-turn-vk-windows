# Decisions — binding choices, one line each, newest first. Scan before designing anything;
# do not relitigate a decision without new information — build on it or surface the conflict.
# Cap 50 active lines; maintain.md moves superseded/expired ones to the Archive section.
# Format: `D<n> <YYYY-MM-DD> [scope] decision — why. (Supersedes D<m>.)`

- D18 2026-09-17 [frontend, user] header ping is a display-only probe: `wgDialFallbackTimeout`
  (netstack when active, else direct) with a 10s timeout, and `updateStatusBadge` is the sole
  status-badge writer — the strict tunnel-only 4s probe (44a36aa) hid the ping until the netstack
  came up and status events wiped it. No restart logic depends on ping since b2be86d.
- D17 2026-09-15 [proxy, user] connection-log format `→ host:port - app - [Nms, proxy|direct]`
  (block → `[block]`, error → `: ошибка: …`); app = local-port→PID→process basename, UA fallback,
  `—` if unknown; `→ ` prefix preserved (it routes lines to the «Подключения» tab, app.go:899). (S16, design 003.)
- D16 2026-09-15 [proxy, user] WinINET system proxy now applies routing rules and follows the global
  «Трафик по умолчанию» like SOCKS5/HTTP (was: unconditional strict tunnel); stock default `proxy`, so no
  leak out of the box, but a user-set `direct`/`block` applies too. (S16, design 003.)
- D15 2026-09-10 [frontend] UI field defaults live in HTML as the single source; JS captures them
  into `DEFAULTS` at load (main.js) instead of duplicating literals — prevents HTML↔JS drift (I19, S15).
- D14 2026-09-09 [engine, user] fingerprint feature dropped (GUI + app.go + go_client): upstream
  has no TLS-fingerprint switching (fixed Chrome_146), so the local layer shrank to GOOS=windows
  build fixes only — fewer patch anchors, sync drifts less. (Supersedes the fingerprint half of D13.)
- D13 2026-09-08 [engine, user] local go_client layer is the ONLY carved-out delta over upstream
  go_client/ — fingerprint switching (-fingerprint) + GOOS=windows build fixes; source of truth in
  `.agent/local/go-client-local/` (patch + apply.sh), re-applied by sync/build via `git apply`
  (fail-loud on drift). All other engine edits remain upstream fixes. (Relaxes D2.)
- D12 2026-09-08 [upstream, user] upstream = SpaceNeuroX/proxy-turn-vk-android (active, GPLv3);
  amurcanov archived; amurcanov/csqtt NOT viable (Rust rewrite + PolyForm Noncommercial). (Supersedes D2 "amurcanov".)
- D11 2026-09-08 [upstream, user] fingerprint-layer lands INSIDE the sync PR: apply.sh runs in sync.yml
  before diff/PR so the reapplied feature is visible for review in the same PR.

- D1 2026-09-01 [stack, observed] Wails v2 + vanilla JS/HTML/CSS frontend, WireGuard userspace over
  gvisor netstack, no admin rights anywhere — engine (go_client/server_src) auto-synced from upstream.
- D3 2026-09-01 [build, observed] engine/CI builds pin GOPROXY=goproxy.io,direct (slow default proxy
  on this box); build.ps1 and build.yml share the same 3-stage build (client → server → wails).

- D4 2026-09-01 [recovery, user] sleep/relay recovery = app-side auto-restart of the engine process (not
  `TunnelStop`); full process restart is required because the WG netstack is built once per engine lifetime.
- D5 2026-09-01 [recovery, user] bounded retries: stop after ~8 consecutive failed restarts with no fresh
  worker activity; fatal (wrong password / WRAP mismatch) keeps the hard stop.
- D6 2026-09-01 [recovery, user] wake detection = wall-clock polling (2s tick, 15s jump threshold),
  cross-platform; native WM_POWERBROADCAST deferred as follow-up.

- D7 2026-09-01 [recovery, user] add a dedicated network-change detector: poll `net.Interfaces()` every 5s,
  signature of non-loopback interfaces (name + up/running + sorted addrs), restart via `ensureRestart`.
- D8 2026-09-01 [recovery, user] network change = interface up/down OR IP change (not just IP); loopback filtered;
  deterministic sorted signature (no false triggers from OS slice order).
- D9 2026-09-01 [recovery, user] 30s cooldown between network-triggered restarts (anti-storm); budget NOT reset
  on network change (flapping must burn the 8-attempt budget).
- D10 2026-09-01 [recovery, user] wake & network-change auto-restore are optional (settings checkboxes), but
  default ENABLED — missing config keys / missing file keep current behavior (defaults set pre-unmarshal).

- (none yet)

## Archive (dead decisions — kept greppable, never loaded into working context)
