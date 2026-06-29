# GPSR Compliance Engine — Deploy Guide

This guide covers the full build and deploy sequence: local dev setup, production
build, database migration, nginx install, Shopify CLI deploy, and the live E2E
verification steps that require a real Shopify environment.

---

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.24+ | Backend build and test |
| Node.js | 22+ | Frontend build and Shopify CLI |
| npm | 10+ | Frontend dependency management |
| Docker | any | Local MySQL 8.4 on port 3308 |
| nginx | 1.24+ | Reverse proxy (local and prod) |
| Shopify CLI | 3.x | `shopify app dev` / `shopify app deploy` |
| mkcert | any | Local self-signed TLS (local setup only) |

Shopify CLI install: `npm install -g @shopify/cli @shopify/theme`.

---

## 1. Environment variables

Copy `.env.example` to `.env` and fill in the values:

```
SHOPIFY_API_KEY=<from Partners dashboard>
SHOPIFY_API_SECRET=<from Partners dashboard>
SHOPIFY_APP_URL=<public HTTPS URL, e.g. https://gpsr.quotesnap.local>
SHOPIFY_SCOPES=read_products,write_products
BACKEND_PORT=8000
DB_HOST=localhost
DB_PORT=3308
DB_NAME=gpsr
DB_USER=gpsr
DB_PASSWORD=<from docker-compose.yml or managed DB>
GIN_MODE=release   # use "debug" for local dev
```

The API secret is NEVER stored in `shopify.app.toml`. Only `.env` holds it.

---

## 2. Start the database

```bash
docker compose up -d db
# Wait for healthy:
docker inspect -f '{{.State.Health.Status}}' gpsr-mysql
# Expected: healthy
```

---

## 3. Run database migrations

```bash
cd backend
go run ./cmd/migrate up
```

Migrations are idempotent: re-running is a no-op if already current.
The `schema_migrations` table tracks applied versions.

---

## 4. Build the frontend

```bash
cd frontend
npm ci
npm run build
# Outputs to frontend/dist/
```

The dist/ directory is what nginx serves in production (see section 7).

---

## 5. Build and run the backend

```bash
cd backend
go build -o gpsr-backend ./cmd/server
# Run (env must be exported or in .env):
./gpsr-backend
# Or for development:
go run ./cmd/server
```

The backend listens on `BACKEND_PORT` (default 8000) on all interfaces.
Verify it is running: `curl http://localhost:8000/healthz` → `{"status":"ok"}`.

---

## 6. Run the full verify chain (init.sh)

Before deploying, confirm the full chain is green:

```bash
bash init.sh
# Expected final line: HARNESS XANH — verify chain passed.
```

This runs: go vet, go build, migrations, GPSR_DB_TESTS=1 go test ./... (DB tier
required), npm ci, lint, vitest, vite build.

---

## 7. nginx setup

### Local (dev domain: gpsr.quotesnap.local)

```bash
# 1. Generate a self-signed cert with mkcert:
mkcert -install
mkcert gpsr.quotesnap.local
# Move certs to /etc/nginx/ssl/ (create dir with sudo if needed):
sudo mkdir -p /etc/nginx/ssl
sudo cp gpsr.quotesnap.local.pem     /etc/nginx/ssl/gpsr.local.crt
sudo cp gpsr.quotesnap.local-key.pem /etc/nginx/ssl/gpsr.local.key

# 2. Install the site config:
sudo cp deploy/nginx/gpsr.quotesnap.local.conf /etc/nginx/sites-available/gpsr.quotesnap.local
sudo ln -sf /etc/nginx/sites-available/gpsr.quotesnap.local \
            /etc/nginx/sites-enabled/
sudo nginx -t && sudo nginx -s reload

# 3. Add to /etc/hosts (local machine only):
echo "127.0.0.1  gpsr.quotesnap.local" | sudo tee -a /etc/hosts
```

Or use `deploy/setup-local.sh` which automates steps 2-3.

**Dev mode vs prod mode in the nginx config:**

The config at `deploy/nginx/gpsr.quotesnap.local.conf` has two `location /`
blocks — only one should be active:

- **Dev mode (default):** `proxy_pass http://127.0.0.1:5173` — proxies to the
  Vite dev server. Start it with `cd frontend && npm run dev`.
- **Prod mode:** `root /path/to/repo/frontend/dist` + `try_files $uri $uri/ /index.html`
  — serves the pre-built `dist/` directly. To activate, comment out the proxy
  block and uncomment the `location /` prod block. Requires `npm run build` first.

