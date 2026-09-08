# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- Keep exactly these sections. Prune, never accrete: this file is read by every session,
     so every stale line here is a tax on all future work. History belongs in journal/.
     Contradicts git log / the journal (a session died before END)? Trust git: rebuild this
     file from the last journal entry + `git log -5`, note the crash in the journal. -->

Session: 11
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: M11 engine upstream migrated to SpaceNeuroX — build/vet verified, uncommitted
Next: commit the migration (61 changed/untracked files incl. .agent/local/ + go_client/ + server_src/);
      GUI Phase 6 touches; I1 open (Linux-only server tests)
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- Local go_client layer = `.agent/local/go-client-local/` (patch + apply.sh): fingerprint switching +
  GOOS=windows build fixes, re-applied by sync/build, fail-loud on drift (D13/D11). NEW FILES NOT
  YET `git add`-ed — commit BEFORE any sync/build run, else CI lacks the layer.
- go_client/ & server_src/ mirrored from `SpaceNeuroX/proxy-turn-vk-android` (go_client ← go_client,
  server_src ← server). server_src = standalone module mirroring upstream ROOT go.mod (dtls v3.1.5,
  transport v4.0.2) + copied upstream go.sum; keep in step when upstream bumps.
- server_src builds for linux/amd64 only (GOOS=linux GOARCH=amd64); its tests can't run on Windows.
- Slow module downloads / sum.golang.org TLS timeouts → build/vet with `GOSUMDB=off`; network unruly
  yesterday but go_client + server_src builds succeeded. Full app build (build.ps1) needs Wails CLI
  + MSYS2 GCC — not installed on this box (unverified).
- app.go-client flags unchanged (GUI args still valid); deploy.sh v3.2 ExecStart flags (-listen
  -wg-port -config-dir) all supported by new server — no app.go deploy changes needed.

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S11 M11 part 1: go_client & server_src replaced with upstream SpaceNeuroX; local layer carries
  fingerprint + Windows build; both `go build` + `go vet` green; root `go build` + `go test` green.
- S10 I7 closed: app.go finalizeDone channel — TunnelStop + quitApp wait for finalizeTunnel (5s timeout).
- S9 I6 closed: proxy.go Transport pool — connection pooling for non-CONNECT HTTP requests.

## Recently audited (cleared — stop re-litigating)
- Backend startup, config schema, build pipeline: no debt found. S5 re-checked DECISIONS: no `(assumed)` lines.
- S1 (uploadData cat> injection) & S2 (deploy shellQuote) judged NON-issues: literal-only callers / correct POSIX quoting.
- `.agent/` is NOT git-ignored (old STATE line was wrong): state/journal + layer ARE git-tracked.