# Workflow: Audit — read-only debt hunt over the product (and this harness). You are the Reviewer.
Run when: STATE `Next:` says `audit due`, or the user asks for a debt/overengineering audit.
This workflow only detects and files — it never fixes. Execution routes later like any request:
behavior-preserving restructure → refactor.md; removing routes/interfaces/dependencies →
feature.md with an explicit removal list, a caller grep proving disuse, and a user gate.

1. **Targets — hotspots only, never a tree sweep.** From the last 2 months of journal: areas
   touched by 3+ sessions or carrying repeat `flag:` lines, plus any area named in the STATE
   `audit due:` line. Code stable and untouched for many sessions is finished, not debt.
   `.agent/` is in scope too: a workflow or area doc nothing has routed to in ~20 sessions is
   itself a finding.
2. **Measure before judging.** PROJECT.md names dead-code/duplication/complexity tools → run
   them on the targets first; tool output outranks impressions. No tools → grep-level checks:
   exports with zero callers, near-duplicate blocks, abstractions with a single implementor,
   config for capabilities that don't exist.
3. **Findings — cap 5, evidence mandatory.** Each finding: `path:line` + mechanical proof
   (duplication count, zero-caller grep, metric vs a PROJECT.md threshold) + a named cost
   (which session it slowed — journal ref — or which open ISSUE / STATE `Next:` item it blocks).
   No proof or no payer → not a finding. Zero findings is a valid, expected outcome — never
   invent one to seem useful.
4. **Recheck `(assumed)` decisions.** Scan DECISIONS.md for `(assumed)` lines (the oneshot
   ledger): still consistent with what the code now knows? A wrong one is a finding.
5. **Coverage gaps are findings.** A hotspot whose behavior lacks tests blocks every future
   cleanup there (refactor.md demands a green safety net first) — file it like any other debt.
6. **File & close.** One ISSUES.md line per finding (P2/P3); a cleanup bigger than ~1 session
   also gets a design stub in `.agent/designs/`. Journal: `S<n> audit: <k> findings, cleared:
   <areas>` — the cleared list stops future audits from re-litigating the same code. Then
   session END as normal.

Done — tick in your final message:
- [ ] Targets chosen from journal hotspots (listed), not a sweep
- [ ] Every finding has path:line + mechanical proof + a named cost; ≤5, filed in ISSUES.md
- [ ] `(assumed)` decisions rechecked; cleared areas journaled
- [ ] No code or harness state beyond ISSUES/journal/STATE changed
