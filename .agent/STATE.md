# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- History belongs in journal/. Contradicts git log/journal? Trust git, note the crash there. -->

Session: 25
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: none. S25 rebased the local go_client layer patch for upstream v1.4.5; sync CI run 146
        (step «Re-apply local go_client layer») is fixed.
Next: dispatch the «Sync from upstream» workflow (or wait for the 06:00 UTC schedule) to open the
      v1.4.5 PR into dev; review + merge, then dev→main. Local go_client/server_src stay v1.4.4
      until that PR lands — running apply.sh on this tree still fails by design (it expects a
      freshly synced go_client).
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- DNS in the netstack: gvisor's resolver walks Config.DNS SEQUENTIALLY (5s each ×2 passes) — a dead
  server in the `dns` list = +5s on EVERY fresh hostname. proxy.go resolves in parallel + caches
  (5m/neg30s, D22) — keep that path; don't revert dialNetstack to plain tnet.DialContext.
  A positive answer must always win; NXDOMAIN is final only after all servers reply (S24).
- Build toolchain now present on this box: wails v2.16.0 (~/go/bin) + MSYS2 gcc 16.2.0
  (C:\msys64\mingw64\bin); pacman mirror pinned to repo.msys2.org (default mirror.msys2.org is
  connection-reset on this network). build.ps1's Rename fails if the target version exe already exists.
- macOS system proxy IS implemented (syscall_darwin.go, sysProxySupported=true) — do NOT re-document
  as unavailable. macOS code still NOT build/run-verified locally (no Mac). GPU toggle Windows-only.
- System proxy follows global «Трафик по умолчанию» (D16); stock default = proxy (no leak).
- Config top-level `fingerprint` = SSH host-key of VPS (deploy MITM guard) — NOT browser fingerprint.

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S25: sync CI unblocked — local go_client layer patch rebased onto upstream v1.4.5 (dev).
- S24/S23: NXDOMAIN masking fix + parallel DNS resolver, shipped as v0.3.2.1 (dd44ff2).
- S22: README rewrite + macOS system-proxy doc correction (committed).

## Recently audited (cleared — stop re-litigating)
- S14 audit cleared: proxy transport pool (I6), system-proxy backup tests (I5), renderRuleSuggest XSS
  (I4), ruleset map concurrency, protobuf parsers, local go_client layer.
- S1/S2/S5 (uploadData cat> injection, deploy shellQuote) judged NON-issues; backend startup/config
  schema/build pipeline: no debt found.
