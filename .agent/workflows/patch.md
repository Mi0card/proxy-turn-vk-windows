# Workflow: Patch
For: small fix or tweak — cause known or trivially findable, no interface change, ≤2 files expected.
Escalate to feature.md the moment an interface/schema must change, the fix wants >2 files, or you
are redesigning anything. Escalate to debug.md the moment you've read ~5 files and still can't
name the cause with evidence.

You are the Fixer: minimal diff, zero drive-by improvements. Cleanup urges become one journal
`flag:` line, not edits.

1. **Scope** — one line: defect + expected behavior. One line: why it happens
   (verified by reading the code, not assumed). Your reading differs from what the user
   described or asked for? Say so before editing — never quietly fix a different bug.
2. **Locate** — MAP.md → grep the symbols → read only the implicated file(s) plus the contracts
   they must honor (immediate caller, type/interface, existing test). Not the whole area.
3. **Fix** — the smallest change that is *fully correct*, not the smallest that works: cover the
   error/empty/boundary paths the defect implies. Match local style exactly, even if you dislike it.
4. **Verify** — run the narrowest test covering the change. If this was a bug, add the regression
   test that fails without your fix. Then the project test command if it's reasonably fast.
5. **Record** — journal entry (≤3 lines). Rewrite STATE.md (usually just Session +1 and
   Recently-shipped). MAP/DECISIONS almost never change here — if they need to, ask yourself
   whether this was really a patch.

Done — tick in your final message:
- [ ] Cause named with evidence
- [ ] Regression test exists and fails without the fix (for bugs)
- [ ] Tests green (output shown)
- [ ] Diff contains nothing but the fix
- [ ] Journal + STATE written
