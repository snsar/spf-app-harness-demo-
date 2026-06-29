# GPSR Compliance Engine — Plan

> Design-first artifact. Approve before code. English only. Drives the F0–F8 DAG in
> `.claude/feature_list.json`.

## 1. Problem & value
EU GPSR (in force since Dec 2024) requires every product sold to EU consumers to carry a
manufacturer, an EU-based **responsible economic operator**, applicable **safety
warnings**, and traceable safety information. Merchants with hundreds–thousands of SKUs
cannot do this per-product by hand. This app **infers compliance data in bulk** via a
rules engine and renders a compliant **storefront safety block** — not a data-entry form.

## 2. MVP scope (decided)
- **Product data:** real sync from the **Shopify Admin API** (OAuth + product read,
  webhook on product create/update). This is a real Shopify app, not mock data.
- **Storefront:** **included** — a theme app extension renders warnings + responsible
  entity on the product page, so the store is actually compliant to the buyer.
- **In:** bulk classification, rules config, entity/template library, compliance status,
  storefront block, security hardening.
- **Out (phase 2):** AI-assisted warning suggestion, multi-language warning packs beyond
  a single locale, GPSR document/PDF generation, analytics.

## 2b. Resolved decisions (from questions.md, 2026-06-28)
- **Locale:** single locale for MVP (multi-locale phase 2).
- **Classification trigger:** manual "apply ruleset"; auto mode later. On product update,
  mark affected records `needs_review` (no silent re-run).
- **Responsible entity:** manual entry (no import).
- **Pricing/billing:** out of MVP — do not design.
- **Distribution:** build to public-app / Built-for-Shopify quality; test on a dev store.
- **GPSR PDF/doc generation:** out of MVP (phase 2).

## 3. Architecture (monorepo)
```
frontend/             React, embedded in Shopify Admin (App Bridge + session token)
backend/              Go + Gin (port 8000) — handler → service → repository; OAuth, sync, rules engine
storefront/           Shopify theme app extension(s) under storefront/extensions/ — storefront safety block
shopify.app.toml      app config at repo root — Shopify CLI runs app dev/deploy from here (F9 fills values)
deploy/               nginx site conf + deploy README (F9)
docs/                 plan / specs / prototype (guidance layer)
MySQL :3308           shop, product, entity, warning_template, classification_rule, compliance_record
```

### Per-shop auth (multi-tenant, F3)
Public embedded app — each shop installs and gets its own token:
```
Merchant clicks install / opens app
   → GET /auth?shop=<shop>.myshopify.com  (backend)
   → Shopify OAuth consent
   → GET /auth/callback  → exchange code → store token in `shop` table
   → app loads embedded; every admin API call carries an App Bridge
     session token (JWT) → middleware verifies it and resolves the shop
```
`shop` table: shop_domain (unique), access_token, scope, installed_at. All product/
rules/compliance data is scoped to the authenticated shop.

### Deployment (F9) — reuses the existing nginx pattern
One domain (e.g. `gpsr.quotesnap.local`), HTTPS with self-signed cert, the same
CORS/OPTIONS-preflight handling as the current quotesnap sites:
```
location /      → frontend FE (Vite dist)
location /api   → backend Go on 127.0.0.1:8000
```
`shopify.app.toml` declares application_url + redirect_urls (must match /auth/callback),
scopes (read_products, write_products), and product create/update webhooks.

Data flow:
```
Shopify Admin API ──sync──▶ product (local mirror)
                                  │
            classification_rule ──▶ rules engine ──▶ compliance_record
              warning_template ──┘        │             (entity + warnings + status)
                    entity ──────────────┘              │
                                                        ▼
                                       theme extension ─▶ storefront safety block
                                       React admin ─────▶ bulk editor / status views
```

## 4. Core logic — the rules engine (the clever part)
Deterministic, auditable bulk inference (see the `gpsr-rules-engine` skill):
- Ordered `classification_rule`s match a product by tag/category/material/origin.
- First/weighted match → maps a responsible `entity` + `warning_template`s.
- Every `compliance_record` stores the **matched rule id** (audit trail).
- Terminal states: `ok` | `needs_review` (no rule matched — never silently empty) |
  `override` (manual, always wins, recorded).

## 5. Feature breakdown (maps to F0–F8 DAG)
| ID | Feature | Depends on | Risk |
|----|---------|-----------|------|
| F0 | Scaffold + `init.sh` green (monorepo, docker MySQL:3308) | — | Low |
| F1 | Data model + migrations | F0 | Med |
| F2 | GPSR rules engine (core logic) | F1 | **High** |
| F3 | Shopify per-shop auth + install (OAuth, `shop` table, session-token mw) | F2 | **High** |
| F3b | Backend API (Gin) + per-shop Shopify sync (product read, webhook) | F3 | **High** |
| F4 | Admin: entity + warning-template library | F3b | Med |
| F5 | Admin: classification rules config (ordered, visible precedence) | F3b | Med |
| F6 | Admin: bulk product editor + compliance status | F4, F5 | Med |
| F7 | Theme extension: storefront safety block | F6 | **High** |
| F8 | Security hardening pass | F7 | **High** |
| F9 | Deploy: shopify.app.toml + nginx (1 domain) + README | F7 | Med |

> Auth was split out of the old "F3 + sync": per-shop OAuth/install (F3) is the
> foundation everything else is scoped to, so it stands alone; API + sync is F3b.
> Deployment (shopify.app.toml + nginx) is its own F9 after the app works end-to-end.

## 6. How this gets built
6-step flow via `gpsr-orchestrator`: this plan (2) → user stories (3) → classify (4) →
execute hybrid team (5) → evidence-gated verify (6). One feature in-progress at a time,
respect the DAG. DONE = real `init.sh` / test output in the feature's `evidence`.

## 7. Verification posture
High-risk features (F2, F3, F7, F8) require the full ladder through **E2E (Playwright)**
plus human approval — wrong compliance data has legal consequence. See
`verification-ladder`.
