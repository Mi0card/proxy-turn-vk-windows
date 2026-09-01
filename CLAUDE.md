# Agent Operating Manual

You operate this repo through the harness in `.agent/` — persistent state and procedures that let
any session, on any model, pick up work with minimal context and leave the repo better than found.
First session on this repo? `.agent/STATE.md` will route you to bootstrap.

## Prime directives
1. The user outranks this file; this file outranks habit. When rules conflict:
   Hard rules > active workflow > style sections.
2. Correct and complete beats fast. No stubs, no placeholder code, no mock data outside tests,
   no silently narrowed scope. If you can't finish honestly, say exactly what's missing.
3. Smallest sufficient context: `.agent/MAP.md` → area doc if one exists → grep → read only
   implicated files. Never read directories wholesale. Lost after ~3 file reads? Re-scope from
   MAP or the area doc instead of reading more code.
4. Never call an API/function you haven't seen defined this session — read its definition first.
   Unsure how something behaves? Verify in source, not from memory.
5. Code is truth; docs serve code. A wrong harness doc is a bug — fix it in passing (≤5 lines)
   or leave a `flag:` in the journal.
6. Chat carries outcomes; files carry detail. Long analysis → `.agent/scratch/`,
   designs → `.agent/designs/`, lasting choices → `.agent/DECISIONS.md`,
   deferred bugs/work → `.agent/ISSUES.md`.

## Session protocol
START: read `.agent/STATE.md` → classify the request (routing below) → open that one workflow
file and follow it. Read only what the workflow tells you to read.
DURING: after each completed slice/checkpoint, update the `Active:` line in STATE.md (crash insurance).
END — mandatory whenever you changed anything:
1. Rewrite `.agent/STATE.md` in full (template is inside it; Session +1).
2. Append to `.agent/journal/<YYYY-MM>.md`: `S<n> <YYYY-MM-DD> <workflow>: outcome + key files`
   (≤4 lines; optionally `  flag: <debt/risk noticed>`).
3. Structure changed → update MAP.md. Lasting choice made → one line in DECISIONS.md.
4. Tick your workflow's Done checklist in your final message.
If the new Session number is a multiple of 10, add "maintenance due" to STATE `Next:`
(run `.agent/workflows/maintain.md` now if the session has room).

## Routing
| Request looks like | Route |
|---|---|
| Whole project from one brief; no product code yet | `.agent/workflows/oneshot.md` |
| New capability; or touches >2 files, or any interface/schema/dependency | `.agent/workflows/feature.md` |
| Small fix or tweak, cause known | `.agent/workflows/patch.md` |
| Defect, cause unknown | `.agent/workflows/debug.md` |
| Restructure with zero behavior change | `.agent/workflows/refactor.md` |
| Review a diff / PR | `.agent/workflows/review.md` |
| Harness upkeep / maintenance due | `.agent/workflows/maintain.md` |
| Debt/overengineering audit — STATE says `audit due`, or user asks | `.agent/workflows/audit.md` (read-only: files findings, never fixes) |
| Bug/task to record for later, not fix now | One line in `.agent/ISSUES.md` (format in its header). |
| Question or read-only analysis | Answer from MAP/PROJECT.md + targeted reads. Change nothing. Skip session END. |

Each workflow states an exit test; when on the fence, start with the lighter workflow.

## Delegation  <!-- only if your tool can spawn subagents; otherwise ignore this section -->
Subagents keep this context small on large tasks. Delegate only work whose spec already lives in
files: read-only exploration (returns conclusions, never file contents), `review.md` on a diff
this session wrote, or one work-plan slice of an approved design.
- Briefs are self-contained: goal, exact files/design sections to read, what to return. A
  subagent inherits no chat context — if it would need to ask questions, don't delegate yet.
- Subagents never touch harness state (STATE/journal/DECISIONS/ISSUES/MAP, session END); they
  change only code, tests, and scratch, then report. You verify (run the tests yourself) and
  you record — a subagent's "done" is a claim, not evidence.
- Sequential, one at a time: the harness assumes a single writer.

## User sync — keep the user in the decision loop
Building the wrong thing correctly is the costliest failure. When the user can respond (attended):
- Before designing non-trivial work, post the restated goal + out-of-scope list and fold in the
  user's corrections first. Design/plan gates default to user approval — self-approval is the
  unattended fallback, never a shortcut past a reachable user.
- A discovery that changes scope, cost, or approach → surface when found, with a recommendation.
  Taste-heavy calls (naming, UX, product behavior) go to the user when asking is cheap.
- Each checkpoint: 1–2 line note (done / next / surprises) so the user can redirect early.
- Task close lists every judgment call made on the user's behalf ("Decided for you: …") for veto.
Unattended (oneshot, CI): question budget spent up front; the `(assumed)` ledger is the audit trail.

## Output contract
- Lead with the outcome. Progress notes ≤2 sentences; never narrate tool calls or echo file contents.
- ≤10 lines of code in chat unless asked; reference `path:line` instead.
- Task close: Did / Changed (files) / Verified (how, with actual output) / Decided for you / Next —
  ≤8 lines, plus the Done checklist.
- Ask the user only what only the user can answer; otherwise decide, record it, proceed — and
  surface it in the task-close "Decided for you" list (§ User sync).
- Report failures verbatim (real test output, real errors) — never summarized optimism.

## Code standard
- Write for the reader: descriptive names, small functions, early returns, obvious control flow.
  Spend complexity on the design (edge cases, failure modes) — never on clever code.
- Every boundary you touch handles: empty/null, invalid input, dependency failure/timeout.
  "Happy path only" is incomplete work.
- Every behavior change ships with a test that fails without it.
- Comments only for constraints and whys the code can't express. No dead or commented-out code.
- Match the file's existing local style when it conflicts with this section.

## Hard rules
- Never: commit secrets · force-push · delete/overwrite content you haven't read · edit journal
  history or archives.
- New dependency ⇒ a DECISIONS.md line justifying it.
- Debug instrumentation is removed before done (keep a ledger while it exists).
- Data-touching changes (migrations, deletions) need a written revert path in their design.

## Project facts  <!-- filled by bootstrap; keep ≤20 lines -->
- What: Windows/macOS GUI client for a WireGuard-over-VK-TURN tunnel: SOCKS5/HTTP/system proxies,
  rule-based routing, SSH VPS deploy, manual VK captcha in a native window. Educational project.
- Stack: Go 1.23.1 root + Wails v2 (GUI) + vanilla JS/HTML/CSS; wireguard userspace + gvisor
  netstack. Engine modules `go_client/` (go 1.26) & `server_src/` (go 1.25, Linux-only) are
  auto-synced from `amurcanov/proxy-turn-vk-android` — never edit them locally.
- Commands — build: `.\build.ps1` (needs Wails CLI + MSYS2 GCC; unverified locally) ·
  compile/test root: `go build ./...`, `go test ./...` · lint: `go vet ./...` (no linter; gofmt)
  · engine: `cd go_client && go build ./...`, `cd server_src && GOOS=linux GOARCH=amd64 go build ./...`
  (server_src only compiles for linux/amd64; its tests only run on Linux)
- Entry points: `main.go` (wails.Run + go:embed) · `app.go` (backend, version const) ·
  `go_client/main.go` · `server_src/main.go`
- Deeper facts: `.agent/PROJECT.md` (architecture, constraints, glossary, landmines)
