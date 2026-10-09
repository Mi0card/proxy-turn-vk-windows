# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- History belongs in journal/. Contradicts git log/journal? Trust git, note the crash there. -->

Session: 22
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: docs (UNCOMMITTED, on top of S21) — README.md rewritten (Russian, restructured/tightened:
        download + quick-start up front, routing trimmed, added tray / GPU toggle / auto-restore /
        settings / deps incl. darwinkit / accurate file tree; log view now «Логи → Подключения»).
        Corrected stale "macOS has no system proxy" in README.md AND .agent/PROJECT.md — it IS
        supported via networksetup (syscall_darwin.go, sysProxySupported=true, since v0.2.10.0).
        S21 (still uncommitted): overheat fix, GPU default on + proxy event coalescing + GPU toggle.
Next: user rebuilds + tests S21 on the hot laptop (idle vs system-proxy-on, watch msedgewebview2.exe).
      Commit/push pending: README+PROJECT.md docs; S21 overheat fix; S20 .gitignore + sync.yml to dev/main.
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- macOS system proxy IS implemented (syscall_darwin.go: networksetup web/secure proxy per service;
  sysProxySupported=true) — do NOT re-document it as unavailable. GPU toggle card is Windows-only
  (gpuSupported false on darwin). macOS code (lsof lookupProcessByPort, darwin proxy) still NOT
  build/run-verified locally — no Mac on this box; re-check on a real Mac.
- GPU: WebviewGpuIsDisabled was true since v0.2.9.5; now false by default. Black/flicker window →
  WDTT_DISABLE_GPU=1 restores software rendering. Toggle is a settings card, applies after restart.
- System proxy (WinINET on Windows, networksetup on macOS) follows the global «Трафик по умолчанию»
  (D16) like SOCKS5/HTTP — a user-set `direct`/`block` applies to it too. Stock default = proxy.
- Engine = upstream mirror: go_client layer in `.agent/local/go-client-local/` (patch + apply.sh,
  GOOS=windows fixes only, fail-loud on drift — D13/D14); upstream IGNORES go.sum and server/ has no
  go.mod, so sync.yml seeds server_src/go.mod+go.sum from upstream root + `go mod tidy`s both (D19).
  server_src linux/amd64 only; slow downloads → GOSUMDB=off. build.ps1 needs Wails+GCC; `-race` off.
- Config top-level `fingerprint` = SSH host-key of VPS (deploy MITM guard) — NOT browser fingerprint;
  SaveConfig keeps it when empty (app.go). Do not repurpose that key.

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S22: README rewrite + macOS-system-proxy doc correction (README.md, PROJECT.md). UNCOMMITTED.
- S21: overheat fix — GPU default on + proxy event coalescing/batching, plus a GPU settings toggle (Config.DisableGpu; see Active). UNCOMMITTED.
- S20: CI sync unblocked — .gitignore + sync.yml remove/ignore `go_client/wg-turn-client.exe` (was aborting create-pull-request's `git checkout dev`). UNCOMMITTED.

## Recently audited (cleared — stop re-litigating)
- S14 audit cleared: proxy transport pool (I6), system-proxy backup tests (I5), renderRuleSuggest XSS
  (I4), ruleset map concurrency, protobuf parsers, local go_client layer.
- S1/S2/S5 (uploadData cat> injection, deploy shellQuote) judged NON-issues; backend startup/config
  schema/build pipeline: no debt found.
