# RepoResident harness manual

RepoResident is a file-based operating harness for coding agents working in this repository. It
gives each session the current project state, a procedure for its task type, and only the project
knowledge relevant to that work.

## Design principles

1. **Context cost follows the task, not project age.** Hot state is capped and rewritten. Historical
   journals, closed issues, archived decisions, and shipped designs remain searchable without being
   loaded by default.
2. **Knowledge is layered.** Sessions load the operating manual, current state, one workflow, and
   then only the project files required for the task.
3. **Procedure carries working discipline.** Numbered steps, approval gates, templates, hard limits,
   and exit checks reduce ambiguity for every model tier.
4. **Completeness is explicit.** Feature designs account for empty input, invalid input, boundaries,
   concurrency, dependency failures, and partial failure. Builders attach verification evidence.
5. **The harness is maintained like code.** A maintenance workflow checks size budgets, verifies
   documentation against the repository, and promotes useful observations into tracked issues.

## Working roles

The roles define rules for each phase. They are not conversational personas.

- **Architect:** scopes and designs without writing implementation code.
- **Builder:** implements an approved design without changing its structure silently.
- **Fixer:** makes the smallest complete correction and avoids unrelated refactoring.
- **Reviewer:** reports verified findings and does not rewrite unless asked.
- **Curator:** maintains the harness and its bounded knowledge files.

Each handoff produces an artifact. Designs guide builders, diffs guide reviewers, and `STATE.md`
guides the next session.

## Using RepoResident

- **New project:** create a repository from the template and provide a project brief.
- **Existing project:** copy `CLAUDE.md`, `AGENTS.md`, and `.agent/`, then ask the agent to adopt it.
- **Daily work:** ask normally. The routing table in `CLAUDE.md` selects the workflow.
- **Long features:** work plans are split into session-sized slices that each leave the project
  verifiable.
- **Delegation:** tools that can spawn subagents may hand off exploration, review, and single
  design slices to keep the main context small. Rules live in `CLAUDE.md` § Delegation; subagents
  never write harness state.
- **Parallel work:** the default harness assumes one writer — delegation is sequential. Use the
  optional team-mode branch when several branches need shared state and integration rules.
- **Maintenance:** run the maintenance workflow when state requests it or a file exceeds its limit.

## File map

| File | Role | Loaded |
|---|---|---|
| `CLAUDE.md` | Operating rules, protocol, routing, and project facts | Every session |
| `AGENTS.md` | Entry point for tools using the `AGENTS.md` convention | Tool dependent |
| `.agent/STATE.md` | Current project state and inter-session handoff | Every session |
| `.agent/MAP.md` | Compact module and directory map | When locating code |
| `.agent/PROJECT.md` | Architecture, constraints, glossary, and project-wide risks | Feature work or confusion |
| `.agent/DECISIONS.md` | Binding technical choices with reasons | Before design work |
| `.agent/ISSUES.md` | Bounded backlog for bugs, debt, and deferred work | Before design or work selection |
| `.agent/workflows/*.md` | Procedures for task categories | One per task |
| `.agent/designs/` | Active and archived feature designs | Active design only |
| `.agent/areas/` | Optional deep documentation for complex modules | When working in that area |
| `.agent/journal/` | Append-only session outcomes | Written at session close |
| `.agent/scratch/` | Ignored temporary investigation notes | Only while active |

## Context layers

| Layer | Content |
|---|---|
| L0 | `CLAUDE.md`, the operating manual |
| L1 | `STATE.md`, the current project state |
| L2 | One workflow for the current request |
| L3 | Map, project facts, decisions, issues, area docs, and active design |
| L4 | Targeted source code |

The limits are intentional. Keep durable knowledge concise, move history to the journal, and trust
the routing process instead of loading every available file.
