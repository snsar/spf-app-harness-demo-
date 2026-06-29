#!/usr/bin/env bash
# init.sh — full verify chain from a clean checkout. Goal: "HARNESS XANH" (green).
# Script/command names here MUST match package.json + Go commands 100%.
# Until the app is scaffolded (feature F0), the relevant steps are skipped with a
# clear message — never fake-green. Each real step prints what it ran (evidence).
set -euo pipefail

say() { printf '\n\033[1;36m== %s ==\033[0m\n' "$1"; }
ok()  { printf '\033[1;32m[ok]\033[0m %s\n' "$1"; }
skip(){ printf '\033[1;33m[skip]\033[0m %s\n' "$1"; }
die() { printf '\033[1;31m[fail]\033[0m %s\n' "$1" >&2; exit 1; }

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

# Load local DB/app env if present (never committed). docker-compose and the Go
# migration runner both read these; defaults mirror .env.example when absent.
if [ -f .env ]; then
  set -a; . ./.env; set +a
  ok ".env loaded"
fi

say "Toolchain"
command -v go      >/dev/null 2>&1 && ok "go: $(go version)"             || die "go not installed"
command -v node    >/dev/null 2>&1 && ok "node: $(node --version)"        || die "node not installed"
command -v docker  >/dev/null 2>&1 && ok "docker present"                 || die "docker not installed"

say "Database — MySQL on port 3308"
if [ -f docker-compose.yml ]; then
  docker compose up -d db
  # Wait on the container's own healthcheck (MySQL is 3306 in-container, mapped to host 3308).
  for i in $(seq 1 40); do
    status=$(docker inspect -f '{{.State.Health.Status}}' gpsr-mysql 2>/dev/null || echo "starting")
    [ "$status" = "healthy" ] && { ok "MySQL healthy (host port 3308)"; break; }
    [ "$i" -eq 40 ] && die "MySQL did not become healthy (host 3308)"
    sleep 2
  done
else
  skip "docker-compose.yml not present yet (feature F0). Skipping DB."
fi

say "Backend — Go + Gin"
if [ -f backend/go.mod ]; then
  ( cd backend
    [ -d migrations ] && ok "migrations dir present" || skip "no migrations yet"
    go vet ./...   && ok "go vet"
    go build ./... && ok "go build"
    # Run DB migrations after MySQL is healthy and before tests (idempotent).
    # Needs the DB; only run when docker-compose (and thus MySQL) is in play.
    if [ -f "$ROOT/docker-compose.yml" ]; then
      go run ./cmd/migrate up && ok "migrate:run (cmd/migrate up)"
      # DB is up: require the DB-backed tier (repository + migrate suites and the
      # SQL-injection guard) to actually RUN. With GPSR_DB_TESTS=1 an unreachable
      # or misconfigured DB FAILS the run instead of skipping — closes false-green.
      GPSR_DB_TESTS=1 go test ./...  && ok "go test (DB tier required: GPSR_DB_TESTS=1)"
    else
      skip "DB not running; skipping migrate:run"
      # No DB present: DB-backed tests skip by design (offline local-dev path).
      go test ./...  && ok "go test (DB tier skipped: no DB)"
    fi )
else
  skip "backend/go.mod not present yet (feature F0). Skipping backend."
fi

say "Frontend — React (embedded Shopify Admin)"
if [ -f frontend/package.json ]; then
  ( cd frontend
    npm ci
    npm run lint  --if-present && ok "lint"
    npm test      --if-present && ok "test"
    npm run build --if-present && ok "build" )
else
  skip "frontend/package.json not present yet (feature F0). Skipping frontend."
fi

say "Result"
if [ ! -f docker-compose.yml ] && [ ! -f backend/go.mod ] && [ ! -f frontend/package.json ]; then
  printf '\033[1;33mHarness bootstrapped, app not scaffolded yet. Run feature F0 next.\033[0m\n'
  printf 'This is expected pre-scaffold; it is NOT a green build.\n'
  exit 0
fi
printf '\033[1;32mHARNESS XANH — verify chain passed.\033[0m\n'
