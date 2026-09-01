# Workflow: Review
For: reviewing a diff / PR / branch. Best run with fresh context — a session that didn't write the
code. Output: verified findings, not rewrites (unless the user asks you to fix).

1. **Context** — read the stated intent (PR description, design doc, or task). Then the diff.
   Then, per changed hunk, just enough surrounding code to judge it: callers, types, tests. Nothing else.
2. **Hunt**, in order of value:
   a. Correctness: broken edge cases (empty/boundary/error/concurrent), contract violations,
      off-by-one, leaks, races
   b. Completeness vs intent: does the diff do ALL it claims? any silently narrowed scope?
   c. Security on touched boundaries: injection, authz gaps, secrets, unsafe deserialization
   d. Tests: do they assert behavior (not implementation)? could they ever fail?
   e. Consistency: naming/pattern drift from neighbors, needless complexity, dead code
3. **Verify every finding** by reading the actual code path — no speculative findings. Rank:
   `[blocker]` / `[should]` / `[nit]`.
4. **Report** — per finding: `path:line`, what breaks, a concrete failing scenario, suggested
   direction (1 line). Blockers first. If clean: say clean, and list what you checked.
   Findings the user defers (or can't act on now) → one ISSUES.md line each, so they survive.

Done — tick in your final message:
- [ ] Every changed file examined
- [ ] Every finding verified against code, ranked, located
- [ ] No unrequested rewrites
- [ ] (Only if you changed anything) journal + STATE written; otherwise skip session END
