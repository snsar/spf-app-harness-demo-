# docs/ — Guidance layer

The repo is the spec. This folder is the harness "Guidance" layer: durable design and
planning artifacts the agents read. English only.

| Folder | Holds | Who writes / reads |
|--------|-------|--------------------|
| `docs/planning/` | `plan.md`, `user-stories.md` (Given-When-Then AC), `complex-cases.md`, `questions.md` | `gpsr-orchestrator` writes; all agents read |
| `docs/specs/` | Per-feature design docs, data-model & API contracts, decisions | orchestrator / engineers |
| `docs/prototype/` | UI source of truth: design tokens + HTML/CSS prototype | `frontend-engineer` reads before building any screen |

## Not here
- **Intermediate agent artifacts** → `_workspace/` (`{phase}_{agent}_{artifact}.{ext}`),
  gitignored, kept locally for audit.
- **Harness config** → `.claude/` (agents, skills, settings, hooks) and `AGENTS.md`.

Keep these durable (committed). `_workspace/` is throwaway; `docs/` is the record.
