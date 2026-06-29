// Package dbtest holds shared helpers for DB-backed test suites so the
// skip-versus-fail policy for an unreachable MySQL lives in exactly one place.
//
// Why this exists: the repository and migrate suites both gate themselves on a
// live MySQL (port 3308). When the DB is genuinely absent (offline local dev,
// no docker) skipping keeps the unit suite portable. But a plain
// `go test ./...` with config.Load() defaulting DB_PASSWORD to empty would
// connect-fail and SKIP the DB tier — including the SQL-injection guard — while
// still reporting `ok`/exit 0. That is false-green: the canonical run lies.
//
// The opt-in environment variable GPSR_DB_TESTS=1 (set by CI and init.sh) flips
// the policy: when it is set, a connection/auth/ping failure is a hard FAILURE,
// not a skip, so the DB tier is guaranteed to execute. When it is NOT set, an
// unreachable DB skips (acceptable for offline local dev). Pick the mechanism
// once, here, and both suites stay consistent.
package dbtest

import (
	"os"
	"testing"
)

// EnvVar is the opt-in switch that forces DB-backed tests to run (and to fail
// loudly instead of skipping) whenever the database is unreachable.
const EnvVar = "GPSR_DB_TESTS"

// Required reports whether DB-backed tests are mandatory for this run.
// It is true when GPSR_DB_TESTS is set to a non-empty value.
func Required() bool {
	return os.Getenv(EnvVar) != ""
}

// SkipOrFail handles an inability to reach/use the database in a DB-backed test.
// It NEVER returns: it calls t.Fatalf when the DB tier is required (CI/init.sh)
// so a broken DB cannot masquerade as a skip, and t.Skipf otherwise so offline
// local dev stays portable. format/args describe the underlying failure.
func SkipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if Required() {
		t.Fatalf("DB-backed tests required ("+EnvVar+"=1) but database unusable: "+format, args...)
	}
	t.Skipf("skipping DB-backed test (set "+EnvVar+"=1 to require): "+format, args...)
}
