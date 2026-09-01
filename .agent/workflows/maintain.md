# Workflow: Maintain — the harness services itself. You are the Curator.
Run when: STATE says "maintenance due" (every 10th session), any budget below is blown, or the
user asks. (Fixing a wrong doc line you stumble on mid-task is NOT this workflow — that's prime
directive 5; just do it.)

## Budgets — hard caps
| File | Cap | When over |
|---|---|---|
| STATE.md | 40 lines | rewrite: keep Active/Next/Blocked, top-5 watch-outs, last-3 shipped |
| MAP.md | 120 lines | collapse subtrees into `.agent/areas/<x>.md`; keep one pointer line each |
| PROJECT.md | 80 lines | tighten prose; landmines >15 → merge, retire, or push into area docs |
| DECISIONS.md (active) | 50 lines | move superseded/expired lines to the Archive section |
| ISSUES.md (Open) | 40 lines | merge duplicates, close the stale, demote or drop P3s; Closed >100 → delete oldest lines (git keeps them) |
| journal/<month>.md | 200 lines | fine while current; months older than 2 → roll up into journal/ARCHIVE.md, ≤1 line per session, keep still-live `flag:` lines |
| areas/*.md | 60 lines each | split or prune; DELETE area docs describing deleted code |
| designs/ (root) | active docs only | shipped/superseded → designs/archive/ |

## Sweep — in order
1. **Budgets** — measure each file above (`wc -l`), fix violations per the table.
2. **MAP vs reality** — compare MAP against the actual tree (2 levels deep); fix drift; verify and
   remove any `(?)` marks.
3. **Staleness probes** — pick 2 area docs and 2 MAP lines at random; open the code they describe;
   fix lies. Found any? Probe 2 more of each.
4. **Flag & issue triage** — collect `flag:` lines from the last 2 months of journal: still true →
   one ISSUES.md line each (urgent ones also get a STATE watch-out); dead → drop during rollup.
   Then scan ISSUES.md Open: close lines already fixed (check git log), merge duplicates, re-rank.
   An area touched by 3+ sessions or repeat-flagged since the last audit → add
   `audit due: <area> → workflows/audit.md` to STATE `Next:` (detection only; the audit is its own session).
5. **Scratch** — delete `.agent/scratch/` files not referenced by STATE.
6. **Metrics ratchet** — one line in the sweep's journal entry: `metrics: <LOC> loc · <deps> deps ·
   <public routes/exports> surface · <tests> tests`. Compare with the previous sweep's line:
   surface growing much faster than shipped features → add `audit due` to STATE `Next:`.
7. **Report** (read-only): uncommitted drift, failing tests, anything smelling of rot you didn't fix.
8. **Journal the sweep** — what was compacted/fixed/triaged, ≤4 lines (metrics line included).

Done — tick in your final message:
- [ ] All budgets within cap (numbers shown)
- [ ] MAP spot-checks pass; no `(?)` remaining
- [ ] Flags triaged; scratch clean
- [ ] Sweep journaled; STATE rewritten
