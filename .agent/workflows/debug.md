# Workflow: Debug
For: a defect whose cause is unknown. The output of this workflow is a NAMED ROOT CAUSE with
evidence — the fix itself then follows patch.md steps 3–5 (or feature.md if the cause demands redesign).

Rules: never fix on hypothesis · reproduce before theorizing · change one variable at a time ·
every probe you add goes on a ledger and comes out at the end.

1. **Reproduce** — a command/script/test that shows the failure deterministically (or a measured
   flake rate). Can't reproduce? Gather evidence from logs/reports, file an ISSUES.md line holding
   your repro attempts + best hypothesis, report honestly — do not "fix" blind.
2. **Hypothesize** — read the failing path (MAP → grep → targeted reads). List hypotheses ranked
   by likelihood, each with a discriminating check: "if H1, then X will show Y".
3. **Discriminate** — run the checks: targeted logging, debugger, `git bisect`, input minimization.
   Keep the instrumentation ledger in `.agent/scratch/debug-<slug>.md` (survives session death;
   point STATE `Active:` at it on long hunts).
4. **Confirm** — state the root cause in 2 lines: mechanism + trigger. GATE: you must be able to
   flip the repro green/red by toggling the mechanism (a targeted probe), not just "it seems right".
   User present and the fix is non-trivial → post cause + intended fix (≤3 lines) before fixing;
   they may hold context that redirects it (CLAUDE.md § User sync).
5. **Fix** — patch.md steps 3–5. The reproduction from step 1 becomes the regression test.
6. **Clean & record** — remove every ledger item; delete the scratch file. Surprising or systemic
   cause → add a landmine line (PROJECT.md if cross-cutting, else the area doc). Journal: cause +
   fix, ≤4 lines.

Done — tick in your final message:
- [ ] Reproduction existed before the fix
- [ ] Root cause stated with discriminating evidence
- [ ] Regression test derived from the repro
- [ ] Instrumentation ledger empty; scratch file deleted
- [ ] Landmine recorded if surprising; journal + STATE written
