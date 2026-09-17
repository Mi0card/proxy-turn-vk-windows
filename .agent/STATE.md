# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- Keep exactly these sections. Prune, never accrete: this file is read by every session,
     so every stale line here is a tax on all future work. History belongs in journal/.
     Contradicts git log / the journal (a session died before END)? Trust git: rebuild this
     file from the last journal entry + `git log -5`, note the crash in the journal. -->

Session: 17
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: patch (UNCOMMITTED) — 3 user-reported fixes, v0.3.1.1: (1) import config now refreshes the
        profile list immediately (frontend refreshProfiles + backend legacy→profile migration on
        import); (2) import/export completion toasts + cancel distinguished from error
        (SaveConfigResult); (3) ping visible again — pingLoop uses wgDialFallbackTimeout (netstack
        else direct) + 10s probe, and updateTunnelUI delegates the badge to updateStatusBadge
        (single writer). Green.
Next: commit v0.3.1.1 (needs user go-ahead). macOS lsof lookup + I13 (darwin captcha timeout) need a
      real-Mac check. No open issues (next id I22).
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- macOS `lookupProcessByPort` (lsof field mode) is NOT build/run verified — no Mac on this box
  (darwinkit/cgo). Same unverified-on-Mac class as I13. Re-check both on a real Mac.
- WinINET system proxy now follows the global «Трафик по умолчанию» (D16) like SOCKS5/HTTP — a
  user-set `direct`/`block` applies to it too (was unconditional strict tunnel). Stock default = proxy.
- sysProxyRestore/ApplyStatic are real WinINET registry ops → never call SystemProxyEnable in tests
  (mutates the user's proxy). Wails EventsEmit log.Fatal's on a nil ctx, so SystemProxyDisable isn't
  unit-testable either. Test seam gap recorded in proxy_test.go.
- Local go_client layer = `.agent/local/go-client-local/` (patch + apply.sh): ONLY GOOS=windows build
  fixes, re-applied by sync/build, fail-loud on drift (D13/D14). Engine mirrored from
  `SpaceNeuroX/proxy-turn-vk-android`; server_src linux/amd64 only. Slow module downloads → build/vet
  with `GOSUMDB=off`. Full build (build.ps1) needs Wails CLI + MSYS2 GCC — unverified. `-race`
  unavailable (CGO off, no GCC).
- Config top-level `fingerprint` = SSH host-key of VPS (deploy MITM guard) — NOT browser fingerprint;
  SaveConfig keeps it when empty (app.go). Do not repurpose that key.

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S16: feature 003 — connection-log format + app-by-port→PID (win/mac/stub) + system-proxy routing &
  logging; v0.3.1.0. COMMITTED 291b2e0.
- S15: fixed I8–I21 — P2 system-proxy restore/rollback + transport leak, frontend init guard + status
  badge, darwin captcha timeout; P3 dead-code removal, proxy relay dedup, log-renderer dedup, ts/lv
  escaping, DEFAULTS/version drift. v0.3.0.3.
- S14: audit — 14 findings filed (I8–I21); PROJECT.md stale lines corrected.

## Recently audited (cleared — stop re-litigating)
- S14 audit cleared: proxy transport pool (I6), system-proxy backup tests (I5), renderRuleSuggest XSS
  (I4), ruleset map concurrency, protobuf parsers, local go_client layer.
- S1/S2/S5 (uploadData cat> injection, deploy shellQuote) judged NON-issues; backend startup/config
  schema/build pipeline: no debt found.
