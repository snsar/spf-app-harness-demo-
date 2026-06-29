# Progress — GPSR Compliance Engine

> Running log. One feature in-progress at a time. DONE requires evidence in
> `.claude/feature_list.json`.

## Status snapshot
- **Done:** F0 (scaffold), F1 (migrations), F2 (rules engine — QA+security gated),
  F3 (per-shop OAuth — security FAIL→fixed→re-verify PASS)
- **In progress:** F3b — Backend API + Shopify sync. Design APPROVED (docs/specs/F3b-api-contract.md),
  building via TDD. CRITICAL: migration 003 adds multi-tenant shop_id scoping (F1 was single-tenant);
  F2 repo/Classifier signatures gain leading shopID (pure Classify unchanged).
- **Blocked:** (none)
- **Next up after F3b:** F4 + F5 (admin library / rules config), both deps F3b.

## F3b design decisions (locked 2026-06-29)
Q1=product surrogate id + shopify_product_id UNIQUE(shop_id,shopify_product_id).
Q2=category←productType, material/origin←gpsr.* metafields. Q3=webhook refresh
title/tags/category+needs_review, material/origin via /api/sync. Q4=offset pagination
page/limit default 50 max 250 (overrides spec's cursor). Q5=edit priority via PUT, no
bulk-reorder. Q6=CSP frame-ancestors set by Go middleware. Webhook HMAC is RAW-BODY
(differs from F3 query-param HMAC) → new VerifyWebhookHMAC primitive.

## DAG revision 2026-06-28 (user caught missing pieces)
User flagged 3 gaps the original plan folded away or omitted: per-shop install/auth,
nginx reverse-proxy (1 domain, reuse quotesnap pattern), and shopify.app.toml deploy.
Changes: split old F3 → F3 (per-shop OAuth + `shop` table + session-token mw) + F3b
(API + sync); F4/F5 now depend on F3b; added F9 (deploy: shopify.app.toml + nginx +
README) after F7. Backend port standardized to 8000 (matches existing nginx upstream).
Existing nginx pattern read from /etc/nginx/sites-available/*.quotesnap.local.conf
(HTTPS self-signed, location-based FE/BE split, CORS+OPTIONS preflight at proxy).

## Notes for later
- MySQL DDL is non-transactional (auto-commit) → a mid-migration failure leaves orphan
  tables. For F2+ migrations: keep statements idempotent (`CREATE TABLE IF NOT EXISTS`)
  or accept manual cleanup. Full DDL transactionality isn't available in MySQL.

## Log
| Date | Feature | Event | Evidence / note |
|------|---------|-------|-----------------|
| 2026-06-28 | — | Harness bootstrapped (agents, skills, AGENTS.md, feature_list, init.sh, security config) | — |
| 2026-06-28 | — | Harness verified: 5 agents + 7 skills frontmatter OK, no commands, JSON valid, init.sh syntax OK | PreToolUse hook tested: rm -rf/.env blocked (exit 2), npm/go allowed (exit 0) |
| 2026-06-28 | — | Added docs/ guidance layer (planning/specs/prototype) + README | — |
| 2026-06-28 | — | Phase 1-4 planning: plan.md, user-stories.md, complex-cases.md, questions.md written; F3 desc updated (Shopify sync) | — |
| 2026-06-28 | — | All 6 DISCUSS questions resolved (user accepted recommendations); plan + questions.md updated | Plan approved. Ready for Phase 5 (Execute F0→F8) via loop prompt. |
| 2026-06-28 | F0 | DONE — scaffold backend+admin, docker MySQL:3308; `bash init.sh` GREEN (exit 0) | Debug: MySQL 8.4 crashed on removed `--default-authentication-plugin`; fixed by dropping it (caching_sha2_password default). |
| 2026-06-28 | F1 | DONE — 7 tables, Go migration runner (cmd/migrate up/down), init.sh runs migrate; GREEN | up/down/idempotent verified on 3308; C5 FK RESTRICT proven (Error 1451); TDD round-trip PASS. Schema: _workspace/F1_backend_schema.md |
| 2026-06-28 | F2 | DONE — GPSR rules engine (pure Classify + bulk Classifier + repo); QA + security gated | init.sh GREEN; false-green DB tier (F-1, found by QA+security independently) fixed via GPSR_DB_TESTS=1 + dbtest.SkipOrFail. _workspace/F2_*.md |
| 2026-06-29 | F3 | DONE — Shopify per-shop OAuth + install (shop table, HMAC/state/JWT, session-token mw, port 8000) | init.sh GREEN w/ DB tier. Independent security: FAIL F3-SEC-1 HIGH (empty-secret forgeable, reproduced) → TDD fix (Validate() fail-closed boot + primitive guards + gin logger query-strip + TrimSpace hardening) → re-verify PASS (forgery now rejected, no regression). _workspace/F3_security_auth.md |
| 2026-06-29 | — | Repo restructure: admin/ → frontend/; added storefront/ (theme app extension dir, README + extensions/.gitkeep) + root shopify.app.toml SKELETON for Shopify CLI | Synced init.sh, package.json, .gitignore, AGENTS.md (+changelog), plan.md, feature_list (F0/F9 desc), frontend-engineer agent + react-admin-shopify skill. shopify.app.toml network fields left as F9 placeholders. `bash init.sh` → HARNESS XANH (frontend/ path works). |
| 2026-06-29 | — | Local dev via nginx (reuse quotesnap pattern), NOT tunnel — user runs app cũ-style on gpsr.quotesnap.local | Added deploy/nginx/gpsr.quotesnap.local.conf (HTTPS mkcert + CORS/OPTIONS; / → Vite 5173, /api+/auth+/healthz → Go 8000) + deploy/setup-local.sh (sudo: mkcert cert, install site, /etc/hosts, nginx -t+reload). DOCUMENTED limit: /etc/hosts is local-only so Shopify-initiated OAuth callback/webhooks cannot reach it (browser-initiated /auth works); real callback needs tunnel or public domain (F9). Needs SHOPIFY_APP_URL=https://gpsr.quotesnap.local in .env (user-set; not secret). |
