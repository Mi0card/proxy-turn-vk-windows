# Workflow: One-shot — an entire project from a single prompt, zero further user input
Use when: no product code exists yet (fresh harness copy) AND the request is a complete product
brief. Exit test — brief asks for one capability on an existing codebase? That's feature.md.
"One shot" means one prompt, not one context window: the checkpoints below make crash, context
loss, or session death resumable without re-prompting. A mid-run question is a failed run.

## Resume — a session finding STATE `Active: oneshot …` starts HERE
Open `.agent/designs/001-genesis.md` → find the first unticked work-plan slice → confirm the
repo is green (build + tests) → continue at phase 3. Trust ticks, commits, and the design —
never memory of a previous session.

## 0 · Persist the brief — before anything else; the prompt is the spec
1. Copy the user's prompt VERBATIM into `.agent/designs/001-genesis.md` § Problem (file from
   designs/TEMPLATE.md). On disk it survives crash and context loss; in chat it doesn't.
2. Add an `## Acceptance` section: table `| # | Criterion (brief's words) | Evidence |` of
   testable criteria derived from the brief. Brief already states criteria or a rubric? Copy
   those verbatim as the rows. Phase 4 walks this table against the brief's wording.

## 1 · Question budget — spent here or never
User reachable → ONE batched message with every truly undecidable question, now — and include
the `## Acceptance` table in it: corrections to those rows are the one cheap sync point this
workflow gets before the close. Unattended
run (benchmark/CI/no reply) → zero questions. Either way, every remaining gap: decide, write
a DECISIONS.md line tagged `(assumed)`, proceed. That ledger is how the run is audited later.

## 2 · Foundation + genesis design — Architect
1. Bootstrap: run bootstrap.md §A steps 2–5 (facts, hello-world green, README, MAP, git init).
2. Complete 001-genesis.md per TEMPLATE: full MVP design, every section, edge-case table filled.
3. Draw the MVP line explicitly: each cut or deferred capability = one ISSUES.md line. Silent
   scope narrowing is the classic one-shot failure; the ledger is the fix.
4. Work plan: slices ordered so the app RUNS as early as possible; each slice green on its own.
5. Self-approval checklist (TEMPLATE bottom) → `Status: approved (self)`. No user gate exists.

## 3 · Slice loop — Builder; the design is the spec
Per slice, run feature.md §3 exactly. At each slice's green checkpoint: tick the slice in the
work plan → STATE `Active: oneshot 001 slice <k>/<N>` → commit `S1 genesis slice <k>: <summary>`.
The commit is the resume anchor — never batch several slices into one commit.
Subagents available → delegate each slice (CLAUDE.md § Delegation): brief = design path + slice
row; it builds and tests, you rerun the suite, tick, commit. Your context then carries the plan
and verification, not implementation detail — that is how one shot survives a large brief.

## 4 · Verify against the brief, not the design — Reviewer
1. Run feature.md §4 (full suite, edge-table walk, end-to-end as a user) and §5 (hostile review).
2. Walk `## Acceptance`: fill Evidence per row — a test name or pasted run output. Unmet row →
   fix now if ≤1 slice of work, else an ISSUES.md line + an honest ✗. Never hide a miss.

## 5 · Close — session END in full
STATE.md rewritten (Session 1; `Next:` = top open ISSUES) · journal S1 entry · MAP current ·
design `Status: shipped`. Final message: Acceptance table with evidence, count of `(assumed)`
decisions, Done checklist. From session 2 the repo is a normal harnessed project.

## Stopping rule
Blocked by something only the user can provide (credentials, paid service, legal choice)?
Write STATE `Blocked:` → finish every slice not depending on it → report at close. Never stall.

Done — tick in your final message:
- [ ] Brief verbatim in the design; every Acceptance row has evidence or an honest ✗ + why
- [ ] Every slice green and committed at its own checkpoint; full tests/lint/build green at close
- [ ] Zero mid-run questions; every assumption a DECISIONS `(assumed)` line; every cut in ISSUES
- [ ] Session END complete; STATE routes session 2 cleanly
