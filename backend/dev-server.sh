#!/usr/bin/env bash
# Launch the Go backend under `shopify app dev`.
# The Shopify CLI provides SHOPIFY_API_KEY/SECRET, APP_URL/HOST (the tunnel URL),
# PORT, SCOPES — but NOT the DB credentials. This script loads ONLY the DB_* vars
# from the repo-root .env so MySQL auth works, without overriding the CLI's tunnel
# APP_URL/PORT (we export just DB_* — SHOPIFY_*/APP_URL from .env are ignored).
set -e

ENV_FILE="$(cd "$(dirname "$0")/.." && pwd)/.env"
if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . <(grep -E '^DB_' "$ENV_FILE")
  set +a
fi

exec go run ./cmd/server
