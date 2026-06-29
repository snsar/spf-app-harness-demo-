# GPSR Compliance Engine — AGENTS.md

System of record for this repo. Give a map, not a manual: constrain, don't micromanage.
Everything the agent can't see in the repo does not exist. **English only**, always.

## What this is
A Shopify embedded app that automates **EU GPSR product-safety compliance**: it
classifies products in bulk and maps each to the correct responsible economic operator,
required warnings, and a storefront safety block — a rules engine, not a data-entry form.

## Stack (do not change name / db / port / script names)
- **frontend/** — React, embedded in Shopify Admin (App Bridge). (Renamed from `admin/`.)
- **backend/** — Go + Gin. Thin handler → service → repository.
- **storefront/** — Shopify theme app extension(s) under `storefront/extensions/`
  (storefront safety block). Each extension has its own `shopify.extension.toml`.
- **shopify.app.toml** — app config at **repo root**; Shopify CLI runs `app dev/deploy`
  from here. Full values (client_id, application_url, redirect_urls, webhooks) land in F9.
- **DB** — MySQL on **port 3308**. Schema changes via migrations only.
- Monorepo. Guidance lives in `docs/`: `docs/planning/` (plan.md, user-stories.md,
  complex-cases.md, questions.md), `docs/specs/` (per-feature design + API/data
  contracts), `docs/prototype/` (UI source of truth). See `docs/README.md`.

## How work runs — the harness
**Trigger:** for any GPSR build/implement/continue/fix/verify request, use the
`gpsr-orchestrator` skill. Simple questions can be answered directly.

The orchestrator runs the 6-step flow (Brainstorm → Plan → User Stories → Classify →
Execute → Verify) and gates every feature on evidence. Specialist agents live in
`.claude/agents/` and their procedures in `.claude/skills/` — the orchestrator wires
them; do not re-list them here.

Execution mode is **hybrid**: build = agent team (backend + frontend self-coordinate),
verify = sub-agents (QA + security, isolated to stay honest).

## Non-negotiable rules
1. **Design first → approve → code.** No production code before the design is approved.
   Always use plan mode for planning.
2. **TDD Iron Law.** No production code without a failing test first. Violation → delete
   and redo.
3. **DONE = EVIDENCE.** A feature is done only with real command output in
   `feature_list.json` (test pass, build, QA report). "It works" is not evidence.
   Separate the doer from the checker.
4. **One feature in-progress at a time** per DAG chain; respect dependencies in
   `.claude/feature_list.json`; never skip a dependency.
5. **Stop and ask, never guess** on ambiguity. Provide business logic; rely on known
   patterns for the rest.
6. **Schema only via migration.** Never edit live schema; never change db name / port
   3308 / script names.

## State files
- `.claude/feature_list.json` — features: `id · name · description · dependencies · status · evidence`.
- `progress.md` — running log of what's done / in-progress / blocked.
- `session-handoff.md` — clean handoff between sessions.
- `_workspace/` — intermediate artifacts (`{phase}_{agent}_{artifact}.{ext}`), preserved for audit.

## Feedback loop (highest-ROI investment)
`init.sh` runs the full verify chain from a clean checkout and must end **green
("HARNESS XANH")**. Script names in `init.sh` match `package.json` / Go commands 100%.
Leave a clean state every session. The harness is reviewed in PR like code; `init.sh`
passing is a merge gate. Harness debt = tech debt.

## Security (guardrail > prompt)
Rules here are advisory. Deterministic enforcement lives in `.claude/settings.json`
(deny → ask → allow; deny wins) and the PreToolUse hook. Never commit `.env` (only
`.env.example`). Treat storefront/merchant input as untrusted (injection, XSS). See the
`ai-security-review` skill.

## Harness changelog
> `CLAUDE.md` is a symlink to this file (cross-model: Codex/Cursor read AGENTS.md).

| Date | Change | Target | Reason |
|------|--------|--------|--------|
| 2026-06-28 | Initial harness | agents, skills, AGENTS.md, feature_list, init.sh, settings, hook | — |
| 2026-06-28 | Added `docs/` guidance layer (planning/specs/prototype) | docs/, AGENTS.md, gpsr-orchestrator | Plan docs had no home; aligns with slide's docs/ convention |
| 2026-06-28 | DAG revision: split F3→F3(auth)+F3b(API/sync), added F9(deploy: shopify.app.toml+nginx), backend port→8000 | feature_list.json, docs/planning/plan.md | User caught missing per-shop install/auth, nginx proxy, deploy config |
| 2026-06-29 | Repo restructure: `admin/`→`frontend/`; added `storefront/` (theme app extension dir) + root `shopify.app.toml` skeleton for Shopify CLI | folders, init.sh, package.json, plan.md, feature_list.json, agents/skills, shopify.app.toml | User wants `frontend/` naming + storefront extension pushed via Shopify CLI (app.toml at root, extensions under storefront/) |
