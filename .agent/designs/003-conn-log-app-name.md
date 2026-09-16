# 003: Connection log — app name + unified format + system-proxy routing

Status: shipped
Date: 2026-09-15 · Session: S16

## Problem
The «Подключения» log shows `→ host:port  [10391ms, туннель]` — no application, and the
type token is verbose Russian. The user wants `→ host:port - chrome.exe - [10391ms, proxy]`.
Additionally the WinINET system proxy (`sysProxyHandler`) ignores routing rules entirely
(always strict-tunnel) and logs nothing, so browser traffic that uses it is invisible.

## Constraints
- Keep the `→ ` prefix: `App.socksLog` (app.go:899) routes `→ `-prefixed lines to the
  «Подключения» tab; changing it would move connection lines to the general log.
- Unmatched traffic follows the global «Трафик по умолчанию» (`RoutingDefault`), same as the
  SOCKS5/HTTP proxies — **user decision** (2026-09-15). A user-set default of `direct` therefore
  also affects the system proxy; the stock default is `proxy`, so no leak out of the box.
- No new dependencies (use `syscall.NewLazyDLL` like tray/wininet).
- Cross-platform: macOS best-effort, must not break `!windows` builds.
- The `ужесточение` of system proxy (strict tunnel) was intentional (README:29) — this feature
  deliberately relaxes it to rule-based routing.

## Current state
- `proxy.go` — `ProxyServer` owns rules (`route`, `dialForRoute`, `log`) and all connection log
  lines: SOCKS5 block/err/ok (:594/:603/:612), HTTP CONNECT block/err/ok (:671/:678/:687),
  HTTP GET block/ok (:709/:736). Only SOCKS5 success measures delay.
- `proxy.go:806` — `sysProxyHandler` (separate WinINET listener from `system_proxy.go:113`) has
  one hardcoded tunnel transport and calls `wgDialStrict` directly; no rules, no logging.
- `system_proxy.go:113` — `newSysProxyHandler()` created on enable.
- `app.go:899` — `socksLog` splits log streams by `→ ` prefix.
- `ruleset_manager.go:41` — policies are literally `block|direct|proxy`.
- `syscall_windows.go` / `syscall_darwin.go` / `syscall_unix.go` — platform-function pattern
  (build tags); `kernel32` lazy DLL already declared in `tray_windows.go`.

## Options
**App identification**
- A. User-Agent only — cheap, but CONNECT/SOCKS5 (the bulk of the log) stay `—`.
- B. Local port → PID → process name — works for every path (SOCKS5, CONNECT, GET).
- **Pick B**, with UA as HTTP fallback: the user's desired token is a process name (`chrome.exe`),
  and most log lines are CONNECT/SOCKS5 where UA is unavailable.

**Port→PID on macOS**
- A. `lsof -nP -iTCP -Fpcn -sTCP:ESTABLISHED` — field mode (`p`/`c`/`n` lines) is robust to
  spaces in process names ("Google Chrome") and needs no column guessing; one fork, cached.
- B. `netstat -anv -p tcp` (PID column in verbose) → `ps -p <pid> -o comm=` — two forks and
  fragile column layout that varies across macOS versions.
- **Pick A**; flagged unverifiable on this box (no Mac, same class as I13).

**System proxy routing**
- A. Keep strict tunnel, only add logging — but the user explicitly requires rule-based routing.
- B. Give `sysProxyHandler` a `*ProxyServer` back-ref and reuse `route`/`dialForRoute`/`log`.
- **Pick B**: rules already live on `a.proxy` (app.go:1803 `applyRouting`) and are always current.

**Cache**
- Port→name TTL cache (3 s, ≤256 entries), non-empty results only — avoids a TCP-table walk /
  `netstat` fork per request while bounding port-reuse staleness to 3 s.

## Design
Platform interface (one impl per build tag; `""` = unknown):
```go
func lookupProcessByPort(port int) string
```
- Windows (`syscall_windows.go`): `GetExtendedTcpTable(AF_INET|AF_INET6, TCP_TABLE_OWNER_PID_ALL)`
  → row with `localPort == port` → `dwOwningPid` → `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)`
  → `QueryFullProcessImageNameW` → `filepath.Base`.
- Darwin (`syscall_darwin.go`): `lsof -nP -iTCP -Fpcn -sTCP:ESTABLISHED` → field-mode parse
  (`p`=PID, `c`=command, `n`=address) → match local port → command name.
- Unix (`syscall_unix.go`): `""`.

