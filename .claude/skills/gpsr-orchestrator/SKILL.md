---
name: gpsr-orchestrator
description: Orchestrate the GPSR Compliance Engine build. Use whenever the user asks to build, implement, continue, re-run, update, fix, extend, or verify any part of the GPSR Shopify app — the product-classification rules engine, the React admin, the Go+Gin backend, the storefront safety block, or the verification/QA flow. Coordinates the backend, frontend, QA, and security agents through the 6-step delivery flow and gates every feature on evidence. Triggers on "build the GPSR app", "implement feature X", "continue the GPSR work", "re-run", "fix the compliance feature", "verify the build".
---

# GPSR Orchestrator

Lead the team that builds the GPSR Compliance Engine. You decompose work, assign it
to specialist agents, and gate every feature on evidence. You do not write app code
yourself.

## Phase 0: Context Check (run first, every time)
Decide the run mode before doing anything:
- `_workspace/` missing → **initial run** (start from Phase 1).
- `_workspace/` present + user asked for a targeted fix → **partial re-run**: re-invoke
  only the relevant agent; reuse other artifacts.
- `_workspace/` present + user gave new input/scope → **new run**: move `_workspace/`
  to `_workspace_prev/`, then start fresh.

Also read `AGENTS.md`, `.claude/feature_list.json`, and `progress.md` to load state.

## Phase 1: Brainstorm & Scope
Confirm WHAT and WHY with the user. Let agents propose approaches; you review. Stop and
ask on ambiguity — never guess. Provide business logic only; rely on known UX/eng
patterns for the rest.

## Phase 2: Plan
Produce `docs/planning/plan.md` (saved to repo, not only chat). Break the work into
features and record them in `.claude/feature_list.json` with a dependency DAG. Use plan
mode.

## Phase 3: User Stories
Write `docs/planning/user-stories.md`: each story is WHO + WHAT + WHY with acceptance
criteria in **Given-When-Then**. AC is the contract QA verifies against.

## Phase 4: Classify Tasks (3 buckets)
- **NOW** — clear, execute immediately.
- **COMPLEX** — write `docs/planning/complex-cases.md` (edge cases, hard logic, esp.
  GPSR classification corner cases).
- **DISCUSS** — write `docs/planning/questions.md`; stop and ask the user.

## Phase 5: Execute (hybrid)
**Build phase = agent team.** Form a team with backend-engineer + frontend-engineer.
They self-coordinate via SendMessage and the shared task list, cross-referencing API
response shapes against frontend hooks. One feature in-progress at a time per DAG
chain; respect dependencies, never skip one.

**Verify phase = sub-agents.** After each module completes:
1. Dispatch qa-integration (incremental QA — compare API shape vs FE hook, run the
   verification ladder, E2E via Playwright for high-risk features).
2. For high-risk features (storefront output, data integrity, any LLM call over
   merchant input), dispatch security-reviewer.

All Agent calls use `model: "opus"`.

## Phase 6: Verify & Gate
A feature reaches `done` ONLY when `feature_list.json` carries real evidence: raw test
output, build success, the QA report path, and (for high-risk) security sign-off plus
human approval. "It works" / "everything is fine" is never evidence.

## Data-Passing Protocol
| Strategy | Use |
|----------|-----|
| Task list (TaskCreate/Update) | progress, dependencies, work requests |
| Files in `_workspace/` | bulky/structured artifacts, audit trail — `{phase}_{agent}_{artifact}.{ext}` |
| SendMessage | real-time questions, shape changes, mismatch reports |

Only final artifacts land at real paths; keep `_workspace/` for audit. Final planning
docs live in `docs/planning/` (`plan.md`, `user-stories.md`, `complex-cases.md`,
`questions.md`); design/contracts in `docs/specs/`. All docs and code are **English only**.

## Error Handling
Retry a failed agent once. On second failure, proceed without it and record the gap in
`progress.md`. Never delete conflicting data — annotate both sources and surface to the
human. If a build agent patches blindly, deletes a failing test, or hides an error in a
fallback, stop it and require a root-cause fix (see `verification-ladder`).

## Test Scenarios
- **Happy path:** user says "build the bulk-classification feature" → Phase 0 (initial)
  → plan + story + DAG entry → backend+frontend team builds → qa-integration compares
  shapes + runs ladder → security review (storefront output) → evidence recorded → done.
- **Error path:** qa-integration finds the API returns `responsible_person` (snake) but
  the hook reads `responsiblePerson` (camel). Orchestrator routes the mismatch to the
  owning agent, does NOT mark done, re-verifies after the fix.
