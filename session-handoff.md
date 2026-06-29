# Session Handoff — GPSR Compliance Engine

> Read this first when starting a session. Keep it short and current; update before you stop.

## Where things stand
The harness is bootstrapped (agents + skills + AGENTS.md + feature_list.json + init.sh +
security config). **No app code exists yet.** Next is F0 (scaffold + green `init.sh`).

## How to start work
1. Read `AGENTS.md` (system of record) and `.claude/feature_list.json`.
2. Trigger the `gpsr-orchestrator` skill for any build/implement/fix/verify request.
3. Pick the next `pending` feature whose dependencies are all `done`. Set it
   `in-progress` (only one at a time).

## Open decisions / context not in code
- Stack is fixed: React admin, Go+Gin backend, MySQL **port 3308**, monorepo.
- Scope so far is harness-only by request; app scaffold (F0) is the first build task.
- The "clever core" is the GPSR rules engine (F2) — keep it deterministic + auditable.

## Before you stop
- Update `progress.md` and this file.
- Leave a clean state; ensure `init.sh` still passes (or note why it doesn't).
