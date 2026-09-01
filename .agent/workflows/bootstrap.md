# Workflow: Bootstrap — first session on a new or adopted project
Use when: CLAUDE.md "Project facts" still has `<fill>` placeholders, or STATE.md Session is 0.
Prompt also contains a full product brief to build now? Route to workflows/oneshot.md instead
(it runs §A steps 2–5 mid-flight).
Wrong facts written here poison every future session — verify, don't guess. Anything you can't
verify gets marked `(unverified)` or `(?)` rather than stated confidently.

## A · New project (empty repo)
1. Get the brief from the user (or their message): what it is, stack, key constraints, how they
   want to run/test it. Ask only for what's missing and truly undecidable.
2. Fill: CLAUDE.md Project facts (≤20 lines) · PROJECT.md (architecture intent, constraints,
   glossary) · first DECISIONS lines (stack and other founding choices, with whys).
3. Scaffold the project per stack conventions until build + test + lint all run green at
   hello-world level. Record the exact commands in CLAUDE.md only after running them.
4. Replace README.md with a real project readme. MAP.md: one line per created directory.
5. `git init` + initial commit if permitted.
6. STATE.md: Session 1, Focus = the project goal, Next = first real slice. Journal entry S1.

## B · Existing repo (adoption)
1. Read: README · manifests (package.json / pyproject.toml / go.mod / …) · top-2-level tree ·
   CI config. Do NOT crawl the source wholesale.
2. Fill CLAUDE.md facts from that evidence; run each command to verify it before recording it.
3. MAP.md from the tree: one line per top-level module; mark `(?)` where you inferred without
   reading. Later sessions (or maintain.md) verify lazily.
4. PROJECT.md: architecture from entry points + the imports of 3–5 key files only. Landmines:
   gotchas that will bite future sessions — top 5 max. Already-burning work items (failing
   tests, TODO bombs, deprecation warnings) → one ISSUES.md line each instead, top 5 by impact.
5. STATE / journal / DECISIONS initialized as in A.6 (record notable pre-existing choices you
   could infer, marked as observed, not decided). Ensure the repo's .gitignore covers
   `.agent/scratch/*` (keeping `.gitkeep`) — scratch notes must never be committed.

Done — tick in your final message:
- [ ] No `<fill>` placeholders remain anywhere
- [ ] Every recorded command was actually run (or is marked unverified)
- [ ] MAP covers all top-level directories
- [ ] STATE at Session 1 with a real Next; journal S1 entry written
