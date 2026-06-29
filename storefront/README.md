# storefront/ — Shopify theme app extension(s)

The storefront safety block lives here as a Shopify **theme app extension**, managed
by the Shopify CLI together with the app config at the repo-root `shopify.app.toml`.

```
storefront/
  extensions/
    safety-block/            # the GPSR storefront safety block (built in F7)
      shopify.extension.toml # extension config (CLI-managed)
      blocks/                # Liquid block(s) merchants add to the product page
      assets/                # CSS/JS for the block
```

- The extension renders the mapped warnings + responsible economic operator on the
  product page, reading the per-product compliance data the backend exposes.
- Built and verified in **F7** (high risk: E2E + human approval). This directory is
  the scaffold; `extensions/` is created by `shopify app generate extension`.
- Storefront/merchant-supplied input is untrusted — escape everything rendered
  (XSS), per AGENTS.md and the `ai-security-review` skill.
