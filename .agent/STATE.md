# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- Keep exactly these sections. Prune, never accrete: this file is read by every session,
     so every stale line here is a tax on all future work. History belongs in journal/.
     Contradicts git log / the journal (a session died before END)? Trust git: rebuild this
     file from the last journal entry + `git log -5`, note the crash in the journal. -->

Session: 2
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: none
Next: first real slice (user to pick from ISSUES or a brief); maintain.md when S10
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- `.agent/` is git-ignored (user choice, D1): state/journal are LOCAL-ONLY, no git persistence.
- go_client/ & server_src/ are auto-synced from upstream — NEVER edit; bugs = upstream fixes.
- server_src builds for linux/amd64 only; its tests can't run on Windows.
- Slow module downloads here → set GOPROXY=https://goproxy.io,direct (see build.ps1).
- Full app build (build.ps1) needs Wails CLI + MSYS2 GCC — not installed on this box (unverified).

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S2 wake auto-restart · S3 network-change auto-restart (pollers + bounded single-flight restart).
- S4 both auto-restores optional: Config flags (default on) + «Автовосстановление соединения» checkboxes.