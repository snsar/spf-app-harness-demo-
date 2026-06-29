---
name: verification-ladder
description: Pick and run the right verification tier for a task by its type and risk, for the GPSR Compliance Engine. Use whenever deciding how to test/verify a change, what "done" requires, or whether E2E and human approval are needed. Triggers on "how should I test this", "what verification", "is this done", "do I need E2E", and on TDD/debug work like "write the failing test first", "verify the fix".
---

# Verification Ladder (GPSR)

Verification is not "I ran a unit test." Pick the tier by task type and risk. Higher
risk climbs higher and adds human approval.

## The Ladder (low → high)
| Tier | What it proves | Use when |
|------|----------------|----------|
| 1. Type / lint / build | code compiles, types align | every change, baseline |
| 2. Unit | a function/rule behaves | pure logic (rules engine, services) |
| 3. Integration | layers/boundaries agree | handler↔service↔repo, API↔hook |
| 4. E2E (Playwright) | the real flow works in a browser | user-facing flows, storefront block |
| 5. Visual / manual + human approval | looks right, judgment call | High-risk features |

Climbing a tier does not skip lower ones — E2E assumes build + unit already pass.

## Risk → Required Tier (GPSR-specific)
- **High risk** = compliance data correctness, storefront safety-block output, data
  integrity, anything money/auth, any LLM call over merchant input.
  → Tiers 1–4 **plus** human approval before "done". E2E is mandatory.
- **Medium risk** = internal admin flows, non-critical config.
  → Tiers 1–3, E2E if user-facing.
- **Low risk** = copy, styling tweaks, docs.
  → Tier 1, plus a quick manual check.

When unsure, treat it as higher risk. For a compliance app, a wrong "ok" status is
worse than a build failure.

## TDD Iron Law
No production code without a failing test first. If production code was written first,
delete it and redo. RED (failing test) → GREEN (make it pass) → REFACTOR.

## Systematic Debugging (when a tier FAILS)
Reproduce → observe the actual behavior → form ≥3 hypotheses → test the likeliest →
fix the **root cause**, not the symptom → add a regression test → re-run the tier.
Stop if the fix patches blindly, deletes a failing test, or hides the error in a
fallback.

## Done = Evidence
"Done" requires: the exact command, raw output, the test/flow name, the tier reached,
and residual risk. For High-risk features, add the human approval note. "Everything is
fine" / "mọi thứ ổn" is never evidence.
