# F7 — Theme Extension: Storefront Safety Block — Contract

> Design-first artifact. Approve before any code. English only.
> Full design rationale: `_workspace/F7_storefront_design.md`.
> This file is the published contract for QA and backend integration.

---

## Decision: Data Delivery = Option B (Metafields)

When the backend classifies a product (apply-ruleset or override), it writes the
compliance outcome to three app-owned Shopify product metafields via the Admin GraphQL
`metafieldsSet` mutation. The Liquid block reads these server-side — no API call, no
JavaScript, no public endpoint.

**Approved? Awaiting user confirmation (Open Question Q1).**

---

## Metafield Definitions

Namespace in Liquid: `app` (maps to GraphQL reserved namespace `$app`).
Declared in `shopify.app.toml` under `[product.metafields.app.*]`.

| Key | Type | Purpose |
|-----|------|---------|
| `gpsr_status` | `single_line_text_field` | One of: `"ok"`, `"override"`, `"needs_review"` |
| `gpsr_entity_json` | `json` | Responsible entity: `{name, address, role}` |
| `gpsr_warnings_json` | `json` | Array of warning text strings |

Liquid access: `{{ product.metafields.app.gpsr_status.value }}`, etc.

---

## Metafield Value Shapes

### `gpsr_entity_json` (JSON object)

```json
{
  "name": "Acme EU GmbH",
  "address": "Musterstraße 1, 10115 Berlin, DE",
  "role": "importer"
}
```

Fields: `name`, `address`, `role` only. Internal `id`, `shop_id`, timestamps are
NEVER written to the metafield.

### `gpsr_warnings_json` (JSON array of strings)

```json
["Warning text one.", "Warning text two."]
```

Plain text strings. No HTML. The `text` field from each resolved `WarningTemplate`.

### `gpsr_status` (string)

One of: `"ok"` | `"override"` | `"needs_review"`.

---

## Backend Write Contract

### New service method

```go
WriteComplianceMetafields(
    ctx              context.Context,
    shopID           int64,
    shopifyProductID int64,
    status           model.Status,
    entity           *model.Entity,  // nil when status == needs_review
    warnings         []string,        // empty when status == needs_review
) error
```

File: `backend/internal/service/shopify_metafield.go` (new).
Interface: `ShopifyMetafieldWriter` (injected, testable with fake HTTP client).

### Write trigger table

| Event | gpsr_status | gpsr_entity_json | gpsr_warnings_json |
|-------|-------------|-----------------|-------------------|
| apply-ruleset → ok | `"ok"` | entity object | warnings array |
| set-override | `"override"` | override entity | override warnings |
| apply-ruleset → needs_review | `"needs_review"` | null / cleared | null / cleared |
| webhook marks needs_review (C8) | `"needs_review"` | null / cleared | null / cleared |
| clear-override → re-classify | per new status | per new status | per new status |

Write is **best-effort**: DB commit happens first; metafield failure is non-fatal.
On failure, the API response includes `"metafield_sync_warning": "<message>"` (subject
to Q2 user decision). Failure is logged; the merchant can re-classify to re-sync.

---

## Extension File Contract

```
storefront/extensions/safety-block/
  shopify.extension.toml         # extension type = "theme"
  blocks/
    safety-block.liquid          # app block, target = "section", enabled on product template
  assets/
    safety-block.css             # scoped styles, no JavaScript
  locales/
    en.default.schema.json       # merchant-facing setting labels
```

---

## Block Settings Schema

| ID | Type | Default | Description |
|----|------|---------|-------------|
| `heading` | `text` | "Product Safety Information" | Section heading shown to buyers |
| `show_entity` | `checkbox` | `true` | Show responsible operator section |
| `show_warnings` | `checkbox` | `true` | Show safety warnings section |

Compliance content (entity, warnings) comes from metafields only — merchants do not
configure compliance text in the theme editor.

---

## Terminal-State Render Contract

| `gpsr_status` value | Renders |
|--------------------|---------|
| `"ok"` | Full block: heading + entity (if show_entity) + warnings (if show_warnings) |
| `"override"` | Full block (same as ok — override IS the compliance data) |
| `"needs_review"` | Nothing — empty/hidden container. No compliance claims. |
| Absent / blank | Nothing — same as needs_review |

**NEVER render partial data (entity without warnings, or warnings without entity when
status is needs_review). NEVER render a "pending" or "compliant" message for
needs_review. NEVER fabricate data.**

---

## XSS Contract

| Surface | Escaping method | Guard |
|---------|----------------|-------|
| `block.settings.heading` | `{{ value \| escape }}` in Liquid | Liquid output + explicit filter |
| `gpsr_entity.name`, `.address`, `.role` | `{{ value \| escape }}` in Liquid | Liquid output + explicit filter |
| Each warning text string | `{{ warning_text \| escape }}` in Liquid | Liquid output + explicit filter |
| Block JS (none in MVP) | N/A | No JS in block |

Rules:
- `| raw` is PROHIBITED in all block Liquid files. Security gate greps for it.
- `innerHTML` is PROHIBITED if any block JS is added in future. Use `textContent`.
- Liquid auto-escapes `{{ }}` output; `| escape` is defense-in-depth and mandatory.

---

## Verification Plan

### Unit tier (required before done)

- `WriteComplianceMetafields` tests: all trigger points, failure non-fatal,
  needs_review passes nil entity + empty warnings.
- Security: method uses shop DB token, not caller-supplied.
- Liquid XSS: `| raw` absent (grep gate in ai-security-review).
- `needs_review` suppression: verified by logic review + QA shape comparison.

### Integration tier (QA-integration agent)

- Metafield JSON shapes match Liquid access paths exactly.
- `needs_review` metafield state produces null/absent entity and warnings.
- No contract drift between `WriteComplianceMetafields` payload and block access.

### E2E tier — DEFERRED TO F9 (user decision 2026-06-29)

Playwright tests against deployed dev store:
- ok product: entity + warnings render correctly.
- needs_review product: block hidden.
- override product: override entity + warnings render.
- XSS: `<script>alert(1)</script>` in warning text renders as escaped literal.

F7 is DONE-WITH-CONDITION: unit + integration closed, E2E deferred to F9.

### Human approval required (plan §7)

Present before marking done: Liquid block code, escape audit, needs_review suppression
evidence, QA shape comparison.

---

## Open Questions (must be resolved before code)

| # | Question | Default if not answered |
|---|----------|------------------------|
| Q1 | Approve Option B (metafields) for data delivery? | BLOCKED — do not code |
| Q2 | Metafield write failure: silent (200 only) or surfaced (`metafield_sync_warning` field)? | Recommend: surfaced |
| Q3 | needs_review display: render nothing (recommended) or show "pending" placeholder? | Recommend: nothing |
| Q4 | Single locale for MVP confirmed (no `t:` filter on warning text)? | Assumed yes per plan §2b |
