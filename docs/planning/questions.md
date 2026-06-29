# GPSR Compliance Engine — Open Questions (DISCUSS) — RESOLVED

> Task classification bucket: **DISCUSS**. All resolved 2026-06-28: user accepted every
> recommendation. These are now decisions, not open questions. English only.

## Q1 — Warning locale (affects F1, F2, F7) — was BLOCKING F2
**Decision:** single locale for MVP (the store's primary EU language); multi-locale in
phase 2. `warning_template.locale` stays in the schema but MVP seeds/uses one locale.

## Q2 — Auto vs manual classification trigger (affects F2, F6, C8)
**Decision:** manual "apply ruleset" for MVP (merchant in control, predictable). Auto
mode is a later toggle. C8 staleness: on product update mark affected records
`needs_review` (no silent auto re-run in MVP).

## Q3 — Responsible-entity data source (affects F4)
**Decision:** manual entry for MVP. No supplier import.

## Q4 — Pricing/plan gating
**Decision:** out of MVP code. Do NOT design billing now.

## Q5 — Shopify app distribution target (affects F3, F8)
**Decision:** build to public-app / Built-for-Shopify quality, test against a single dev
store first.

## Q6 — GPSR document/PDF generation
**Decision:** out of MVP (phase 2).
