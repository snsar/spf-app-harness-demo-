---
name: react-admin-shopify
description: Conventions for the React admin UI and the Shopify theme app extension of the GPSR Compliance Engine. Use for any admin UI work — the bulk product editor, classification-rules config, responsible-entity/warning-template library, compliance-status views — or the storefront safety block. Triggers on "the admin UI", "bulk editor", "rules config screen", "entity library", "storefront block", "theme extension", and re-work like "fix the admin", "restyle the table".
---

# React Admin + Theme Extension (GPSR)

Build the embedded admin in `frontend/` and the storefront safety block as a Shopify
theme app extension under `storefront/extensions/`. The admin's job is bulk compliance
work, not single-record entry.

## Source of Truth for UI
- Read `docs/prototype/` (design tokens + HTML/CSS) before building any screen — it is
  the UI source of truth. Never ship raw, unstyled HTML.
- Standard table/form/modal UX you already know — spend effort on GPSR-specific flows.

## Key Screens (the surface that matters)
1. **Bulk product editor** — table of products with current compliance status; select
   many → apply ruleset / set override / mark reviewed. Bulk is the primary action.
2. **Classification rules config** — ordered list of rules with visible precedence;
   editing order matters and must be obvious to the merchant.
3. **Entity + warning-template library** — manage responsible operators and warning
   templates (the data the rules reference by id).
4. **Compliance status / audit view** — per product: matched rule, entity, warnings,
   status (`ok` / `needs_review` / `override`), last generated.
5. **Storefront safety block** (theme extension) — renders mapped warnings + entity on
   the product page.

## Contract Discipline (why: kills cross-boundary bugs)
- Before wiring any call, read the backend's published request/response shape. Match
  field names and casing **exactly** (backend JSON is snake_case by default). Do not
  invent or guess field names — ask backend-engineer.
- Keep API types in one place (a typed client / hooks layer) so QA can compare the
  hook's expected shape against the real API response in one read.

## Embedded-App Constraints
- The admin runs embedded in Shopify Admin (App Bridge): handle the session token
  (attach as `Authorization: Bearer <jwt>` on every `/api` call; the backend's
  `RequireSessionToken` expects it).
- **Use Shopify Polaris (`@shopify/polaris`) for ALL admin UI** — this is a
  Built-for-Shopify-quality embedded app; do NOT ship plain/unstyled HTML. Wrap the app
  in Polaris `AppProvider` (with its CSS + en translations) once in the shared shell.
  Use `Page`/`Card`, `IndexTable`/`DataTable` for lists, `FormLayout`/`TextField`/
  `Select`/`Checkbox` for forms, `Banner`/`InlineError` for errors, `Frame`/`Navigation`
  for the shell. Respect any tokens in `docs/prototype/` if present.
- The theme extension runs on the storefront — treat all merchant/LLM-derived text as
  untrusted; escape before render (coordinate with `ai-security-review`). In React, never
  use `dangerouslySetInnerHTML` for merchant text — default escaping only.

## State & Data
- Surface the three terminal states clearly (`ok` / `needs_review` / `override`); never
  let "needs review" look identical to "ok".
- Bulk actions must show progress and a per-item result, not a single opaque spinner.

## Testing (verification-ladder)
- Component/logic tests for non-trivial behavior (bulk selection, rule reorder).
- Storefront block + critical admin flows → E2E via the `playwright-cli` skill.
- DONE = build passes + E2E green; paste the output as evidence.
