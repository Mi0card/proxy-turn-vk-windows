# Issues — the tracked backlog: bugs, debt, deferred work. One line each, newest first.
# If "we should fix that later" isn't a line here, it will be forgotten. Journal `flag:` lines are
# the informal inbox; maintain.md promotes the ones that matter into this file and drops the rest.
# Scan Open before designing or when picking what to work on; FIXING an issue is routed like any
# other request (patch/debug/feature) — this file only tracks, it never carries the work itself.
# Format: `I<n> <YYYY-MM-DD> P<1|2|3> [scope] symptom or task — evidence/repro (refs: D<n>, design, S<n>)`
#   P1 broken for users now · P2 wrong or risky, schedule it · P3 debt/idea, fix when passing.
#   Next <n> = highest number anywhere in this file + 1. One line; at most one indented detail
#   line. Needs more? Stub a design in .agent/designs/ and reference it — don't bloat this file.
# Close = move the line under Closed and append ` → closed <YYYY-MM-DD> S<n>: <fix or wontfix + why>`.
# Caps: Open ≤40 (over → merge duplicates, close the stale, demote to P3 or drop) ·
# Closed ≤100 (maintain.md deletes the oldest lines; git history keeps everything forever).

## Open (newest first — scan this section only)
- I1 2026-09-01 P3 [server_src] server tests can't run on Windows host (module is Linux-only;
  `ipc.UAPIOpen` undefined) — need a Linux env/CI job or a build-tag gate to run them (observed at bootstrap).

## Closed (append-only history; grep it, never load it wholesale)
- I7 2026-09-04 P3 [app] shutdown doesn't join finalizeTunnel goroutine (app.go:978) — TunnelStop kills proc but finalizeTunnel mutating tunnelRunning/sysProxyOn can race shutdown cleanup (audit S5) → closed 2026-09-07 S10: app.go finalizeDone channel signals finalizeTunnel completion; TunnelStop + quitApp wait for it (5s timeout) before proceeding.
- I6 2026-09-04 P3 [proxy] new http.Transport per non-CONNECT HTTP request (proxy.go:722), no pooling — inconsistent with sysProxyHandler which reuses one (proxy.go:777) (audit S5) → closed 2026-09-07 S9: proxy.go Transport pool (transportDirect + transportTunnel) replaces per-request creation; connection pooling enabled for non-CONNECT HTTP requests.
- I3 2026-09-04 P2 [proxy] zero test coverage on ProxyServer → closed 2026-09-04 S6: proxy_test.go added (route, conn-counters/setters, parseProxyAuth, wgB64ToHex, ParseDNSOverride); parseWGConf already covered in parse_test.go.
- I4 2026-09-04 P2 [frontend] XSS in renderRuleSuggest (main.js:1264-1277): rule `group` interpolated into data-value="${group}" + innerHTML with no escaping — corrupt .dat group name → HTML/attr injection (audit S5) → closed 2026-09-04 S8: added escAttr(), applied escAttr() to data-value attrs + escHtml() to innerHTML text (lines 1264, 1274, 1289).
- I5 2026-09-04 P3 [system_proxy] no tests for enable/disable/backup/restore (Windows-critical path) — blocks refactor.md cleanup there (audit S5) → closed 2026-09-04 S7: system_proxy_test.go added (backup path, save/load round-trip, load-no-file, load-corrupt, clear, overwrite, valid-JSON, SystemProxyStatus, SystemProxySupported, all-Had-false); sysProxyRead/sysProxyApplyStatic/sysProxyRestore are Windows-only registry ops, not cross-platform testable.
- I2 2026-09-01 P3 [harness] `.agent/` is git-ignored → agent state is local-only, cross-machine handoff impossible — decide whether to track harness state or accept local-only (ref: D1) → closed 2026-09-04: premise false — .agent/ removed from .gitignore by user.
