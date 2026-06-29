# GPSR Compliance Engine — User Stories

> Each story: WHO + WHAT + WHY, with acceptance criteria in **Given-When-Then**.
> AC is the contract `qa-integration` verifies against. English only.

Personas: **Merchant** (store owner/ops, uses the admin), **Buyer** (storefront
visitor), **Compliance auditor** (reviews records). Stories map to F0–F8.

---

## F0 — Scaffold & green harness
**As a** developer, **I want** a one-command verify chain, **so that** every change is
provably safe before merge.
- **Given** a clean checkout, **when** I run `bash init.sh`, **then** the DB on 3308
  comes up healthy, backend builds+tests, admin builds+tests, and it ends green.
- **Given** any step fails, **when** `init.sh` runs, **then** it exits non-zero and names
  the failing step.

## F1 — Data model
**As a** developer, **I want** a versioned schema for entities, templates, rules,
products, and compliance records, **so that** the engine has a stable foundation.
- **Given** an empty DB, **when** migrations run, **then** all five tables exist with the
  documented columns and a `compliance_record.matched_rule_id` (nullable for overrides).
- **Given** a migration applied, **when** I run its `down`, **then** the schema reverts
  cleanly.

## F2 — GPSR rules engine (core)
**As a** merchant, **I want** products auto-classified into responsible entity +
warnings, **so that** I don't fill compliance data per SKU.
- **Given** a product matching an ordered rule, **when** the engine runs, **then** the
  record gets that rule's entity + warning templates, status `ok`, and `matched_rule_id`
  set.
- **Given** a product matching no rule, **when** the engine runs, **then** status is
  `needs_review` and no entity/warnings are silently invented.
- **Given** two rules could match, **when** the engine runs, **then** the documented
  precedence (priority order) decides, deterministically — same input always same output.
- **Given** a manual override exists, **when** the engine runs, **then** the override
  wins, status is `override`, and it is recorded as such.

## F3 — Backend API + Shopify sync
**As a** merchant, **I want** my real Shopify products synced in, **so that** I classify
actual inventory.
- **Given** a valid OAuth install, **when** sync runs, **then** products are mirrored
  locally with id, title, tags, category, and the fields rules match on.
- **Given** a product is created/updated in Shopify, **when** the webhook fires, **then**
  the local mirror updates within one sync cycle.
- **Given** the admin requests products+status, **when** it calls the API, **then** the
  JSON response shape matches the documented contract (snake_case) exactly.

## F4 — Entity & warning-template library
**As a** merchant, **I want** to manage responsible operators and warning templates,
**so that** rules can reference them by id.
- **Given** the library screen, **when** I create an entity/template, **then** it persists
  and is selectable in rule config.
- **Given** a template is referenced by a rule, **when** I try to delete it, **then** I am
  warned/blocked rather than silently breaking the rule.

## F5 — Classification rules config
**As a** merchant, **I want** to define ordered rules with visible precedence, **so that**
I control how products classify.
- **Given** the rules screen, **when** I reorder rules, **then** the new precedence is
  shown clearly and saved.
- **Given** a rule with conditions, **when** I save it, **then** it references a valid
  entity + template ids and is usable by the engine.

## F6 — Bulk product editor + status
**As a** merchant, **I want** to apply a ruleset to many products at once and see status,
**so that** I handle inventory at scale.
- **Given** selected products, **when** I apply the ruleset, **then** each gets a
  per-item result and its status (`ok` / `needs_review` / `override`) is visible.
- **Given** the product table, **when** it renders, **then** `needs_review` is visually
  distinct from `ok` (never identical).
- **Given** a bulk action, **when** it runs, **then** progress is shown — not one opaque
  spinner.

## F7 — Storefront safety block
**As a** buyer, **I want** to see required safety warnings + responsible operator on the
product page, **so that** the store is actually GPSR-compliant.
- **Given** a classified product (status `ok`/`override`), **when** I view its page,
  **then** the safety block shows the mapped warnings and entity.
- **Given** merchant/inferred text, **when** it renders, **then** it is escaped (no XSS).
- **Given** a `needs_review` product, **when** I view its page, **then** the block does
  not display false/empty compliance data.

## F8 — Security hardening
**As a** compliance auditor, **I want** deterministic guardrails enforced, **so that**
secrets and destructive actions can't slip through.
- **Given** the harness, **when** an agent attempts to read `.env` or run `rm -rf`,
  **then** the PreToolUse hook blocks it (exit 2).
- **Given** merchant-supplied input, **when** it reaches the backend/any LLM call,
  **then** it is treated as untrusted (injection review documented).
