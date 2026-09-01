# 001: Wake-aware auto-restart (sleep recovery instead of hard stop)
Status: shipped
Date: 2026-09-01 · Session: S2

## Problem
After the PC wakes from sleep (or the relay dies from a network flap) the tunnel
hard-stops instead of recovering. TURN allocations die during sleep (10s refresh),
and the app-side WG netstack is built once per engine process and never rebuilt, so
proxies keep failing even if workers reconnect. The circuit breaker calls
`TunnelStop()` (app.go:1261) for both transient and fatal conditions. Users must
manually restart.

## Constraints
- `go_client/` & `server_src/` are auto-synced upstream — MUST NOT be edited (D3); only app-side (`app.go`, new `wake.go`).
- State stays local-only (D1) — no new git-tracked state.
- No new dependencies (Wails v2 app, vanilla frontend).
- Wake detection must work on Windows and macOS (both are targets; tray exists on both).
- Preserve user pause intent; never restart while paused or stopped.
- Bounded retries: no infinite hammering when the network is genuinely down (user chose ~8).
- Keep the hard stop for fatal errors (wrong password / WRAP mismatch).

## Current state
- `checkCircuitBreaker` (app.go:1210) increments counters per log line; at thresholds it calls
  `go a.handleCritical(...)` (1261) which logs + `TunnelStop()`.
- `handleCritical` cases: flood≥5, mismatch≥5, refused/timeout≥400, wrap-timeout≥3 (hard).
- `restartTunnel` (app.go:1344) kills the proc, waits, clears state, calls `TunnelStart`
  (fresh process → fresh `[КОНФИГ]` → `StartWGTunnel` rebuilds the WG netstack). Already used
  by `watchdog` (process-crash, zombie>90s).
- `restartAttempts` (app.go:122) counts consecutive restarts, reset to 0 on worker activity
  (`активных: N>0` at 1254 and watchdog at 1323). This is exactly a "consecutive failures" budget.
- `lastActiveAt` (unix ms) tracks last worker activity; `lastCBReset` resets breaker counters/60s.
- `startup` (app.go:210) starts `watchMinimize` (262); `shutdown` (app.go:428) closes stop chans.
- `App` already has a `tunnelMu sync.Mutex` guarding all tunnel fields.
- No power/sleep/network-change handling exists anywhere (verified S1: zero grep hits).

## Options
1. **Wake detection: polling wall clock vs native `WM_POWERBROADCAST`.**
   - A: poll `time.Now()` every 2s; jump > 15s ⇒ wake. ~15 lines, cross-platform, RTC ticks during
     sleep, NTP skew ≪ threshold. Misses sub-15s sleeps only.
   - B: native `RegisterPowerSettingNotification` via the tray message loop (tray_windows.go:254).
     Precise, catches short sleeps — but Windows-only, needs an HWND registration API, untestable here.
   - Pick **A now** (user: "polling only for now"), note B as follow-up.

2. **Restart vs stop on breaker.**
   - A: all four thresholds restart (simple, but wrong password would loop).
   - B: classify — flood/mismatch/refused ⇒ restart; wrap-timeout (password/WRAP) ⇒ keep hard stop.
   - Pick **B** (matches plan; keeps fatal handling intact).

3. **Concurrency/limits for restart.**
   - watchdog, wake, and breaker all restart ⇒ need single-flight + a cap.
   - Cap rule (user): stop after >8 consecutive failed restarts AND no recent worker activity.
     `restartAttempts>8` already implies no recent activity (activity resets it), so the cap is
     `attempts > maxRestartAttempts` guarded by staleness of `lastActiveAt`.
   - Pick single-flight `restartMu`+`restarting` flag, cap via pure `shouldHaltRestarts`.

## Design
Constants (new `wake.go`):
```go
const (
    wakePollInterval   = 2 * time.Second
    wakeThreshold      = 15 * time.Second
    maxRestartAttempts = 8
    restartStaleWindow = 2 * time.Minute
)
```
New App fields (app.go struct):
```go
wakeStop  chan struct{}
restartMu sync.Mutex
restarting bool
```

`wake.go`:
- `detectWake(prev, cur time.Time, threshold time.Duration) bool` — pure; true iff prev non-zero and `cur.Sub(prev) > threshold`.
- `shouldHaltRestarts(attempts, maxAttempts int, lastActiveAt, nowMs, staleMs int64) bool` — pure;
  true iff `attempts > maxAttempts` AND (`lastActiveAt == 0` OR `nowMs-lastActiveAt > staleMs`).
- `(a *App) startWakeMonitor(stop chan struct{}, onWake func())` — ticker every `wakePollInterval`;
  on jump > `wakeThreshold` calls `onWake()` once; returns on `close(stop)`.

