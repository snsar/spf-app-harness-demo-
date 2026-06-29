#!/usr/bin/env bash
# Local dev setup for GPSR on gpsr.quotesnap.local (reuses the quotesnap nginx pattern).
# Run from repo root WITH SUDO:  sudo bash deploy/setup-local.sh
#
# What it does (idempotent):
#   1. mkcert cert for gpsr.quotesnap.local -> /etc/nginx/ssl/gpsr.local.{crt,key}
#   2. installs deploy/nginx/gpsr.quotesnap.local.conf into sites-available + enabled
#   3. adds "127.0.0.1 gpsr.quotesnap.local" to /etc/hosts if missing
#   4. nginx -t && reload
#
# NOTE: /etc/hosts is local-only — Shopify servers cannot reach this domain, so the
# Shopify-initiated OAuth callback/webhooks will not arrive. Browser-initiated flows
# (you opening https://gpsr.quotesnap.local/auth?shop=...) work. For a real Install
# callback from Shopify, use `shopify app dev` (tunnel) or deploy to a public domain.
set -euo pipefail

DOMAIN="gpsr.quotesnap.local"
SSL_DIR="/etc/nginx/ssl"
CONF_SRC="$(cd "$(dirname "$0")" && pwd)/nginx/${DOMAIN}.conf"
CONF_DST="/etc/nginx/sites-available/${DOMAIN}.conf"

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run with sudo: sudo bash deploy/setup-local.sh" >&2
  exit 1
fi

echo "[1/4] mkcert cert for ${DOMAIN}"
mkdir -p "$SSL_DIR"
if [ ! -f "${SSL_DIR}/gpsr.local.crt" ]; then
  mkcert -install >/dev/null 2>&1 || true
  mkcert -cert-file "${SSL_DIR}/gpsr.local.crt" \
         -key-file  "${SSL_DIR}/gpsr.local.key" "$DOMAIN"
  echo "  created ${SSL_DIR}/gpsr.local.{crt,key}"
else
  echo "  cert already present, skipping"
fi

echo "[2/4] install nginx site"
cp "$CONF_SRC" "$CONF_DST"
ln -sf "$CONF_DST" "/etc/nginx/sites-enabled/${DOMAIN}.conf"
echo "  -> $CONF_DST"

echo "[3/4] /etc/hosts entry"
if ! grep -q "$DOMAIN" /etc/hosts; then
  printf '127.0.0.1 %s\n' "$DOMAIN" >> /etc/hosts
  echo "  added 127.0.0.1 $DOMAIN"
else
  echo "  already in /etc/hosts, skipping"
fi

echo "[4/4] nginx test + reload"
nginx -t
systemctl reload nginx 2>/dev/null || nginx -s reload
echo "DONE. Open: https://${DOMAIN}/  (frontend)  |  https://${DOMAIN}/healthz (backend)"
