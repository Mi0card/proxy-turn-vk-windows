# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- History belongs in journal/. Contradicts git log/journal? Trust git, note the crash there. -->

Session: 20
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: patch (UNCOMMITTED) — CI sync stray-binary fix. sync.yml's Windows build left
        go_client/wg-turn-client.exe (cleanup targeted `wg-turn-client`, no `.exe`), so `git add -A`
        staged the 24 MB binary and create-pull-request's final `git checkout dev` aborted (job exit 1).
        .gitignore now ignores go_client/wg-turn-client.exe; both build steps `rm -f wg-turn-client
        wg-turn-client.exe`.
Next: commit+push .gitignore + sync.yml to dev (and main) — needs user go-ahead; then one manual
      workflow_dispatch run of sync.yml to confirm the PR path end-to-end. macOS lsof lookup + I13
      (darwin captcha timeout) need a real-Mac check. No open issues (next id I22).
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- macOS `lookupProcessByPort` (lsof field mode) is NOT build/run verified — no Mac on this box
  (darwinkit/cgo). Same unverified-on-Mac class as I13. Re-check both on a real Mac.
- WinINET system proxy now follows the global «Трафик по умолчанию» (D16) like SOCKS5/HTTP — a
  user-set `direct`/`block` applies to it too (was unconditional strict tunnel). Stock default = proxy.
- sysProxyRestore/ApplyStatic are real WinINET registry ops → never call SystemProxyEnable in tests
  (mutates the user's proxy). Wails EventsEmit log.Fatal's on a nil ctx, so SystemProxyDisable isn't
  unit-testable either. Test seam gap recorded in proxy_test.go.
- Engine = upstream mirror: go_client layer in `.agent/local/go-client-local/` (patch + apply.sh,
  GOOS=windows fixes only, fail-loud on drift — D13/D14); upstream IGNORES go.sum and server/ has no
  go.mod, so sync.yml seeds server_src/go.mod+go.sum from upstream root + `go mod tidy`s both (D19).
  server_src linux/amd64 only; slow downloads → GOSUMDB=off. build.ps1 needs Wails+GCC; `-race` off.
- Config top-level `fingerprint` = SSH host-key of VPS (deploy MITM guard) — NOT browser fingerprint;
  SaveConfig keeps it when empty (app.go). Do not repurpose that key.

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S20: CI sync unblocked — .gitignore + sync.yml remove/ignore `go_client/wg-turn-client.exe` (was aborting create-pull-request's `git checkout dev`). UNCOMMITTED.
- S19: CI sync fix — server_src go.mod/go.sum restored from upstream root + `go mod tidy` both modules; Node24; staged-diff gate. COMMITTED a333d6c.
- S18: pingProbeTimeout 10s → 30s (real TURN dials 10-15s); v0.3.1.2. COMMITTED b2bcb51.

## Recently audited (cleared — stop re-litigating)
- S14 audit cleared: proxy transport pool (I6), system-proxy backup tests (I5), renderRuleSuggest XSS
  (I4), ruleset map concurrency, protobuf parsers, local go_client layer.
- S1/S2/S5 (uploadData cat> injection, deploy shellQuote) judged NON-issues; backend startup/config
  schema/build pipeline: no debt found.