`app.go`:
- `startup`: after tray init, `a.wakeStop = make(chan struct{}); go a.startWakeMonitor(a.wakeStop, a.onWake)`.
- `shutdown`: close `a.wakeStop`.
- `onWake()`: if `!tunnelRunning || tunnelPaused` → return. Else reset breaker counters + `restartAttempts=0`
  (fresh network deserves a fresh budget), `log("💤 ПК вышел из сна — перезапуск туннеля...")`,
  `time.Sleep(5s)` (let network settle), then `a.ensureRestart("ПК вышел из сна")`.
- `ensureRestart(reason string)`: single-flight guard on `restartMu`/`restarting` (defer clear);
  read `restartAttempts`+`lastActiveAt`; if `shouldHaltRestarts(...)` → `handleCritical("Много неудачных перезапусков — туннель остановлен. Проверьте сеть/пароль.")` and return;
  else `restartTunnel()`.
- `checkCircuitBreaker`: flood≥5, mismatch≥5, refused/timeout≥400 now call
  `go a.ensureRestart("<reason>")` instead of `handleCritical`; wrap-timeout≥3 stays `handleCritical` (hard).
- `watchdog`: replace both direct `a.restartTunnel()` calls with `a.ensureRestart(...)` so they share the
  single-flight + cap.

## Edge cases & failure modes
| # | Case | Expected behavior | Covered by |
|---|---|---|---|
| 1 | wake while tunnel stopped | no-op (guard `tunnelRunning`) | onWake guard |
| 2 | wake while paused | no-op (respect pause) | onWake guard |
| 3 | network truly down after wake | restarts ≤8, then hard stop with error | shouldHaltRestarts |
| 4 | transient flap that recovers | restarts, `restartAttempts` resets on activity, continues | activity reset |
| 5 | concurrent triggers (watchdog+wake+breaker) | single-flight collapses to one restart | restarting flag |
| 6 | wrong password / WRAP mismatch | stays hard stop (never retried) | wrap branch keeps handleCritical |
| 7 | NTP clock adjust ≪ threshold | no false wake (threshold 15s) | detectWake |
| 8 | `detectWake` first sample | prev zero ⇒ false | detectWake |
| 9 | restarts but workers never active | `lastActiveAt==0` ⇒ halt after budget | shouldHaltRestarts |
| 10 | `restartTunnel` panics | `restarting` cleared via defer | ensureRestart defer |

## Test plan
Unit (`wake_test.go`, no new deps):
- `detectWake`: prev==zero ⇒ false; jump==threshold ⇒ false (strict >); jump>threshold ⇒ true; negative/small jump ⇒ false.
- `shouldHaltRestarts`: attempts 0/8/9 vs max 8; lastActiveAt 0, fresh, stale; boundary off-by-one.
- `onWake` pause/stopped guards via App{...} with fields set directly (no ctx needed — return before any emit).
Repo green: `go test ./...` and `go vet ./...` after each slice.
Manual e2e: connect → sleep 60s → wake → expect "ПК вышел из сна — перезапуск туннеля" + tunnel recovered.

## Migration / rollout
n/a — no config/schema change; behavior only. Revert = remove `wakeStop` wiring + restore breaker branches.

## Work plan — slices ≤1 session, each leaving the repo green
| Slice | Delivers | Green when |
|---|---|---|
| 1 | `wake.go` (detectWake, shouldHaltRestarts, startWakeMonitor) + App fields + startup/shutdown wiring + `onWake` + unit tests | go test/vet green |
| 2 | breaker reclassification + `ensureRestart` single-flight/cap + watchdog routing + tests | go test/vet green |

## Deviations (Builder appends here during build)
- `ensureRestart` re-checks `tunnelRunning`/`tunnelPaused` before restarting — guards the shutdown/pause race
  during the wake settle sleep or while queued behind a single-flight.
- `restartTunnel` now closes the old `watchdogStop` channel (was: only nil it). Required because restarts can
  now originate outside the watchdog (wake/breaker); double-close is prevented by nil-ing under `tunnelMu`
  (only one of restartTunnel/finalizeTunnel captures a non-nil channel).
- Watchdog crash + zombie branches are now identity-checked (`a.tunnelProc == proc`) so a restart that happened
  during the crash-backoff sleep isn't duplicated.
- `wakeSettleDelay` is a package `var` (not const) so tests can shorten the settle sleep.
- Budget semantics: `restartAttempts` 1..8 proceed, the 9th consecutive attempt halts (`shouldHaltRestarts`
  uses strict `>`), matching the user's "~8 failed restarts then stop".
