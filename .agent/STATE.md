# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- Keep exactly these sections. Prune, never accrete: this file is read by every session,
     so every stale line here is a tax on all future work. History belongs in journal/.
     Contradicts git log / the journal (a session died before END)? Trust git: rebuild this
     file from the last journal entry + `git log -5`, note the crash in the journal. -->

Session: 10
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: none
Next: I1 open (Linux-only server tests)
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- `.agent/` is git-ignored (user choice, D1): state/journal are LOCAL-ONLY, no git persistence.
- go_client/ & server_src/ are auto-synced from upstream — NEVER edit; bugs = upstream fixes.
- server_src builds for linux/amd64 only; its tests can't run on Windows.
- Slow module downloads here → set GOPROXY=https://goproxy.io,direct (see build.ps1).
- Full app build (build.ps1) needs Wails CLI + MSYS2 GCC — not installed on this box (unverified).

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S8 I4 closed: frontend XSS fix — escAttr() + escHtml() applied to renderRuleSuggest data-value + innerHTML.
- S9 I6 closed: proxy.go Transport pool — connection pooling for non-CONNECT HTTP requests (direct + tunnel).
- S10 I7 closed: app.go finalizeDone channel — TunnelStop + quitApp wait for finalizeTunnel completion (5s timeout).

## Recently audited (cleared — stop re-litigating)
- Backend startup, config schema, build pipeline: no debt found. S5 re-checked DECISIONS: no `(assumed)` lines.
- S1 (uploadData cat> injection) & S2 (deploy shellQuote) judged NON-issues: literal-only callers / correct POSIX quoting.
- S6 parseWGConf already covered by parse_test.go — proxy_test.go omits it to avoid duplication.
