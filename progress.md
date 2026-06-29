# Progress — GPSR Compliance Engine

> Running log. One feature in-progress at a time. DONE requires evidence in
> `.claude/feature_list.json`.

## Status snapshot — 🎉 ALL F0–F9 DONE
- **Done:** F0, F1, F2, F3, F3b, F4, F5, F6, F7, F8, F9 — every feature evidence-gated.
- **In progress:** (none — build loop complete)
- **Blocked:** (none)
- **Whole-project:** `bash init.sh` → HARNESS XANH with guardrails ACTIVE + DB tier.
  184 tests green (backend 116 + frontend 68). F8-SEC-1 closed (guardrails restored at F9).
- **REMAINING = live verify (USER, needs interactive Shopify + deployed env):**
  shopify app deploy; click Install (OAuth); live product webhook; live app/uninstalled;
  Playwright E2E for F6+F7; nginx prod-mode switch. See deploy/README.md + session-handoff.md.
- **Pre-prod hardening (documented, non-blocking):** F9-1 nginx CORS allowlist (MEDIUM),
  F9-2/3 nginx prod-mode + real application_url (LOW); npm dev-only vulns; 658KB bundle code-split.

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
| 2026-06-29 | — | Shopify CLI link: shopify.app.toml linked to app gspr-harness (client_id filled by CLI); [[web]] moved to backend/shopify.web.toml (roles=backend); config.Load() reads CLI aliases APP_URL/HOST + PORT (precedence: own var wins) so both local .env and `shopify app dev` tunnel work | Local stack verified GREEN: backend boot + nginx https + /auth 302 to Shopify + /api/me 401. Live callback still needs tunnel (hosts is local-only). |
| 2026-06-29 | F3b | DONE — Backend API + sync + multi-tenant scoping (migration 003 shop_id; F2 sig +shopID) | init.sh XANH, 168 tests. QA PASS + security PASS (2 MEDIUM: cross-shop override + SSRF, both fixed TDD + re-verified w/ mutation testing). _workspace/F3b_{qa_report,security_review}.md |
| 2026-06-29 | — | 5 sub-agents model opus→sonnet (token cost) | orchestrator may override model=opus per-call on HIGH-risk security gates. AGENTS.md changelog. |
| 2026-06-29 | — | Frontend standardized on Shopify Polaris (skill react-admin-shopify updated) | User caught agents weren't told to use Polaris; now a durable skill rule for F6/F7. F4 owns @shopify/polaris@13.9.5 install. |
| 2026-06-29 | F4 | DONE — entity + warning-template library (Polaris, App Bridge session token, shared app-shell) | init.sh XANH from clean (npm ci resolves Polaris), 47 tests. QA no-drift; F4-1 LOW type fixed. _workspace/F4_F5_qa_report.md |
| 2026-06-29 | F5 | DONE — classification rules config (Polaris, precedence (priority,id) visible, reorder=edit priority) | Parallel with F4. init.sh XANH, TDD ordering test. QA no-drift, no phantom order endpoint. |
| 2026-06-29 | F6 | DONE (E2E deferred to F9) — bulk product editor + 3-state compliance status (Polaris) | init.sh XANH, 68 tests. QA PASS unit/integration, no drift, no phantom endpoint, mark-reviewed=apply-ruleset. E2E tier OPEN→deferred F9 (user waiver). _workspace/F6_qa_report.md |
| 2026-06-29 | F7 | DONE (E2E→F9, HUMAN APPROVED) — storefront safety block (Option B metafields + Liquid theme ext) | Backend WriteComplianceMetafields (SSRF-guarded, best-effort, surface warning) + storefront/extensions/safety-block (Liquid, all outputs \| escape, no JS). Security PASS Opus (XSS clean, SSRF 0-egress, no leak), 2 LOW. init.sh XANH w/ DB. _workspace/F7_security_review.md |
| 2026-06-29 | F8 | DONE (cond: F8-SEC-1→F9) — whole-app security hardening pass (Opus) | App posture PASS, all prior HIGH/MED re-verified fixed in code; SQL parameterized, \| escape, no secret-log all HOLD. F8-SEC-1 HIGH: guardrails (deny+hook) commented out by user during .env debug → restore at F9/merge (user deferred). npm vulns dev-only accepted. init.sh XANH. _workspace/F8_security_hardening.md |
| 2026-06-29 | F9 | DONE — deploy config (LAST): shopify.app.toml webhooks + nginx prod/dev + README + app/uninstalled handler + GUARDRAILS RESTORED (F8-SEC-1 closed) | TDD uninstall handler (teardown order F3b-proven, multi-tenant). QA PASS, 184 tests green whole-project. Guardrails active (live-confirmed). Launch gaps F9-1/2/3 documented (pre-prod). init.sh XANH. _workspace/F9_qa_report.md |
| 2026-06-29 | — | 🎉 BUILD LOOP COMPLETE — all F0–F9 done, evidence-gated. Whole-project init.sh HARNESS XANH with guardrails active. | Remaining = live verify (interactive Shopify) per session-handoff.md. |
