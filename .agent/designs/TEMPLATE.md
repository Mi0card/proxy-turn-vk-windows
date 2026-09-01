# <NNN>: <title>
Status: draft | approved (user) | approved (self — user unreachable) | building | shipped | superseded by <NNN>
Date: <YYYY-MM-DD> · Session: S<n>

## Problem
<what's broken or missing, for whom, why now — ≤5 lines>

## Constraints
<hard requirements pulled from PROJECT.md + DECISIONS.md, plus new ones: perf, compat, security>

## Current state
<how it works today: the involved files with roles, and the flow being changed.
Facts from reading code this session — never from memory.>

## Options
<for each non-obvious choice: option A / B (/C) with one tradeoff line each, then the pick and why.
Depth scales with blast radius — interface, schema, and dependency choices get real analysis.
Prefer the complete, correct design over the quick one; complexity is acceptable when this
section justifies it. Each pick becomes a DECISIONS.md line at close.>

## Design
<the chosen shape, precise enough that a session with no other context could build any slice:
data model · interfaces/signatures · control flow · state & lifecycle · concurrency>

## Edge cases & failure modes — the completeness contract; Builder ticks every row
| # | Case | Expected behavior | Covered by |
|---|---|---|---|
| 1 | empty / none / zero input | | [ ] |
| 2 | invalid / malformed input | | [ ] |
| 3 | boundaries (0, 1, max, off-by-one) | | [ ] |
| 4 | concurrent or repeated invocation | | [ ] |
| 5 | dependency fails / times out | | [ ] |
| 6 | partial failure midway (crash between steps) | | [ ] |
| 7 | <domain-specific — add rows; striking a row requires a written why> | | [ ] |

## Test plan
<what proves it: unit / integration / e2e split, plus the one manual check if any>

## Migration / rollout
<data migration, compat window, flagging, and the revert path — or "n/a: <why>">

## Work plan — slices ≤1 session, each leaving the repo green
| Slice | Delivers | Green when |
|---|---|---|
| 1 | | |

## Deviations (Builder appends here during build)

---
Self-approval checklist (only when the user is unreachable):
- [ ] Every constraint traceable into the Design section
- [ ] Every edge-case row has an expected behavior (or a written strike reason)
- [ ] Interfaces fully named and typed — no hand-waving
- [ ] Slices each ≤1 session and independently green
- [ ] No section reads "TBD"
