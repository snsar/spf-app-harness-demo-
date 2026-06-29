# storefront/ — Shopify theme app extension(s)

The storefront safety block lives here as a Shopify **theme app extension**, managed
by the Shopify CLI together with the app config at the repo-root `shopify.app.toml`.

```
storefront/
  extensions/
    safety-block/
      shopify.extension.toml     # extension type = "theme", handle = "gpsr-safety-block"
      blocks/
        safety-block.liquid      # app block: reads metafields, renders entity + warnings
      assets/
        safety-block.css         # scoped styles, no JavaScript
      locales/
        en.default.schema.json   # merchant-facing setting labels (theme editor sidebar)
```

---

## What the block does

The block reads three app-owned product metafields written by the backend service
(`backend/internal/service/shopify_metafield.go`) after every classify / override:

| Metafield key (Liquid access) | GraphQL key (`$app` namespace) | Type |
|---|---|---|
| `product.metafields.app.gpsr_status.value` | `gpsr_status` | `single_line_text_field` |
| `product.metafields.app.gpsr_entity_json.value` | `gpsr_entity_json` | `json` |
| `product.metafields.app.gpsr_warnings_json.value` | `gpsr_warnings_json` | `json` |

### Metafield shapes the block expects

**`gpsr_entity_json`** (Shopify parses the JSON automatically for `json` type
metafields; Liquid receives it as an object):

```json
{
  "name": "Acme EU GmbH",
  "address": "Musterstraße 1, 10115 Berlin, DE",
  "role": "importer"
}
```

Fields: `name`, `address`, `role` only. Internal fields (`id`, `shop_id`,
`created_at`, `updated_at`) are never written to this metafield.

**`gpsr_warnings_json`** (Liquid receives it as an array of strings):

```json
["Choking hazard. Keep away from children under 3.", "Warning: Contains small parts."]
```

**`gpsr_status`** — one of `"ok"`, `"override"`, `"needs_review"`.

### Terminal-state render rules

| `gpsr_status` | Renders |
|---|---|
| `"ok"` | Full block: heading + entity (if show_entity) + warnings (if show_warnings) |
| `"override"` | Identical to `"ok"` — override data IS the compliance data |
| `"needs_review"` | Nothing — zero output to buyer |
| Absent / blank | Nothing — same suppression as `"needs_review"` |

**Never** render a "pending" placeholder or partial data for non-terminal states.

---

## Metafield dependency

The three metafield definitions are declared in **`shopify.app.toml`** (repo root):

```toml
[product.metafields.app.gpsr_status]
name = "GPSR Compliance Status"
type = "single_line_text_field"

[product.metafields.app.gpsr_entity_json]
name = "GPSR Responsible Entity"
type = "json"

[product.metafields.app.gpsr_warnings_json]
name = "GPSR Safety Warnings"
type = "json"
```

These definitions are registered automatically by `shopify app dev` during development
and by `shopify app deploy` in production (F9). The extension TOML does not redeclare
them — the block just reads `product.metafields.app.*` in Liquid.

---

## Adding the block to a theme (development)

1. **Run the app in dev mode:**
   ```
   shopify app dev
   ```
   The CLI tunnels the backend and registers the extension with a connected dev store.

2. **Open the theme editor** in the dev store:
   Shopify Admin → Online Store → Themes → Customize.

3. **Navigate to a product page template.**

4. **Add the section block:**
   In the left sidebar, click "+ Add section" (or "+ Add block" if adding to an
   existing section), search for "GPSR Safety Block", and add it to the product page.

5. **Configure the block settings** in the sidebar:
   - **Section heading** — default "Product Safety Information"
   - **Show responsible operator** — checkbox (default: on)
   - **Show safety warnings** — checkbox (default: on)

6. **Run classification in the GPSR admin** for a test product so the metafields are
   populated. The block renders entity + warnings once `gpsr_status` is `"ok"` or
   `"override"`.

---

## XSS posture

- All metafield outputs use `| escape` (defense-in-depth on top of Liquid's auto-
  escaping). The `ai-security-review` gate greps `blocks/*.liquid` for `| raw`
  and rejects any match.
- No JavaScript in the block (MVP). The `innerHTML` XSS surface does not exist.
- If JS is added in future: use `element.textContent`, never `element.innerHTML`,
  for any value derived from metafields or settings.

---

## Verification

- **Liquid logic trace** and escape audit: see report in the F7 build output.
- **Security grep** (`| raw` absent): run `grep -r '| raw' storefront/` and
  `grep -r 'innerHTML' storefront/` — both must return empty.
- **E2E (Playwright):** deferred to F9 (deployed dev-store environment). The E2E suite
  covers: `ok` renders, `needs_review` hides, `override` renders, XSS injection escapes.
- **init.sh:** `storefront/` contains only static files; it does not affect the Go or
  Node build steps in `init.sh`. Running `bash init.sh` from the repo root continues
  to end HARNESS XANH.
