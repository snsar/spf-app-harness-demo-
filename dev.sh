#!/usr/bin/env bash
# dev.sh — run the whole GPSR stack locally in one command (NOT via `shopify app dev`).
#
#   bash dev.sh
#
# Starts (and cleans up together on Ctrl+C):
#   - MySQL (docker compose, port 3308) — waits until healthy
#   - Go backend on :8000  (loads .env for DB creds + Shopify secret + SHOPIFY_APP_URL)
#   - Vite frontend on :5173
#
# nginx (gpsr.quotesnap.local) proxies / → :5173 and /api,/auth,/webhooks → :8000;
# set it up once with: sudo bash deploy/setup-local.sh
#
# Logs are interleaved in this terminal, prefixed [backend] / [frontend].
set -euo pipefail
cd "$(dirname "$0")"
ROOT="$(pwd)"

echo "[dev] ensuring MySQL (docker) is up on 3308..."
docker compose up -d db >/dev/null 2>&1 || true
for i in $(seq 1 30); do
  hs="$(docker inspect -f '{{.State.Health.Status}}' gpsr-mysql 2>/dev/null || echo none)"
  [ "$hs" = "healthy" ] && { echo "[dev] MySQL healthy"; break; }
  sleep 1
done

# Load .env for the backend (DB creds, SHOPIFY_API_KEY/SECRET, SHOPIFY_APP_URL).
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
else
  echo "[dev] WARNING: .env not found — backend will fail-closed on missing Shopify secret" >&2
fi

pids=()
cleanup() {
  echo
  echo "[dev] shutting down..."
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  # also free the ports in case children re-spawned
  fuser -k 8000/tcp 5173/tcp 2>/dev/null || true
  exit 0
}
trap cleanup INT TERM

echo "[dev] starting backend on :8000..."
( cd "$ROOT/backend" && go run ./cmd/server 2>&1 | sed 's/^/[backend] /' ) &
pids+=($!)

echo "[dev] starting frontend on :5173..."
( cd "$ROOT/frontend" && npm run dev 2>&1 | sed 's/^/[frontend] /' ) &
pids+=($!)

echo "[dev] both running. Open https://gpsr.quotesnap.local/  (Ctrl+C to stop both)"
wait
