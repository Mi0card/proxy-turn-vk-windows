# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- Keep exactly these sections. Prune, never accrete: this file is read by every session,
     so every stale line here is a tax on all future work. History belongs in journal/.
     Contradicts git log / the journal (a session died before END)? Trust git: rebuild this
     file from the last journal entry + `git log -5`, note the crash in the journal. -->

Session: 19
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: patch (UNCOMMITTED) — CI sync fix. Upstream's .gitignore drops go.sum and server/ has no
        go.mod (server = upstream root module), so sync.yml's `rm -rf + cp -r` + `go build` died on
        "missing go.sum entry". sync.yml now restores server_src/go.mod+go.sum from upstream root,
        `go mod tidy`s both modules before the `git add -A`+`git diff --cached` gate; Node24 env.
Next: commit the sync fix (needs user go-ahead), then one manual workflow_dispatch run of sync.yml to
      confirm the PR path end-to-end. macOS lsof lookup + I13 (darwin captcha timeout) need a real-Mac
      check. No open issues (next id I22).
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
- S18: pingProbeTimeout 10s → 30s (real TURN dials 10-15s, 10s probe never emitted `tunnel:ping`);
  v0.3.1.2. COMMITTED b2bcb51.
- S17: config import refreshes the profile list; import/export toasts + cancel vs error; ping probe
  wgDialFallbackTimeout + single status-badge writer; v0.3.1.1. COMMITTED 850a51d.
- S16: feature 003 — connection-log format + app-by-port→PID (win/mac/stub) + system-proxy routing &
  logging; v0.3.1.0. COMMITTED 291b2e0.

## Recently audited (cleared — stop re-litigating)
- S14 audit cleared: proxy transport pool (I6), system-proxy backup tests (I5), renderRuleSuggest XSS
  (I4), ruleset map concurrency, protobuf parsers, local go_client layer.
- S1/S2/S5 (uploadData cat> injection, deploy shellQuote) judged NON-issues; backend startup/config
  schema/build pipeline: no debt found.
