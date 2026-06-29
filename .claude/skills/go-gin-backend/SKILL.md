---
name: go-gin-backend
description: Conventions and workflow for the Go + Gin backend of the GPSR Compliance Engine, backed by MySQL on port 3308. Use for any Go code, Gin route/handler, service/repository layer, database migration, or backend config task. Triggers on "add an endpoint", "write the handler", "create a migration", "the Go service", "backend for feature X", and on re-work like "fix the API", "update the migration".
---

# Go + Gin Backend (GPSR)

Build the backend in `backend/` following a thin-handler, service-centric layout. The
GPSR classification logic itself lives in the `gpsr-rules-engine` skill — call it from
the service layer; keep this skill for plumbing.

## Project Layout
```
backend/
├── cmd/server/main.go        # entrypoint, wires Gin + DB
├── internal/
│   ├── handler/              # HTTP: bind, validate, call service, render JSON
│   ├── service/              # business logic (calls rules engine)
│   ├── repository/           # DB access only, no business rules
│   └── model/                # structs + JSON tags
├── migrations/               # versioned SQL, forward + rollback
└── config/                   # env-driven config (DB DSN, port)
```

## Database — MySQL on port 3308
- DSN comes from env (`.env`, never committed); `.env.example` documents keys.
- **Never** change the DB name or the port (3308). Never edit schema live.
- Every schema change is a numbered migration with up + down. Migrations run from
  `init.sh`; do not bypass it.

## Layer Rules (why: testability + clear boundaries)
- **handler** — bind request, validate, call one service method, map result/error to
  HTTP. No business logic, no SQL.
- **service** — owns business decisions; for classification, delegates to the rules
  engine. Returns domain errors, not HTTP codes.
- **repository** — SQL/queries only. Returns models or errors. No rules.
- A handler must never import the repository directly — go through the service.

## API Contract Discipline
- Define request/response as `model` structs with explicit JSON tags. Pick **one**
  casing convention (snake_case in JSON is the default here) and never mix.
- When you add/change an endpoint, publish the exact request + response JSON shape so
  frontend-engineer and qa-integration can match it. A silent shape change is the
  single most common cross-boundary bug — push it, don't let it be discovered.

## Errors
- Wrap with context (`fmt.Errorf("...: %w", err)`). Handlers translate domain errors to
  status codes in one place. Never swallow an error to return a fake-success fallback.

## Testing (TDD Iron Law)
- Write a failing test first. Service logic gets unit tests; handlers get
  httptest-based tests; repositories get tests against a test schema.
- DONE = paste real `go test ./...` and `go build ./...` output as evidence.

## References
- GPSR classification logic → use the `gpsr-rules-engine` skill.
- Test tier selection → use the `verification-ladder` skill.
