# F3b — Backend API (Gin) + Shopify Sync — Design Spec (DESIGN ONLY, gate before code)

> Design-first artifact. Approve before any code. English only. Builds on F0
> (contract), F1 (schema), F2 (engine), F3 (auth). Does **not** redesign the F2
> engine — F3b is the HTTP + sync + multi-tenant-scoping layer over it.
>
> Status: DESIGN, awaiting orchestrator approval. No production code or tests
> written yet (TDD Iron Law: failing tests come first, after this spec is gated).

---

## 0. Scope recap (decided with user — exactly this)

1. Per-shop Shopify product sync via Admin API (GraphQL), **contract-based** (injected
   HTTP-client interface, unit-testable with a fake; live round-trip deferred).
2. Webhook endpoint(s) for product create/update: raw-body HMAC verify, shop resolve,
   idempotent upsert into the local mirror (C7), mark affected `compliance_record`s
   `needs_review` (C8 / plan 2b — no silent re-run).
3. REST endpoints behind `RequireSessionToken`, shop-scoped: products+status (paginated),
   entity CRUD, warning_template CRUD, classification_rule CRUD, bulk apply-ruleset,
   set-override, clear-override.
4. CSP `frame-ancestors` middleware for the embedded iframe.
5. nginx: add a `/webhooks` location; don't strip CSP.

---

## 1. CRITICAL: Multi-tenant migration is REQUIRED (top risk)

### Verdict: **A migration IS required (migration 003).**

The F1 schema is **single-tenant**. None of `product`, `entity`, `warning_template`,
`classification_rule`, `compliance_record` carries a `shop_id`. The `shop` table
exists (F3, migration 002) but **nothing references it**. As written, every shop
would read and write the *same* global product/rules/compliance rows — a cross-tenant
data-leak and a correctness bug for a public, multi-tenant Shopify app. This must be
fixed before F4–F7 (which are all shop-scoped) can be built on F3b.

### Migration 003 — `backend/migrations/003_shop_scoping.{up,down}.sql`

Adds `shop_id BIGINT NOT NULL` + FK `→ shop(id) ON DELETE CASCADE` to the five domain
tables, and reworks uniqueness/precedence keys to be **per-shop**:

| Table | Change |
|-------|--------|
| `entity` | add `shop_id` + FK; index `(shop_id)` |
| `warning_template` | add `shop_id` + FK; index `(shop_id)` |
| `product` | add `shop_id` + FK. **PK change:** the Shopify product id is only unique *within a shop* (two shops can theoretically mirror the same product id is not the real risk — the real point is rows must be partitioned by tenant and upsert must be per-shop). Keep `id` = Shopify product id but make the **upsert/unique key `(shop_id, id)`**. Concretely: keep `id` as a plain column, add surrogate handling — see "Product PK note" below. Index `(shop_id)`. |
| `classification_rule` | add `shop_id` + FK; precedence is per-shop, so the priority index becomes `(shop_id, priority)`; FK to `entity` unchanged (still RESTRICT, C5) |
| `compliance_record` | add `shop_id` + FK. **Unique key change:** `uq_record_product` becomes `UNIQUE (shop_id, product_id)` so one record per product *per shop*. Index `(shop_id, status)` |

Join tables (`rule_warning_templates`, `compliance_record_warnings`) do **not** need
`shop_id`: they are reachable only through an already-shop-scoped parent row, and
their FKs (CASCADE/RESTRICT) still hold. Scoping is enforced at the parent.

**Product PK note (decision needed — see Open Questions Q1).** Today `product.id` is the
Shopify product id and the PK. For multi-tenant, the natural key is `(shop_id, shopify_product_id)`.
Two clean options:
- **Option A (recommended):** rename the column intent — `product.id` stays the PK but
  becomes an auto-increment surrogate; add `shopify_product_id BIGINT NOT NULL` and a
  `UNIQUE (shop_id, shopify_product_id)`. `compliance_record.product_id` then FKs the
  surrogate. This is the cleanest multi-tenant shape but changes the F2 record's notion
  that `product_id` == Shopify id (model + repository touch-up).
