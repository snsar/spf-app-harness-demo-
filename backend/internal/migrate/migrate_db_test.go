package migrate_test

import (
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/config"
	"github.com/gpsr/backend/internal/dbtest"
	"github.com/gpsr/backend/internal/migrate"
)

// openTestDB connects to the project MySQL (port 3308). When the DB is
// unreachable the test skips, so the unit suite stays portable; but with
// GPSR_DB_TESTS=1 set (CI/init.sh) an unreachable DB is a hard failure, so this
// migrate round-trip must execute rather than silently skip.
//
// This suite's round-trip drops every table on its DOWN leg. Go runs package
// test binaries in parallel, so without coordination that teardown races other
// DB-backed suites (e.g. repository) reading the same schema. It acquires a
// shared MySQL named lock — matching the repository suite's lock name — held for
// the test's duration, so DB suites serialize regardless of `go test`
// parallelism flags.
const dbTestLock = "gpsr_schema_test_lock"

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	cfg := config.Load()
	db, err := sql.Open("mysql", cfg.MySQLDSN())
	if err != nil {
		dbtest.SkipOrFail(t, "open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		dbtest.SkipOrFail(t, "MySQL not reachable on %s:%s: %v", cfg.DBHost, cfg.DBPort, err)
	}

	// Pin one connection so the named lock stays owned for the whole test.
	db.SetMaxOpenConns(1)
	var got sql.NullInt64
	if err := db.QueryRow("SELECT GET_LOCK(?, 30)", dbTestLock).Scan(&got); err != nil {
		db.Close()
		t.Fatalf("acquire test lock: %v", err)
	}
	if !got.Valid || got.Int64 != 1 {
		db.Close()
		t.Fatalf("could not obtain test lock %q (timeout)", dbTestLock)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("SELECT RELEASE_LOCK(?)", dbTestLock)
		db.Close()
	})
	return db
}

// TestUpDownRoundTrip applies the real migrations, asserts the expected tables
// exist, rolls back the latest migration and re-applies it cleanly — the F1
// reversibility + idempotency contract.
func TestUpDownRoundTrip(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	const dir = "../../migrations"

	// Ensure a clean slate: roll everything back first (ignore "nothing to do").
	for {
		v, err := migrate.Down(db, dir)
		if err != nil {
			t.Fatalf("pre-clean down: %v", err)
		}
		if v == 0 {
			break
		}
	}

	// UP — apply all migrations.
	applied, err := migrate.Up(db, dir)
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if len(applied) == 0 {
		t.Fatal("up applied nothing; expected the initial schema")
	}

	wantTables := []string{
		"product", "entity", "warning_template", "classification_rule",
		"rule_warning_templates", "compliance_record", "compliance_record_warnings",
	}
	for _, tbl := range wantTables {
		if !tableExists(t, db, tbl) {
			t.Errorf("expected table %q to exist after up", tbl)
		}
	}

	// Idempotency: a second Up applies nothing.
	again, err := migrate.Up(db, dir)
	if err != nil {
		t.Fatalf("second up: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second up should be a no-op, applied %v", again)
	}

	// DOWN — roll back the latest migration, then UP again must succeed.
	v, err := migrate.Down(db, dir)
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	if v == 0 {
		t.Fatal("down reverted nothing; expected to revert the latest migration")
	}
	if _, err := migrate.Up(db, dir); err != nil {
		t.Fatalf("re-up after down: %v", err)
	}
	for _, tbl := range wantTables {
		if !tableExists(t, db, tbl) {
			t.Errorf("table %q missing after down+up cycle", tbl)
		}
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
		name,
	).Scan(&n)
	if err != nil {
		t.Fatalf("check table %q: %v", name, err)
	}
	return n > 0
}
