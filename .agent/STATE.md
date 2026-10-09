# State — the single source of "now". Rewritten IN FULL at every session END. Cap: 40 lines.
<!-- History belongs in journal/. Contradicts git log/journal? Trust git, note the crash there. -->

Session: 21
Focus: WinDTT — Wails GUI client for a WireGuard-over-VK-TURN tunnel (proxies, routing, VPS deploy)
Active: patch (UNCOMMITTED) — overheating with system proxy ON (CPU in msedgewebview2.exe). Cause =
        WebView2 software rendering + per-connection event/DOM churn, NOT the netstack. main.go: GPU
        on by default; app.go: socks:log batched (200ms/500) + socks:stats coalesced via
        proxyEventFlusher; frontend: onSocksLog batch + LOG_DOM_MAX 2000 / LOG_HISTORY_MAX 5000. GPU
        now a settings toggle (Config.DisableGpu *bool; main.go reads it pre-window; config > env
        WDTT_DISABLE_GPU > default on; restart to apply; card hidden off-Windows). app_test.go: 3 tests.
        build/vet/test green; node --check ok. v0.3.1.3 (user bump).
Next: user rebuilds + tests on the hot laptop (compare idle vs system-proxy-on, watch
      msedgewebview2.exe); new GPU checkbox enables A/B without env vars. Deferred on purpose (Decided
      for you): go_client `-mode socks` consolidation and the PID-lookup TCP-table snapshot cache both
      target wdtt.exe, not msedgewebview2.exe — do only if wdtt.exe CPU is also high. Pending from S20:
      commit+push .gitignore + sync.yml to dev/main.
Blocked: none

## Watch-outs (≤5 — things the next session must know; prune ruthlessly)
- GPU: WebviewGpuIsDisabled was true since v0.2.9.5 (no rationale recorded); now false by default.
  If a black/flicker window appears on some GPU, WDTT_DISABLE_GPU=1 restores software rendering.
- macOS `lookupProcessByPort` (lsof field mode) is NOT build/run verified — no Mac on this box
  (darwinkit/cgo). Same unverified-on-Mac class as I13. Re-check both on a real Mac.
- WinINET system proxy follows the global «Трафик по умолчанию» (D16) like SOCKS5/HTTP — a user-set
  `direct`/`block` applies to it too (was unconditional strict tunnel). Stock default = proxy.
- Engine = upstream mirror: go_client layer in `.agent/local/go-client-local/` (patch + apply.sh,
  GOOS=windows fixes only, fail-loud on drift — D13/D14); upstream IGNORES go.sum and server/ has no
  go.mod, so sync.yml seeds server_src/go.mod+go.sum from upstream root + `go mod tidy`s both (D19).
  server_src linux/amd64 only; slow downloads → GOSUMDB=off. build.ps1 needs Wails+GCC; `-race` off.
- Config top-level `fingerprint` = SSH host-key of VPS (deploy MITM guard) — NOT browser fingerprint;
  SaveConfig keeps it when empty (app.go). Do not repurpose that key.

## Recently shipped (≤3 one-liners; anything older lives in the journal)
- S21: overheat fix — GPU default on + proxy event coalescing/batching, plus a GPU settings toggle (Config.DisableGpu; see Active). UNCOMMITTED.
- S20: CI sync unblocked — .gitignore + sync.yml remove/ignore `go_client/wg-turn-client.exe` (was aborting create-pull-request's `git checkout dev`). UNCOMMITTED.
- S19: CI sync fix — server_src go.mod/go.sum restored from upstream root + `go mod tidy` both modules; Node24; staged-diff gate. COMMITTED a333d6c.

## Recently audited (cleared — stop re-litigating)
- S14 audit cleared: proxy transport pool (I6), system-proxy backup tests (I5), renderRuleSuggest XSS
  (I4), ruleset map concurrency, protobuf parsers, local go_client layer.
- S1/S2/S5 (uploadData cat> injection, deploy shellQuote) judged NON-issues; backend startup/config
  schema/build pipeline: no debt found.
