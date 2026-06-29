# Migrations

Versioned SQL schema changes for the GPSR Compliance Engine (MySQL on port 3308).

- Every change is a numbered migration with a forward (`up`) and rollback (`down`).
- Never edit a live schema by hand; never change the DB name or the port (3308).
- Migrations are run from the repo-root `init.sh`; do not bypass it.

Placeholder — actual migrations land in feature F1 (data model).
