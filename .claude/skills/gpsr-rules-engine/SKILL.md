---
name: gpsr-rules-engine
description: The core business logic of the GPSR Compliance Engine — classify a product (by tag, category, material, origin) and map it to the correct responsible economic operator, required warnings, and safety-doc template, then render a compliant storefront safety block. Use for any work on product classification, warning mapping, responsible-entity assignment, the rules engine, bulk compliance inference, or the safety widget. Triggers on "classify products", "the rules engine", "map warnings", "responsible person logic", "safety block", and re-work like "fix the classification", "tune the rules".
---

# GPSR Rules Engine

This is the app's clever core: not a CRUD form, but a **rules engine** that infers
compliance data in bulk. Get this right and the app is valuable; get it generic and
it's just another data-entry tool.

## What GPSR Requires (the domain)
Under EU GPSR, each product sold to EU consumers must carry: a **manufacturer**, an
EU-based **responsible person / economic operator**, applicable **safety warnings**,
and traceable **safety information**. Doing this per-SKU by hand across thousands of
products is the pain. The engine infers it.

## The Core Logic (inference, not entry)
```
product (tags, category, material, origin, supplier)
        │
        ▼  classification rules (ordered, first-match or weighted)
product class  ───────────────┐
        │                     │
        ▼                     ▼
responsible-entity mapping   warning-template mapping
        │                     │
        └──────────┬──────────┘
                   ▼
        compliance record (entity + warnings + safety doc)
                   ▼
        storefront safety block (theme extension)
```

## Design the Rules Engine For
- **Determinism + auditability.** A given product + ruleset must always produce the
  same record, and you must be able to explain *which rule matched and why*. Store the
  matched-rule id on the record. Compliance disputes demand a trail.
- **Bulk first.** The primary operation is "apply ruleset to N products", not editing
  one. Optimize and test for batches.
- **Rule precedence is explicit.** Ordered rules with a documented tie-break (first
  match, or weighted score). Never leave precedence to chance.
- **Safe defaults + escape hatch.** Unmatched products fall to a flagged "needs review"
  state — never silently emit an empty/wrong safety record. A manual override per
  product always wins over inference, and is recorded as an override.
- **Templates are data, not code.** Warning templates and responsible-entity library
  are editable data the merchant manages; the engine references them by id.

## Data Model (shape the rest follows)
- `entity` — responsible economic operators (id, name, address, role, EU?).
- `warning_template` — id, locale, text, applies-to conditions.
- `classification_rule` — id, priority, match conditions (tag/category/material/origin),
  → entity_id, → warning_template_ids.
- `compliance_record` — product_id, matched_rule_id (or `override`), entity_id,
  warning_template_ids, status (`ok` | `needs_review` | `override`), generated_at.

## Verification (high risk — compliance)
Wrong compliance data has legal consequence → treat as **High risk**. Required tests:
- Unit: each rule matches/doesn't match the products it should (table-driven).
- Property: every product ends in exactly one terminal state (`ok` | `needs_review` |
  `override`) — never silently empty.
- Audit: matched-rule id is always recorded.
- E2E (Playwright): the storefront safety block renders the mapped warnings for a
  classified product. Require human approval before "done" (see `verification-ladder`).

## Security note
Merchant-supplied fields (product descriptions, custom warning text) are untrusted —
escape before rendering to the storefront, and treat them as possible injection vectors
if any LLM call processes them. Coordinate with the `ai-security-review` skill.
