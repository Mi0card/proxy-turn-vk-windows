# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- History belongs in journal/. Contradicts git log/journal? Trust git, note the crash there. -->

Session: 23
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: none. S23 (first-connect latency fix) COMPLETE, UNCOMMITTED. Bumped v0.3.1.3 → v0.3.2
        (app.go:30 + wails.json) → will trigger the CI build on push. Prior S22 docs + S21 overheat fix
        + S20 CI-sync still UNCOMMITTED.
Next: commit/push pending: S23 (proxy.go DNS resolver + tests, go.mod, version bump); S22 README/PROJECT;
      S21 overheat fix; S20 .gitignore + sync.yml.
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- DNS in the netstack: gvisor's resolver walks Config.DNS SEQUENTIALLY (5s each ×2 passes) — a dead
  server in the `dns` list = +5s on EVERY fresh hostname. proxy.go now resolves in parallel + caches
  (5m/neg30s, D22) — keep that path; don't revert dialNetstack to plain tnet.DialContext.
- Build toolchain now present on this box: wails v2.16.0 (~/go/bin) + MSYS2 gcc 16.2.0
  (C:\msys64\mingw64\bin); pacman mirror pinned to repo.msys2.org (default mirror.msys2.org is
  connection-reset on this network). build.ps1's Rename fails if the target version exe already exists.
- macOS system proxy IS implemented (syscall_darwin.go, sysProxySupported=true) — do NOT re-document
  as unavailable. macOS code still NOT build/run-verified locally (no Mac). GPU toggle Windows-only.
- System proxy follows global «Трафик по умолчанию» (D16); stock default = proxy (no leak).
- Config top-level `fingerprint` = SSH host-key of VPS (deploy MITM guard) — NOT browser fingerprint.

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S23: first-connect latency fix — parallel DNS resolver + TTL cache in proxy.go (D22). UNCOMMITTED, v0.3.2.
- S22: README rewrite + macOS-system-proxy doc correction. UNCOMMITTED.
- S21: overheat fix — GPU default on + proxy event coalescing + GPU settings toggle. UNCOMMITTED.

## Recently audited (cleared — stop re-litigating)
- S14 audit cleared: proxy transport pool (I6), system-proxy backup tests (I5), renderRuleSuggest XSS
  (I4), ruleset map concurrency, protobuf parsers, local go_client layer.
- S1/S2/S5 (uploadData cat> injection, deploy shellQuote) judged NON-issues; backend startup/config
  schema/build pipeline: no debt found.
