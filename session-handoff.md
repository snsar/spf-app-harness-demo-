# Session Handoff — GPSR Compliance Engine

> Read this first when starting a session. Keep it short and current; update before you stop.

## Where things stand — 🎉 BUILD COMPLETE
**All features F0–F9 are `done` and evidence-gated** in `.claude/feature_list.json`.
`bash init.sh` → **HARNESS XANH** with security guardrails ACTIVE and the DB tier
(MySQL:3308) running for real. Whole-project: **184 tests green** (backend 116 + frontend 68).

The app is a working EU-GPSR compliance engine for Shopify:
- Go+Gin backend (per-shop OAuth, multi-tenant API, rules engine, Shopify sync, webhooks,
  metafield writer) on port 8000.
- React+Polaris embedded admin (entity/template library, rules config, bulk product editor).
- Liquid theme app extension (storefront safety block).
- MySQL:3308, shop-scoped schema (migrations 001–003).

## What remains = LIVE VERIFY (needs interactive Shopify auth + a reachable URL — USER runs these)
The automated tiers (unit/integration/DB) are all green. These need a real dev store and a
public URL (tunnel or deployed domain) — they cannot run headless. Full steps in
`deploy/README.md`. In order:
1. `shopify app deploy` — push shopify.app.toml (3 webhooks, metafield defs), the theme
   extension, config to Partners. (App already linked as `gspr-harness`, client_id set.)
2. `shopify app dev` — creates a tunnel + serves; OR deploy to a real public domain.
   (Local `gpsr.quotesnap.local` via /etc/hosts works for browser-initiated flows but
   Shopify CANNOT call back to it — callbacks/webhooks need the tunnel or a real domain.)
3. Click **Install** on the dev store → confirm OAuth completes, shop row appears in MySQL.
4. Create/update a product in the store → confirm webhook → `compliance_record` = needs_review.
5. Uninstall → confirm shop + all its data removed (app/uninstalled handler).
6. **Playwright E2E for F6 + F7** (deferred per user decision 2026-06-29): products/compliance
   UI renders; storefront block ok-renders / needs_review-hides / override-renders /
   XSS-injection-escapes in a real browser.

## Pre-prod hardening (documented, non-blocking — do before public launch)
- **F9-1 (MEDIUM):** nginx CORS reflects `$http_origin` with credentials for all locations
  → tighten to an allowlist (`*.myshopify.com`, `admin.shopify.com`) for a public domain.
- **F9-2/F9-3 (LOW):** switch nginx `location /` from dev (Vite proxy :5173) to prod
  (`root frontend/dist` + try_files) and set `application_url` to the real domain before deploy.
- 5 frontend npm vulns are DEV-ONLY (vite/esbuild/vitest, not in prod bundle) — fix = vite@8
  (breaking), schedule separately. 658KB JS bundle → code-split post-launch.
- F3-SEC-3 (access_token at rest plaintext) → follow-up ticket (app-layer AES-GCM).

## How to run / dev
- DB: `docker compose up -d db` (MySQL:3308, container gpsr-mysql).
- Backend: needs `.env` (SHOPIFY_API_KEY/SECRET/APP_URL + DB creds). Guardrails now block
  shell from sourcing `.env` directly — use `bash init.sh` (sources it safely) for verify,
  or run the backend with the env loaded by your shell outside the agent.
- Verify chain: `bash init.sh` must end HARNESS XANH (merge gate).

## Key context not obvious from code
- Stack fixed: React+Polaris admin, Go+Gin, MySQL:3308, monorepo (frontend/ backend/
  storefront/ + root shopify.app.toml). Do NOT change db/port/script names.
- Product ids exposed by the API are SURROGATE ids (not Shopify ids) — migration 003 Q1.
- Storefront data delivery = Shopify product metafields (gpsr_status/entity_json/warnings_json),
  read server-side in Liquid (no public endpoint). needs_review/absent → block renders nothing.
- Sub-agents run `model: sonnet`; orchestrator may override `model: opus` per-call on
  HIGH-risk security gates (it did for F3/F3b/F7/F8 reviews).
- Per-feature design specs in `docs/specs/`; QA + security reports in `_workspace/`.

## Before you stop
- Update `progress.md` and this file. Leave `init.sh` green.
