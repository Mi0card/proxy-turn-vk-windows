# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- Keep exactly these sections. Prune, never accrete: this file is read by every session,
     so every stale line here is a tax on all future work. History belongs in journal/.
     Contradicts git log / the journal (a session died before END)? Trust git: rebuild this
     file from the last journal entry + `git log -5`, note the crash in the journal. -->

Session: 12
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: dead-tunnel auto-restart made conservative (grace 90s, stale-activity gate, 3-min cooldown,
       ping threshold 5) — v0.3.0.1, about to commit+push
Next: commit+push v0.3.0.1; I1 open (Linux-only server tests)
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- Local go_client layer = `.agent/local/go-client-local/` (patch + apply.sh): now ONLY GOOS=windows
  build fixes (listen retry/dynamic-port + tun_fd unavailable), re-applied by sync/build, fail-loud
  on drift (D13 windows half). Fingerprint feature removed repo-wide (D14). go_client/ in repo is
  upstream master + this layer (no local fingerprint code anywhere).
- go_client/ & server_src/ mirrored from `SpaceNeuroX/proxy-turn-vk-android` (go_client ← go_client,
  server_src ← server). server_src = standalone module mirroring upstream ROOT go.mod (dtls v3.1.5,
  transport v4.0.2) + copied upstream go.sum; keep in step when upstream bumps.
- server_src builds for linux/amd64 only (GOOS=linux GOARCH=amd64); its tests can't run on Windows.
- Slow module downloads / sum.golang.org TLS timeouts → build/vet with `GOSUMDB=off`. Full app build
  (build.ps1) needs Wails CLI + MSYS2 GCC — not installed on this box (unverified).
- Config top-level `fingerprint` = SSH host-key of VPS (deploy MITM guard) — NOT browser fingerprint;
  SaveConfig keeps it when empty (app.go). Do not repurpose that key.

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S12 cont: dead-tunnel рестарт сделан консервативным (жалоба «постоянно реконнектит»): единый
  гейт requestDeadRestart — grace 90s, активность воркеров <2 мин блокирует, кулдаун 3 мин
  (не сбрасывается активностью); ping-порог 3→5. Тесты. v0.3.0.1.
- S12: qwdtt://config импорт профиля (ParseQwdtt + importQwdtt), коммит f0bb564 v0.3.0.0.
- S12: fingerprint удалён + dead-tunnel авто-рестарт добавлен, коммит 44a36aa.
- S10: app.go finalizeDone channel — TunnelStop + quitApp wait for finalizeTunnel (5s timeout).

## Recently audited (cleared — stop re-litigating)
- Backend startup, config schema, build pipeline: no debt found. S5 re-checked DECISIONS: no `(assumed)` lines.
- S1 (uploadData cat> injection) & S2 (deploy shellQuote) judged NON-issues: literal-only callers / correct POSIX quoting.
- `.agent/` is NOT git-ignored (old STATE line was wrong): state/journal + layer ARE git-tracked.