Shared helpers (proxy.go):
```go
const unknownApp = "—"
func clientPortFromAddr(addr string) int          // "127.0.0.1:54321" → 54321, else 0
func uaAppName(ua string) string                  // "Firefox/137" → "Firefox", "" if none
func connAppName(clientPort int, ua string) string// PID lookup → UA → unknownApp; cached
func connLine(target, app, policy string, ms int) string   // "→ target - app - [msms, policy]"
func connLineBlock(target, app string) string              // "→ target - app - [block]"
func connLineError(target, app, err string) string         // "→ target - app: ошибка: err"
```
`policy` is passed through unchanged (already `proxy|direct`). Block lines omit delay; error lines
keep level `warn`; success keeps `dim`.

Call sites rewrite to `connLine*`; CONNECT and GET gain `start := time.Now()` around
`dialForRoute` / `transport.RoundTrip`. SOCKS5 uses `c.RemoteAddr()`, HTTP uses `r.RemoteAddr`;
GET also passes `r.Header.Get("User-Agent")`.

System proxy (`proxy.go`): `sysProxyHandler{p *ProxyServer, transportDirect, transportTunnel}`;
`newSysProxyHandler(p *ProxyServer)`; `handleConnect`/`handleHTTP` mirror the main handlers
(route → block 403 / direct-vs-tunnel transport / log), keeping the 90 s idle-conn pool and
`Close()` semantics. `system_proxy.go:113` passes `a.proxy`.

Version: `AppVersion` (app.go:30) and `wails.json` `productVersion` → `0.3.1.0`.

## Edge cases & failure modes
| # | Case | Expected behavior | Covered by |
|---|---|---|---|
| 1 | port lookup returns nothing (closed/unknown/timing) | UA fallback (HTTP) else `—` | TestConnAppName |
| 2 | malformed/empty `RemoteAddr` | `clientPortFromAddr`→0 → `—` | TestClientPortFromAddr |
| 3 | port == 0 / negative | treated as unknown → `—` | TestClientPortFromAddr |
| 4 | port reused within TTL | ≤3 s stale name (accepted for a log) | design note |
| 5 | rule = block | 403 + `[block]` log, no dial | TestSysProxyBlock |
| 6 | no tunnel + proxy policy | 502 (strict), unchanged | TestSysProxy*NoTunnel |
| 7 | rule = direct, reachable target | direct dial + `[Nms, direct]` | TestSysProxyDirect |
| 8 | macOS lsof missing/slow | `""` → `—`; cache limits cost | design note |
| 9 | concurrent lookups | cache mutex-guarded | TestAppNameCache (race-free by design) |
| 10 | GET target has no explicit port | default 80/443 added | TestConnLine |
| 11 | non-Windows/non-Darwin build | stub compiles, `—` | `go build` on unix tag |

## Test plan
Unit: format builders, `uaAppName`, `clientPortFromAddr`, cache. Windows: `lookupProcessByPort`
against the test process's own live socket (must equal `filepath.Base(os.Executable())`).
Integration: `sysProxyHandler` via `httptest` + captured `logFn` — block→403, direct→200 (`direct`),
proxy+no tunnel→502. Manual: run the GUI on Windows, confirm `→ host:port - app.exe - [Nms, proxy]`.

## Migration / rollout
n/a — log format only; no persisted data. Revert = `git revert`.

## Work plan
| Slice | Delivers | Green when |
|---|---|---|
| 1 | format builders + policy tokens + delays; app passed as `—` | build/vet/test |
| 2 | Windows `lookupProcessByPort` + cache + UA fallback wired | + Windows test |
| 3 | system proxy routing + logging + README/UI text | + sysproxy tests |
| 4 | macOS `lsof` impl (flagged) + version bump | build/vet/test |

## Deviations
- Test files added beyond the plan: `syscall_windows_test.go` (`lookupProcessByPort` self-test,
  invalid port) and `syscall_darwin_test.go` (only `localPortOf`; full darwin lookup unverifiable).
- Review follow-ups: `tcpOwnerPID` retries once on `ERROR_INSUFFICIENT_BUFFER`; GET success is now
  logged right after `WriteHeader` (was after `io.Copy`, which measured body transfer, unlike
  CONNECT/SOCKS5); `logField` strips CR/LF from target/app/error so a process name or SOCKS5 host
  can't forge a logline.
- Negative-lookup results are NOT cached: each connection uses a fresh client port, so per-port
  negative caching would not help; the `—` line is logged directly.
- `newSysProxyHandler` builds its own direct pool rather than reusing `p.transportDirect` — keeps the
  two listeners' pool lifetimes independent (90 s idle each). Accepted.