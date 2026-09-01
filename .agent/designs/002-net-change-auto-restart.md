# 002: Network-change auto-restart (interface poller)
Status: shipped
Date: 2026-09-01 · Session: S3

## Problem
After a network change the tunnel can silently degrade with no recovery. The S2 breaker
covers public-IP changes (`ip mismatch` ≥5) and connectivity loss (`refused/timeout` ≥400),
but silent blips — same-IP roaming, adapter swap, USB-C dock, WiFi→Ethernet with no IP
change — trip no threshold and leave a stale WG session. A full engine restart is the only
recovery (config is sent once per process; app-side netstack is built once per process).

## Constraints
- App-side only (D3): `go_client/` & `server_src/` never edited.
- No new deps (D2): stdlib `net` only, Wails v2 app.
- Works on Windows and macOS (both are targets); tray exists on both.
- Reuse `ensureRestart` (001): single-flight + bounded budget + pause/stop guards.
- Cooldown (user): 30-60s — no restart storms during flapping/docking.
- Restart budget: NOT reset on network change (unlike wake) — flapping must burn the budget.
- Deterministic signature: OS slice order variance must not cause false triggers.

## Current state
- `startWakeMonitor`/`onWake`/`ensureRestart` (wake.go) provide the restart primitive; `App` has
  `wakeStop`, `restartMu`, `restarting`, `tunnelMu` guarding all tunnel fields.
- `startup` (app.go:271) starts the wake monitor; `shutdown` (app.go:442) closes it.
- `ensureRestart` (wake.go:101) re-checks `tunnelRunning`/`tunnelPaused` before restarting.
- No interface/network-change detection exists (S2 grep: zero hits).

## Options
1. **Detection mechanism.**
   - A: poll `net.Interfaces()` every 5s, compare signatures. Stdlib, cross-platform, ~40 lines.
   - B: platform-native: Windows `NotifyIpInterfaceChange` (iphlpapi) / macOS `SCDynamicStore`.
     Immediate + precise — but Windows-only CGO-free syscalls + macOS API, untestable here, 2 files.
   - Pick **A** (user), consistent with wake polling; native hooks are follow-ups.

2. **What counts as a change.**
   - A: IPs only — misses adapter swap with same IP.
   - B: interface up/down OR IP add/remove — catches WiFi→Ethernet, VPN toggles; slightly more
     false positives (power-save flaps, absorbed by cooldown).
   - C: any state incl. name/flags — most sensitive, noisiest.
   - Pick **B** (user).

3. **Restart storm protection.**
   - A: cooldown `lastNetRestartAt` within 30s blocks new network-triggered restarts. single-flight
     + budget still apply on top.
   - B: no cooldown — flapping could burn the 8-attempt budget fast.
   - Pick **A** (user), `netCooldown = 30s`.

## Design
Constants (netwatch.go):
```go
const (
    netPollInterval = 5 * time.Second
    netCooldown     = 30 * time.Second
)
```
Injectable fns (netwatch.go, package vars — override in tests):
```go
var (
    netInterfacesFn = net.Interfaces
    netAddrsFn      = func(i net.Interface) ([]net.Addr, error) { return i.Addrs() }
)
```
New App fields (app.go struct, under `tunnelMu`):
```go
netStop         chan struct{}
lastNetRestartAt time.Time
```

`netSignature() (string, error)` — pure:
- `net.Interfaces()` error → return error (caller skips tick).
- For each non-loopback interface: collect `fmt.Sprintf("%s|%t|%t|%v", name, up, running, sortedAddrs)`
  where `sortedAddrs` = sorted IPv4/IPv6 strings from `netAddrsFn` (skip interface on addr error).
- Return sorted join of all entries (deterministic regardless of OS slice order).

`startNetMonitor(stop chan struct{})`:
- 5s ticker; first sample seeds prev (no trigger on startup — matches `detectWake` first-sample rule).
- Signature error → log once, skip tick (next tick still detects a real change).
- On `prev != cur` → `a.onNetworkChange()`; update prev.

`networkChangeAllowed() bool` (all under `tunnelMu`):
- false if `!tunnelRunning || tunnelPaused`.
- false if `!lastNetRestartAt.IsZero() && time.Since(lastNetRestartAt) < netCooldown`.
- else true.

`onNetworkChange()`:
- `if !a.networkChangeAllowed() { return }`.
- `a.log("🌐 Сеть изменилась — перезапуск туннеля...", "warn")`.
- `a.ensureRestart("Сеть изменилась")`.

Notes:
- Does NOT reset `restartAttempts` (unlike wake). Worker activity resets it (existing mechanism);
  `TunnelStart` already resets breaker counters. A genuinely flapping network burns the budget → hard stop.
- `ensureRestart` re-checks running/paused + single-flight, so the cooldown here is purely anti-storm.

## Edge cases & failure modes
| # | Case | Expected behavior | Covered by |
|---|---|---|---|
| 1 | startup first sample | no trigger (prev seeded) | startNetMonitor seed |
| 2 | interface down (WiFi off) | signature change → restart (cooldown-guarded) | netSignature + onNetworkChange |
| 3 | same-IP adapter swap | up/down changes → restart | signature includes up+running |
| 4 | loopback/tunnel ifc churn | filtered out (skip loopback) | netSignature |
| 5 | `net.Interfaces()` error | skip tick, no crash | startNetMonitor error path |
| 6 | one interface Addrs() error | skip that ifc, still hash rest | netSignature per-ifc skip |
| 7 | OS slice order change | sorted signature → no false trigger | sorted join |
| 8 | flapping network | cooldown 30s between net restarts | networkChangeAllowed |
| 9 | paused/stopped during change | no restart (guard) | networkChangeAllowed |
| 10 | worker reconnects anyway | restart still safe (idempotent, bounded) | ensureRestart single-flight |

## Test plan
Unit (netwatch_test.go, no sleeps):
- `netSignature`: unchanged ifaces ⇒ same sig; up→down ⇒ changed; IP add/remove ⇒ changed;
  loopback-only change ⇒ unchanged; one bad-addr ifc ⇒ skipped (no error); deterministic
  given reordered interface slice.
- `networkChangeAllowed`: stopped ⇒ false; paused ⇒ false; cooldown boundary — exactly 30s ⇒ true,
  29s ⇒ false; after allowed trigger `lastNetRestartAt` set.
Repo green: `go test ./...` + `go vet ./...`.
Manual e2e: toggle WiFi off→on with tunnel running ⇒ log "🌐 Сеть изменилась..." + tunnel recovers.

## Migration / rollout
n/a — no config/schema change; behavior only. Revert = remove netStop wiring + restore breaker-only behavior.

## Work plan — slices ≤1 session, each leaving the repo green
| Slice | Delivers | Green when |
|---|---|---|
| 1 | netwatch.go (signature/monitor/gate/onNetworkChange) + App fields + wiring + tests | go test/vet green |

## Deviations (Builder appends here during build)
- `netInterfacesFn`/`netAddrsFn` are package vars (injectable) so signature tests don't touch real interfaces.
- `a.log` (app.go) now returns early when `a.ctx == nil` — defensive guard that lets `onNetworkChange`/
  `ensureRestart` paths be exercised in unit tests without a Wails context; no production behavior change
  (ctx is always set in startup).
- Cooldown uses `>=` semantics: a net restart exactly `netCooldown` ago is allowed (`time.Since < netCooldown`
  blocks, equality passes), matching the off-by-one contract in the edge-case table.