For a real public domain, copy/adapt the nginx config with your domain name,
a real TLS certificate (Let's Encrypt: `certbot --nginx`), and the prod
`location /` block pointing to the absolute path of `frontend/dist/`.

### Nginx syntax check

```bash
sudo nginx -t
# Expected: nginx: configuration file ... syntax is ok
#           nginx: configuration file ... test is successful
```

---

## 8. Shopify app deploy

### Redirect URI

Before deploying, verify the redirect URI in `shopify.app.toml` matches the
backend's `/auth/callback` path:

```toml
[auth]
redirect_urls = ["https://<your-public-domain>/auth/callback"]
```

The F3 backend handler is mounted at `GET /auth/callback`. The redirect URI
**must** be registered in the Partners dashboard under the app's allowed redirect
URLs, and it **must** be a publicly reachable HTTPS URL.

### Deploy config + extension + webhooks

```bash
# From the repo root (where shopify.app.toml lives):
shopify app deploy
```

This command:
1. Pushes the app config (scopes, webhooks, metafield definitions).
2. Builds and deploys the theme app extension (`storefront/extensions/safety-block/`).
3. Registers the three webhook subscriptions:
   - `products/create` → `<app_url>/webhooks/products/create`
   - `products/update` → `<app_url>/webhooks/products/update`
   - `app/uninstalled` → `<app_url>/webhooks/app/uninstalled`

### Local dev with Shopify tunnel

For local dev where Shopify servers need to reach your machine (OAuth callbacks,
webhooks), use:

```bash
shopify app dev
```

This starts a public HTTPS tunnel, overwrites `application_url` and
`redirect_urls` in the toml with the tunnel URL, and starts the backend via
`backend/shopify.web.toml` (`go run ./cmd/server`).

**Important:** The `gpsr.quotesnap.local` domain in `shopify.app.toml` is a
local-only placeholder. It resolves only on this machine (`/etc/hosts`). Shopify's
servers cannot reach it for OAuth callbacks or webhook delivery. A tunnel
(`shopify app dev`) or a real public domain is required for those flows.

---

## 9. Click Install (live E2E gate)

After `shopify app deploy` or during `shopify app dev`, perform the live
verification steps that cannot be automated without a real Shopify environment:

1. **Install the app:** Open `https://<your-dev-store>.myshopify.com/admin` and
   navigate to Apps → install `gspr-harness`. Verify the OAuth flow completes and
   the app loads embedded.

2. **Compliance UI (F6):** In the app, open the Products screen. Apply the ruleset
   to a product with a matching rule — verify status changes to `ok`. Test
   `needs_review` (no matching rule) and `override` flows.

3. **Storefront safety block (F7):** Install the `gpsr-safety-block` theme block on
   a product page in your dev store theme. View a product with `ok` status — verify
   the entity and warnings render. View a `needs_review` product — verify nothing
   renders. Inject a `<script>` tag into a warning template text and confirm it
   renders as escaped text, not an executed script (XSS gate).

4. **Product webhooks:** Create or update a product in the Shopify admin. Verify
   the backend's compliance record updates to `needs_review` (the webhook handler
   marks it). Check the server log for the webhook hit.

5. **app/uninstalled webhook:** Uninstall the app from the dev store. Verify the
   shop row and all related data are removed from the database (MySQL teardown).

6. **SSRF / OAuth / webhook live checks:** Confirm the backend rejects requests with
   invalid shop domains, that the OAuth HMAC is verified on callbacks, and that
   webhook HMAC verification rejects tampered payloads.

These steps require interactive Shopify auth and a real dev store and cannot be
performed in automated CI without Shopify test credentials.

---

## 10. Local-only caveat

`gpsr.quotesnap.local` resolves only on this machine via `/etc/hosts`. It is
suitable for browser-initiated flows where the user's browser and the backend are
on the same machine. For Shopify-initiated flows (OAuth callback after merchant
clicks "Install", webhook delivery), Shopify's servers must be able to reach the
`application_url`. Options:

- **`shopify app dev`** (recommended for dev): creates a public HTTPS tunnel
  automatically.
- **Real public domain**: use a VPS or cloud host with a valid TLS certificate;
  update `application_url` and `redirect_urls` in the Partners dashboard and in
  `shopify.app.toml`.

---

## File reference

| File | Purpose |
|------|---------|
| `shopify.app.toml` | App config: client_id, scopes, webhooks, metafield defs |
| `backend/shopify.web.toml` | Shopify CLI backend process config (port, dev command) |
| `deploy/nginx/gpsr.quotesnap.local.conf` | nginx site config (local + prod variants) |
| `deploy/setup-local.sh` | Script to install nginx config and /etc/hosts entry |
| `backend/migrations/` | Numbered SQL migrations (up + down) |
| `backend/cmd/server/main.go` | Backend entrypoint |
| `backend/cmd/migrate/main.go` | Migration runner |
| `frontend/` | React admin (build → `frontend/dist/`) |
| `storefront/extensions/safety-block/` | Theme app extension |
| `.env.example` | Documents required environment variables |