- **Option B (smaller diff):** keep `product.id` = Shopify product id as PK, add
  `shop_id`, and rely on `(shop_id, id)` being unique. Works only if two shops never
  share a Shopify product id. They generally don't (ids are globally unique in Shopify
  in practice), but this is a **correctness assumption** — flag it.

> **STOP-AND-ASK (Q1):** Option A (surrogate id + `shopify_product_id`, robust) vs
> Option B (keep Shopify id as PK, smaller diff, relies on global-uniqueness assumption)?
> Default if no answer: **Option A** (correctness over diff size for a compliance product).

### Down migration
Drops the FKs and `shop_id` columns, restores the original unique/precedence keys.
Note the MySQL non-transactional-DDL caveat already documented in F1/F3: write the
down so it is safe to re-run; `init.sh` exercises up→down→up.

### Data-migration concern
If migration 003 runs against a DB that already has F1/F2 rows (none in CI — those are
test fixtures), `shop_id NOT NULL` with no default fails on existing rows. Mitigation:
the up migration adds the column `NULL` first if needed, but since there is no
production data and CI starts clean, we add it `NOT NULL` directly. (No backfill story
required — confirm there is no live data: there isn't, this is pre-launch.)

### Repository impact (every query gains a shop filter)
The F2 `ComplianceRepository` methods (`GetProducts`, `GetRecord`, `SaveRecord`,
`DeleteRecord`) currently take no shop. They must become shop-scoped. **Design decision:**
add `shopID int64` as the **first parameter** of each method (and of `Classifier`
methods that call them). This keeps scoping explicit and impossible to forget, and is
the cleanest interface change. The F2 engine (`Classify`, pure) is untouched. See §6.

> This changes the published F2 `ComplianceRepository`/`Classifier` signatures. That is
> a deliberate, necessary contract change — documented here, pushed to QA. The engine's
> behavior (precedence/terminal-states/override) is unchanged.

---

## 2. Shopify product sync (Admin API, GraphQL, contract-based)

### 2.1 Client interface (injected, fake-able)

```go
// internal/service/shopify_admin.go
type ShopifyAdminClient interface {
    // FetchProducts returns one page of products for a shop, given an offline
    // access token and an optional GraphQL cursor. cursor == "" means first page.
    FetchProducts(ctx context.Context, shopDomain, accessToken, cursor string) (ProductPage, error)
}

type ProductPage struct {
    Products []ShopifyProduct
    HasNext  bool
    EndCursor string
}

type ShopifyProduct struct {
    ID          string            // gid://shopify/Product/1234567890
    Title       string
    Tags        []string
    ProductType string
    Vendor      string
    Metafields  map[string]string // namespace.key -> value (for material/origin, see mapping)
}
```

The live HTTP implementation (`shopifyAdminHTTP`) is a thin POST to
`https://<shop>/admin/api/<version>/graphql.json` with header
`X-Shopify-Access-Token: <token>`, behind the interface. **Live round-trip is deferred**
(no real API call needed for F3b green): the sync service is unit-tested against a fake
`ShopifyAdminClient`.

### 2.2 GraphQL query (products)

```graphql
query SyncProducts($cursor: String) {
  products(first: 100, after: $cursor) {
    pageInfo { hasNextPage endCursor }
    edges {
      node {
        id
        title
        tags
        productType
        vendor
        # material / origin are not first-class Shopify fields — see mapping below
        metafields(first: 10, namespace: "gpsr") {
          edges { node { key value } }
        }
      }
    }
  }
}
```

### 2.3 Field mapping → local `product` (rules engine match fields)

The engine matches on **tag / category / material / origin** (F2 `MatchConditions`).
Mapping from Shopify:

| Local `product` field | Shopify source | Notes |
|-----------------------|----------------|-------|
| `id` / `shopify_product_id` | `node.id` (gid) | parse the numeric id out of `gid://shopify/Product/<n>` |
| `title` | `node.title` | direct |
| `tags` | `node.tags` | direct (array of strings) |
| `category` | `node.productType` | **Mapping decision:** Shopify's free-text `productType` is the closest match for our `category`. (Shopify also has a taxonomy `category` field; `productType` is the merchant-controlled one merchants actually fill — see Q2.) |
| `material` | metafield `gpsr.material` | **Not a native Shopify field.** Sourced from a merchant metafield. NULL if absent → engine treats as "field absent" (C4). |
| `origin` | metafield `gpsr.origin` | **Not a native Shopify field.** Same metafield strategy. NULL if absent (C4). |

**Fields we cannot get natively + handling:** `material` and `origin` have no native
Shopify product field. We read them from a `gpsr` metafield namespace. When absent →
stored NULL → the engine never false-matches on them (C4, already proven in F2). This
is the honest, non-inventing behavior the rules engine requires. (Documented; the F4/F5
admin or merchant docs explain the metafield convention. Confirm — Q2.)

### 2.4 Sync service + endpoint

- `service.ProductSyncService` with a `ShopifyAdminClient` + a `ProductRepository`
  (new — see §6). Method:
  `SyncProducts(ctx, shop *model.Shop) (synced int, err error)` — pages through all
  products, upserts each into `product` scoped to `shop.ID` (idempotent, C7), returns
  the count.
- Triggered by an authenticated endpoint: **`POST /api/sync`** (shop-scoped). Returns
  `{ "synced": <n> }`. This is the merchant-initiated "pull my products" action used by
  the F6 admin. Live Shopify reachability is an F9/deploy concern, not F3b green.

---

## 3. Webhooks (product create / update)

### 3.1 HMAC is RAW-BODY based — DIFFERENT from F3's query HMAC

F3's `VerifyQueryHMAC` hashes sorted **query params** (OAuth redirect). Webhooks are
**POST** with a JSON body; Shopify signs the **raw request body bytes** and sends the
result base64-encoded in header `X-Shopify-Hmac-Sha256`. Verification:

```
mac = HMAC_SHA256(rawBody, SHOPIFY_API_SECRET)   // bytes
ok  = constant_time_equal( base64.StdEncoding.encode(mac), header X-Shopify-Hmac-Sha256 )
```

New service primitive: `service.VerifyWebhookHMAC(rawBody []byte, headerHMAC, secret string) error`
(constant-time via `hmac.Equal`; empty-secret guard like the other primitives —
fail-closed). **Must read the raw body before Gin parses it** — the handler reads
`c.Request.Body` into bytes, verifies, then unmarshals (Gin's `ShouldBindJSON` consumes
the body and would defeat byte-exact HMAC, so we do not use it here).

### 3.2 Headers

| Header | Use |
|--------|-----|
| `X-Shopify-Hmac-Sha256` | base64 HMAC of raw body — verify first, fail-closed |
| `X-Shopify-Shop-Domain` | `<shop>.myshopify.com` — resolve the tenant; validate format with `ValidateShopDomain`, load the installed `shop` row (unknown/uninstalled → 401) |
| `X-Shopify-Topic` | `products/create` or `products/update` (informational; route already implies it) |
| `X-Shopify-Webhook-Id` | dedupe hint (idempotency is already guaranteed by upsert; optional log) |

### 3.3 Routes (decision)

**Two explicit routes**, one shared handler — clearer for Shopify topic registration
in F9 and self-documenting:

| Method & path | Auth | Body |
|---------------|------|------|
| `POST /webhooks/products/create` | webhook HMAC | Shopify product JSON |
| `POST /webhooks/products/update` | webhook HMAC | Shopify product JSON |

These live **outside** `/api` (no session token — Shopify is the caller, not the
browser). They are mounted on the root router and protected by webhook-HMAC verification,
not `RequireSessionToken`.

### 3.4 Behavior

1. Read raw body. Verify `X-Shopify-Hmac-Sha256` (constant-time). Fail → **401**
   `{ "error": "invalid webhook signature" }`. (No body echoed.)
2. Resolve shop from `X-Shopify-Shop-Domain`; not installed → **401**.
3. Parse the Shopify product JSON, map to the local `product` shape (§2.3 mapping —
   the webhook payload field names differ from GraphQL: `id`, `title`, `tags` (CSV
   string in REST webhook payloads → split), `product_type`, `vendor`, metafields not
   in the default webhook payload → material/origin left as-is/NULL unless a metafield
   webhook is configured; for MVP we map what the payload carries and leave
   material/origin to the `/api/sync` GraphQL path — see Q3).
4. **Idempotent upsert** into `product` scoped to `shop.ID` (C7 — upsert by
   `(shop_id, shopify_product_id)`; replays/out-of-order are safe).
5. **Mark affected compliance record `needs_review`** (C8 / plan 2b): if a
   `compliance_record` exists for that product *and its status is not `override`*, set
   it to `needs_review`. Overrides are NOT touched (C3 — a manual override survives a
   product change; the merchant decides when to clear it). No silent re-run of inference.
6. Return **200** (empty body or `{ "status": "ok" }`). Shopify treats non-2xx as a
   failed delivery and retries — so we return 200 once the upsert+mark commit succeeds,
   and only non-200 on a genuine failure (so Shopify retries).

> **Idempotency of step 5:** marking `needs_review` is itself idempotent (setting an
> already-`needs_review` row to `needs_review` is a no-op), so replays are safe (C7).

---

## 4. REST endpoints (all under `/api`, all `RequireSessionToken`, all shop-scoped)

JSON is **snake_case**. Every handler resolves the shop via
`handler.ShopFromContext(c)` (F3) and passes `shop.ID` to the service/repository so
no query can read another tenant's rows. Auth failure → **401** (F3 middleware).
Validation failure → **400** `{ "error": "<message>" }`. Not-found → **404**.
Cross-shop access of a known id → **404** (do not reveal another tenant's row exists).

### 4.1 Products + compliance status (paginated)

`GET /api/products?limit=<n>&cursor=<id>` (or `&page=<n>` — see Q4 pagination)

- Lists products for the shop joined with their compliance record (status, entity,
  warnings). Paginated.
- Response:
```json
{
  "products": [
    {
      "id": 7001,
      "title": "Wooden Toy Train",
      "tags": ["toys", "wood"],
      "category": "toys",
      "material": "wood",
      "origin": "CN",
      "compliance": {
        "status": "ok",
        "matched_rule_id": 10,
        "entity_id": 100,
        "warning_template_ids": [1, 2]
      }
    },
    {
      "id": 7002,
      "title": "Unmatched Gadget",
      "tags": [],
      "category": null,
      "material": null,
      "origin": null,
      "compliance": {
        "status": "needs_review",
        "matched_rule_id": null,
        "entity_id": null,
        "warning_template_ids": []
      }
    }
  ],
  "next_cursor": "7050",
  "has_next": true
}
```
- A product with no compliance record yet → `compliance: null` (never synthesize a fake
  record — C2). `200 OK`.

### 4.2 Entity CRUD

| Method & path | Request body | Response | Status |
|---------------|--------------|----------|--------|
| `GET /api/entities` | — | `{ "entities": [Entity...] }` | 200 |
| `POST /api/entities` | `{name,address,role,is_eu}` | created `Entity` | 201 |
| `GET /api/entities/:id` | — | `Entity` | 200 / 404 |
| `PUT /api/entities/:id` | `{name,address,role,is_eu}` | updated `Entity` | 200 / 404 |
| `DELETE /api/entities/:id` | — | — | 204 / 404 / **409** if referenced by a rule (C5 RESTRICT → map MySQL 1451 to 409 `{ "error": "entity is referenced by a classification rule" }`) |

`Entity` JSON:
```json
{ "id": 100, "name": "Acme EU GmbH", "address": "...", "role": "importer", "is_eu": true,
  "created_at": "2026-06-29T10:00:00Z", "updated_at": "2026-06-29T10:00:00Z" }
```

### 4.3 Warning-template CRUD

| Method & path | Request body | Response | Status |
|---------------|--------------|----------|--------|
| `GET /api/warning-templates` | — | `{ "warning_templates": [...] }` | 200 |
| `POST /api/warning-templates` | `{locale?,text,applies_to?}` | created template | 201 |
| `GET /api/warning-templates/:id` | — | template | 200 / 404 |
| `PUT /api/warning-templates/:id` | `{locale?,text,applies_to?}` | updated | 200 / 404 |
| `DELETE /api/warning-templates/:id` | — | — | 204 / 404 / **409** if referenced (C5) |

`WarningTemplate` JSON:
```json
{ "id": 1, "locale": "en", "text": "Choking hazard. Small parts.",
  "applies_to": { "tags": ["toys"] }, "created_at": "...", "updated_at": "..." }
```
`text` is merchant-supplied → UNTRUSTED. Stored as-is (parameterized SQL — F2 injection
guard pattern); escaping happens at the storefront (C10, F7), not here.

### 4.4 Classification-rule CRUD (ordered, precedence visible)

| Method & path | Request body | Response | Status |
|---------------|--------------|----------|--------|
| `GET /api/rules` | — | `{ "rules": [Rule...] }` **ordered by (priority asc, id asc)** so the UI shows precedence directly (C1) | 200 |
| `POST /api/rules` | `{priority,match_conditions,entity_id,warning_template_ids}` | created `Rule` | 201 |
| `GET /api/rules/:id` | — | `Rule` | 200 / 404 |
| `PUT /api/rules/:id` | full rule body | updated `Rule` | 200 / 404 |
| `DELETE /api/rules/:id` | — | — | 204 / 404 |

`Rule` JSON (matches F2 `model.Rule`):
```json
{ "id": 10, "priority": 100,
  "match_conditions": { "tags": ["toys"], "category": "toys", "material": null, "origin": "CN" },
  "entity_id": 100, "warning_template_ids": [1, 2] }
```
- `entity_id` and every `warning_template_id` must belong to the **same shop** →
  validated server-side (cross-shop reference → 400/404). The rule's warnings are stored
  via the `rule_warning_templates` join (delete+insert on update, mirroring the F2
  record-warning pattern).
- The GET list ordering is the **single source of truth for precedence** the F5 UI
  renders. (Reordering = editing `priority`; no separate reorder endpoint in MVP — Q5.)

### 4.5 Bulk apply-ruleset

`POST /api/compliance/apply` — runs the F2 `Classifier.ApplyRuleset` for the shop.

Request:
```json
{ "product_ids": [7001, 7002, 7003] }
```
- `product_ids` optional; if omitted/empty → apply to **all** the shop's products
  (the bulk default the F6 editor uses). The handler loads the shop's ruleset
  (rules + their warning ids + entity, ordered) and calls
  `Classifier.ApplyRuleset(ctx, shop.ID, ids, rules)`.
- Overrides are left untouched (C3); deterministic + idempotent (C6) — F2 guarantees.

Response (`200`):
```json
{ "applied": 3, "results": [
  { "product_id": 7001, "status": "ok", "matched_rule_id": 10, "entity_id": 100, "warning_template_ids": [1,2] },
  { "product_id": 7002, "status": "needs_review", "matched_rule_id": null, "entity_id": null, "warning_template_ids": [] },
  { "product_id": 7003, "status": "override", "matched_rule_id": null, "entity_id": 999, "warning_template_ids": [42] }
] }
```
Each result is a serialized `model.ComplianceRecord` (F2 shape — `null` for
matched_rule_id/entity_id is meaningful, never omitted).

### 4.6 Set override / clear override

| Method & path | Request body | Response | Status |
|---------------|--------------|----------|--------|
| `POST /api/compliance/override` | `{ "product_id": 7003, "entity_id": 999, "warning_template_ids": [42] }` | the resulting `override` record | 200 / 400 (bad entity/warning/cross-shop) |
| `DELETE /api/compliance/override/:product_id` | — | — | 204 |

- `POST` → `Classifier.SetOverride(ctx, shop.ID, productID, entityID, warningTemplateIDs)`.
  Validates the product, entity, and warning ids belong to the shop. Survives later
  apply runs (C3).
- `DELETE` → `Classifier.ClearOverride(ctx, shop.ID, productID)` — deletes the record so
  the next apply re-infers (C3 reverse). Returns 204 even if no record existed
  (idempotent clear).

### 4.7 Sync (from §2.4)

`POST /api/sync` → `{ "synced": <n> }` (200). Merchant-initiated product pull.

---

## 5. CSP for the embedded iframe

### Where: a small Gin middleware, applied to the routes that serve the embedded app

`handler.EmbeddedCSP()` sets, on every response:
```
Content-Security-Policy: frame-ancestors https://<shop>.myshopify.com https://admin.shopify.com;
```

### How the shop is determined

Shopify loads the app iframe with `?shop=<shop>.myshopify.com&host=...` on the document
request. The middleware reads the `shop` query param (or `host` decoded), validates it
with `service.ValidateShopDomain`, and substitutes it into `frame-ancestors`. If the
shop param is absent/invalid (e.g. a direct hit), fall back to **only**
`https://admin.shopify.com` (still allows the Admin shell; does not open the frame to
arbitrary origins). Never emit `frame-ancestors *` or reflect an unvalidated host
(clickjacking guard).

> The CSP must cover the **document/app-shell** responses the browser frames. In this
> backend that is whatever serves the embedded HTML entry. In the F9 deploy, nginx
> serves the SPA at `/`, so the CSP for the framed document is set by whoever serves
> that document. **Design note / Q6:** decide whether the app-shell HTML is served by
> nginx (then CSP belongs in nginx, and the backend sets it only on backend-served HTML
> if any) or by the Go backend. For F3b the testable, code-owned piece is the
> middleware + its shop-resolution logic; we apply it to any backend-served HTML and
> document the nginx requirement so nginx does not strip it.

This is a **code concern** (the header value depends on the validated shop). nginx must
**not** strip or override it (see §7).

---

## 6. Layering & new files (handler → service → repository)

```
handler (thin: HTTP, shop-from-context, status codes)
  ProductHandler, EntityHandler, WarningTemplateHandler, RuleHandler,
  ComplianceHandler, SyncHandler, WebhookHandler, CSP middleware
        │
service (business logic; shop-scoped)
  ProductSyncService (ShopifyAdminClient + ProductRepository)
  Classifier  ← F2, signatures gain leading shopID  (ApplyRuleset/SetOverride/ClearOverride)
  RuleService (load ordered ruleset, validate same-shop entity/warning refs)
  VerifyWebhookHMAC (new primitive, raw-body)
        │
repository (parameterized SQL only, every query filtered by shop_id)
  ProductRepository (new: Upsert(shopID, product), List(shopID, page), GetWithCompliance)
  EntityRepository, WarningTemplateRepository, RuleRepository (new CRUD, all shop-scoped)
  ComplianceRepository ← F2, methods gain leading shopID
```

### Contract change to F2 (documented, pushed to QA)
- `service.ComplianceRepository` interface methods gain a leading `shopID int64`:
  `GetProducts(ctx, shopID, ids)`, `GetRecord(ctx, shopID, productID)`,
  `SaveRecord(ctx, shopID, rec)`, `DeleteRecord(ctx, shopID, productID)`.
- `Classifier.ApplyRuleset(ctx, shopID, productIDs, rules)`,
  `SetOverride(ctx, shopID, productID, entityID, warnings)`,
  `ClearOverride(ctx, shopID, productID)`.
- The pure `Classify(product, rules)` engine is **unchanged**.
- `model.ComplianceRecord` gains no new JSON field (shop_id is a DB column, not part of
  the published record shape — the API is already shop-scoped by auth, so the client
  never needs it). Same for `model.Product`: API responses do not expose `shop_id`.

### Wiring (cmd/server/main.go)
Construct the new repositories from the existing `*sql.DB`, the new services, and mount
the new handlers on the existing `api := router.Group("/api"); api.Use(RequireSessionToken(...))`
group, plus the unprotected `/webhooks/...` routes on the root router, plus the CSP
middleware where the app shell is served.

---

## 7. nginx change (deploy/nginx/gpsr.quotesnap.local.conf)

Current location regex `location ~ ^/(api|auth|healthz)` does **not** match `/webhooks`.
Add `webhooks` to the alternation so Shopify webhook POSTs reach the backend:

```
location ~ ^/(api|auth|healthz|webhooks) { ... proxy_pass http://127.0.0.1:8000; }
```

Also: the backend's `Content-Security-Policy` header must **not** be stripped. The
current backend location block uses `proxy_hide_header` only for the `Access-Control-*`
headers, so CSP already passes through — but the SPA `location /` block (which serves
the framed document in F9) must **add** the correct `frame-ancestors` CSP (or proxy it
from the backend) and must not set a conflicting `X-Frame-Options: DENY`/`SAMEORIGIN`
that would break embedding. Document this in the conf comments. (The actual conf edit is
small; F3b spec'd, F9 may finalize the SPA-serving variant.)

> Same local-only caveat as F3: Shopify's servers can't reach `gpsr.quotesnap.local`,
> so live webhook delivery needs a tunnel/public domain (F9). F3b green does not require
> live delivery — webhook handler is unit-tested with crafted requests.

---

## 8. TDD plan — failing tests FIRST, per area

> Order: write each test RED, watch it fail, then implement. DB-backed tests use the
> `GPSR_DB_TESTS=1` + `dbtest.SkipOrFail` pattern from F2/F3 (no false-green skips).

### A. Migration 003 (DB-backed, `internal/migrate`)
- `TestMigration003_AddsShopIdColumns` — after up, `DESCRIBE` each of the 5 tables shows
  `shop_id` NOT NULL + the FK to `shop`.
- `TestMigration003_PerShopUniqueness` — two shops can each have a product with the same
  Shopify id / a compliance_record per (shop, product); the unique key is `(shop_id, …)`.
- `TestMigration003_DownRestores` — down removes `shop_id` + FKs; up→down→up clean
  (extends the existing round-trip test).
- `TestMigration003_ShopCascade` — deleting a `shop` row cascades its
  product/entity/template/rule/record rows (FK ON DELETE CASCADE).

### B. Sync mapping (unit, fake `ShopifyAdminClient`)
- `TestSync_MapsShopifyFields` — gid parsed to numeric id; tags/title/productType→category;
  metafield gpsr.material→material, gpsr.origin→origin.
- `TestSync_AbsentMetafields_NullFields` — missing material/origin metafields → NULL
  (engine reads as absent, C4).
- `TestSync_Paginates` — fake returns 2 pages; all products upserted; cursor followed.
- `TestSync_Idempotent` — syncing the same page twice yields one row per product (C7).
- `TestSync_ScopedToShop` — upserts carry the caller's shop_id.

### C. Webhook (unit handler + DB-backed upsert)
- `TestWebhook_RejectsBadHMAC` — wrong/empty `X-Shopify-Hmac-Sha256` → **401**, no DB write.
- `TestWebhook_AcceptsValidHMAC` — body signed with the secret → **200**.
- `TestWebhook_EmptySecret_FailClosed` — secret "" → reject (defense-in-depth).
- `TestWebhook_UnknownShop_Rejected` — `X-Shopify-Shop-Domain` not installed → **401**.
- `TestWebhook_IdempotentUpsert` — same payload twice → one product row (C7).
- `TestWebhook_OutOfOrderReplay` — replay an older payload → safe upsert (C7).
- `TestWebhook_MarksNeedsReview` — existing `ok` record for the product → becomes
  `needs_review` after a product update (C8).
- `TestWebhook_OverrideNotTouched` — existing `override` record → stays `override` (C3).
- `TestWebhook_ScopedToShop` — webhook for shop A never writes shop B's product.

### D. Each REST endpoint (handler unit + DB-backed where it touches SQL)
For products / entities / warning-templates / rules / compliance:
- happy path (correct status code + exact JSON shape from §4),
- **auth path**: no/invalid session token → **401** (reuse F3 middleware),
- **validation path**: malformed body / bad id → **400**; missing → **404**,
- C5 path: `DELETE` a referenced entity/template → **409** (1451 mapped).
- rules list ordered by `(priority, id)` (C1 visible precedence).
- apply-ruleset: happy (mix of ok/needs_review/override results), override survives (C3),
  idempotent re-run (C6).
- override: set then clear round-trip (C3 both directions).

### E. CSP
- `TestCSP_HeaderPresent` — response carries
  `Content-Security-Policy: frame-ancestors https://<shop>.myshopify.com https://admin.shopify.com`.
- `TestCSP_ValidatesShop` — invalid/absent `shop` param → falls back to
  `https://admin.shopify.com` only; never reflects an unvalidated origin.

### F. Shop isolation (the multi-tenant guarantee — DB-backed)
- `TestIsolation_ListProducts` — shop A's `GET /api/products` never returns shop B's rows.
- `TestIsolation_GetByIdCrossShop` — shop A requesting shop B's entity/rule/product id → **404**.
- `TestIsolation_ApplyRuleset` — applying for shop A never touches shop B's records.
- `TestIsolation_RuleRefsSameShop` — creating a rule referencing shop B's entity → rejected.

### G. Security (extends F2/F3 patterns)
- injection guard on the new write paths (entity/template/rule create with hostile text)
  — table survives (parameterized SQL).
- webhook HMAC constant-time (`hmac.Equal`), empty-secret fail-closed.
- no secret/token in any new response or log line.

---

## 9. Open questions (STOP-AND-ASK — do not guess)

- **Q1 (Product PK / migration shape):** Option A (auto-increment surrogate `id` +
  `shopify_product_id` + `UNIQUE(shop_id, shopify_product_id)`, robust, touches F2 model)
  vs Option B (keep Shopify id as PK + `shop_id`, smaller diff, assumes Shopify ids are
  globally unique across shops)? **Recommend A.**
- **Q2 (category + material/origin mapping):** OK to map `category` ← Shopify
  `productType` (merchant free-text), and `material`/`origin` ← `gpsr.*` metafields?
  Or should `category` come from Shopify's taxonomy `category` field instead? This
  determines what merchants must fill for rules to match.
- **Q3 (webhook material/origin):** product webhooks don't include metafields by default.
  Acceptable that webhook updates refresh title/tags/category but leave material/origin
  to the `POST /api/sync` GraphQL path (and mark `needs_review` so the merchant re-applies)?
- **Q4 (pagination):** cursor-based (`?cursor=<last_id>&limit=`) vs offset/page
  (`?page=&limit=`)? And the **default + max `limit`** (proposal: default 50, max 250)?
  This is a frontend contract point — needs to be fixed before F6.
- **Q5 (rule reordering):** is editing each rule's `priority` (PUT) enough for the F5 UI,
  or does the UI need a dedicated bulk-reorder endpoint
  (`PUT /api/rules/order { "ordered_ids": [...] }`)? F2 precedence is `(priority, id)`,
  already decided — this is only about the editing ergonomics.
- **Q6 (CSP ownership):** is the embedded app-shell HTML served by the Go backend or by
  nginx serving the SPA dist (F9)? Determines whether the CSP middleware fully covers the
  framed document or whether nginx must also emit `frame-ancestors`.

---

## 10. Endpoint summary (for frontend + QA)

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | `/api/products` | session | list products + compliance (paginated) |
| GET/POST | `/api/entities` | session | list / create entity |
| GET/PUT/DELETE | `/api/entities/:id` | session | read / update / delete (409 if referenced) |
| GET/POST | `/api/warning-templates` | session | list / create template |
| GET/PUT/DELETE | `/api/warning-templates/:id` | session | read / update / delete (409 if referenced) |
| GET/POST | `/api/rules` | session | list (ordered by precedence) / create rule |
| GET/PUT/DELETE | `/api/rules/:id` | session | read / update / delete rule |
| POST | `/api/compliance/apply` | session | bulk apply-ruleset |
| POST | `/api/compliance/override` | session | set override |
| DELETE | `/api/compliance/override/:product_id` | session | clear override |
| POST | `/api/sync` | session | pull products from Shopify Admin API |
| POST | `/webhooks/products/create` | webhook HMAC | mirror new product (C7) |
| POST | `/webhooks/products/update` | webhook HMAC | mirror update + mark needs_review (C8) |

All `/api/*` are shop-scoped via `RequireSessionToken` + `ShopFromContext`. CSP
middleware covers the embedded app-shell response.
