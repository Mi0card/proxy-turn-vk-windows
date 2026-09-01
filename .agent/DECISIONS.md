# Decisions — binding choices, one line each, newest first. Scan before designing anything;
# do not relitigate a decision without new information — build on it or surface the conflict.
# Cap 50 active lines; maintain.md moves superseded/expired ones to the Archive section.
# Format: `D<n> <YYYY-MM-DD> [scope] decision — why. (Supersedes D<m>.)`

- D1 2026-09-01 [stack, observed] Wails v2 + vanilla JS/HTML/CSS frontend, WireGuard userspace over
  gvisor netstack, no admin rights anywhere — engine (go_client/server_src) auto-synced from upstream.
- D2 2026-09-01 [engine, observed] `go_client/` & `server_src/` are separate Go modules synced from
  amurcanov/proxy-turn-vk-android — local edits are overwritten; bugs there are upstream fixes.
- D3 2026-09-01 [build, observed] engine/CI builds pin GOPROXY=goproxy.io,direct (slow default proxy
  on this box); build.ps1 and build.yml share the same 3-stage build (client → server → wails).

- D4 2026-09-01 [recovery, user] sleep/relay recovery = app-side auto-restart of the engine process (not
  `TunnelStop`); full process restart is required because the WG netstack is built once per engine lifetime.
- D5 2026-09-01 [recovery, user] bounded retries: stop after ~8 consecutive failed restarts with no fresh
  worker activity; fatal (wrong password / WRAP mismatch) keeps the hard stop.
- D6 2026-09-01 [recovery, user] wake detection = wall-clock polling (2s tick, 15s jump threshold),
  cross-platform; native WM_POWERBROADCAST deferred as follow-up.

- (none yet)

## Archive (dead decisions — kept greppable, never loaded into working context)
