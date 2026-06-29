# GPSR Compliance Engine — Complex Cases

> Task classification bucket: **COMPLEX**. These need extra care/tests but are decided —
> not blocking questions (those are in `questions.md`). Most cluster in the rules engine
> (F2) and Shopify sync (F3). English only.

## Rules engine (F2)

### C1 — Rule precedence & ties
Two rules match the same product. Decision: **ordered priority, first match wins**;
ties broken by lower `priority` integer, then by rule id (stable). Must be deterministic
and covered by a table-driven test. Document the order in the rule config UI (F5).

### C2 — No match → never silently empty
A product matching no rule must land in `needs_review`, not get a blank/fake record.
Property test: **every** product ends in exactly one of `ok` / `needs_review` /
`override`.

### C3 — Override vs re-classification
A manual `override` must survive a later bulk re-run (override wins, stays `override`).
But the merchant must be able to *clear* an override and let inference take over again.
Test both directions.

### C4 — Partial data
Product missing the field a rule matches on (e.g., no `material`). Rule conditions must
treat missing as "does not match" (not "matches empty"). Avoid false-positive
classification.

### C5 — Template/entity referenced then deleted
A rule references a `warning_template`/`entity` that is later deleted. Engine must not
crash or emit a dangling record. Enforce referential integrity (block delete if
referenced — see F4 story) and add a guard test.

### C6 — Bulk scale & idempotency
Applying a ruleset to N products must be idempotent (re-running with the same ruleset
yields the same records) and perform acceptably for a few thousand SKUs. Test idempotency
on a batch.

## Shopify sync (F3)

### C7 — Webhook ordering / duplicates
Shopify webhooks can arrive out of order or duplicated. Mirror updates must be
idempotent (upsert by Shopify product id) and tolerate replays.

### C8 — Sync vs classification staleness
A product changes in Shopify after classification → its `compliance_record` may be stale.
Decision: on product update, mark affected records `needs_review` (or re-run inference if
auto mode), never leave a silently-stale `ok`.

### C9 — OAuth scope / token expiry
Handle token refresh and missing scopes gracefully — surface a re-auth prompt, don't
fail sync silently.

## Storefront (F7)

### C10 — Untrusted text rendering
Merchant/inferred warning text rendered on the storefront is untrusted → escape (XSS).
Treat as a security finding if not escaped (coordinate with `ai-security-review`).

### C11 — needs_review on storefront
A `needs_review` product must NOT show partial/empty compliance UI to the buyer — decide
the fallback (hide block vs neutral message) and test it.
