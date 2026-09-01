# Workflow: Feature
For: new capability, or any change touching >2 files or any interface/schema/dependency.
Exit test — if ALL true, downgrade to patch.md: single known site, no interface change, obvious verification.

Phases have different rules; do not blend them. The design doc is the handover from Architect to
Builder — write it so a session with zero other context could build from it (one might).

## 1 · Scope — you are the Architect
1. Restate the goal in ≤3 lines, plus an explicit out-of-scope list. User present → post both
   and fold in corrections before designing (CLAUDE.md § User sync): a wrong restatement is
   cheapest to fix here, before any design or code exists.
2. Read: STATE watch-outs · PROJECT.md · scan DECISIONS.md + ISSUES.md Open (known issues in the
   target area shape the design) · MAP.md → area docs for the target modules → targeted reads of
   files you'll touch. List those files with one-line roles (feeds the design's Current state).
   Exploration wider than ~3 reads → delegate it to a read-only subagent (CLAUDE.md § Delegation):
   it returns the file list, roles, and flow summary; you read only what you'll change.
3. GATE: request conflicts with a Decision or a PROJECT constraint? Surface it to the user now —
   never design around it silently.

## 2 · Design — Architect
1. Copy `.agent/designs/TEMPLATE.md` → `.agent/designs/<NNN>-<slug>.md`. Fill every section;
   write "n/a — <why>" rather than deleting one. No code in this phase.
2. Each non-obvious choice: ≥2 options, one tradeoff line each, then the pick and why. Prefer the
   complete, correct design over the quick one — complexity is acceptable when the Options
   section justifies it.
   High-stakes choice (schema, dependency, public interface)? Optionally delegate option
   generation to a blind subagent: brief = problem statement, PROJECT.md constraints, and the
   request's own wording — assembled from those sources, never paraphrased from your draft, and
   withholding the current design, DECISIONS.md, and your leaning. To stress-test *existing*
   structure instead, brief an adversarial pass: full code access, mandate to argue for the
   simpler design. Either way the subagent proposes; you decide with full context.
3. The edge-case table is the completeness contract: every empty/boundary/failure/concurrency
   row filled in, or explicitly struck with a reason.
4. Work plan: slices of ≤1 session, each leaving the repo green (builds, tests pass).
5. GATE — approval: default is USER approval. Post a ≤10-line summary — the pick per Options
   entry, any struck edge-case rows, slice count — wait, and fold corrections into the design
   before building. Only when the user is unreachable: run the self-approval checklist at the
   bottom of TEMPLATE.md, set `Status: approved (self)`, proceed, and flag that at close.

## 3 · Build — you are now the Builder; the design is the spec
Per slice, in order:
1. Re-read that slice and the design sections it touches. Implement it completely —
   no TODO, no stub, no "for now".
2. Write tests for the slice's behavior, covering its edge-case rows; tick each row in the
   design: `[x] covered: <test name>`.
3. Run narrow tests, then the project test command. Green → update STATE `Active:` line;
   commit if permitted (`S<n> <slug> slice <k>: <summary>`); post a 1-line progress note
   (slice k/N: what now works) so an attending user can redirect early.
4. Design wrong or insufficient? Cosmetic → note under `## Deviations`, continue. Structural →
   STOP, return to phase 2, update the design (re-gate if the user gated it).

## 4 · Verify
1. Full test suite + lint + build.
2. Walk the edge-case table: every row has evidence — a test name, or 2-line reasoning where genuinely untestable.
3. If the feature is runnable, exercise it end-to-end once as a user would; record what you observed.

## 5 · Review — you are now the Reviewer: hostile fresh eyes
(Best: have a subagent run `workflows/review.md` on the diff — clean context that didn't write it
(CLAUDE.md § Delegation). No subagents → suggest the user run it in a fresh session.)
Read the complete diff and hunt: unhandled error paths · dead code / debug leftovers ·
naming or pattern drift vs neighboring code · diff ≠ design (drift) · tests that could never fail.
Fix findings now; genuinely out-of-scope ones become ISSUES.md lines.

## 6 · Record & close
Delete pass first: what did this feature make obsolete (old routes, flags, helpers, docs)?
Remove trivially-dead leftovers in this diff; anything needing its own removal work → one
ISSUES.md line each. Never leave the old path in place silently.
Then: DECISIONS.md line per lasting choice · MAP.md if structure changed · area doc for a new
gnarly module (only if a future session would otherwise re-derive it) · design `Status: shipped`
(move to designs/archive/ when confident) · journal entry · STATE.md rewritten.

## Done — copy into your final message and tick honestly
- [ ] All design sections resolved; every edge-case row has evidence
- [ ] Full tests/lint/build green; e2e exercised if runnable (output shown)
- [ ] Diff free of TODOs, stubs, debug leftovers
- [ ] Review pass done; residual findings filed in ISSUES.md
- [ ] MAP/DECISIONS/area docs current; journal appended; STATE rewritten
- [ ] User synced: intent confirmed at scope, design user-approved (or self-approval flagged),
      "Decided for you" list in the close
