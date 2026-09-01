# Workflow: Refactor
For: structure improvement with ZERO behavior change. Any interface/schema change or behavior
tweak → feature.md. Found a bug mid-refactor → finish or park the refactor, fix via patch.md
separately; never bundle the two in one diff.

1. **Safety net** — tests covering the affected behavior are green right now. Uncovered? Write
   characterization tests FIRST: assert what the code currently does, even where that's ugly.
2. **Plan** — an ordered list of mechanical moves (rename / extract / inline / move / dedupe),
   each leaving the repo green. Post the list and wait for a go-ahead when the user can respond
   (CLAUDE.md § User sync); proceed on it directly only when unattended.
3. **Execute** — one move at a time: apply it everywhere (grep to enumerate every caller/import/
   config reference — never from recall), run tests, commit if permitted. Never bundle two moves.
4. **Verify** — full suite green with test logic unmodified (mechanical renames inside tests are
   the only allowed test edits). Had to change an assertion? You changed behavior → stop, revert
   that move, reroute to feature.md.
5. **Record** — MAP.md (paths moved!) · area docs that mention moved files · journal · STATE.

Done — tick in your final message:
- [ ] Safety-net tests existed (or were written) before any move
- [ ] Each move atomic and green; no assertion changes anywhere
- [ ] All references updated (grepped, not recalled)
- [ ] MAP + area docs current; journal + STATE written
