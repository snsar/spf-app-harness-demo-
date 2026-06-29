package migrate

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMigration is a helper that writes a pair of up/down SQL files into dir.
func writeMigration(t *testing.T, dir, name, up, down string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".up.sql"), []byte(up), 0o644); err != nil {
		t.Fatalf("write up: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".down.sql"), []byte(down), 0o644); err != nil {
		t.Fatalf("write down: %v", err)
	}
}

// TestLoadMigrations verifies that up/down pairs are discovered, parsed and
// ordered by their numeric version prefix regardless of filesystem order.
func TestLoadMigrations(t *testing.T) {
	dir := t.TempDir()
	// Intentionally write out of order to prove sorting by version.
	writeMigration(t, dir, "002_second", "CREATE TABLE b (id INT);", "DROP TABLE b;")
	writeMigration(t, dir, "001_first", "CREATE TABLE a (id INT);", "DROP TABLE a;")
	// A stray non-sql file must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	migs, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(migs) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(migs))
	}
	if migs[0].Version != 1 || migs[1].Version != 2 {
		t.Fatalf("migrations not sorted by version: got %d then %d", migs[0].Version, migs[1].Version)
	}
	if migs[0].Name != "001_first" {
		t.Fatalf("unexpected name: %q", migs[0].Name)
	}
	if migs[0].Up == "" || migs[0].Down == "" {
		t.Fatalf("up/down not loaded for first migration")
	}
}

// TestLoadMissingDown fails when an up file has no matching down file, because a
// migration that cannot be rolled back violates the F1 reversibility contract.
func TestLoadMissingDown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "001_only_up.up.sql"), []byte("CREATE TABLE a (id INT);"), 0o644); err != nil {
		t.Fatalf("write up: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error for missing down file, got nil")
	}
}

// TestSplitStatements ensures multi-statement SQL files are split so the driver
// (which executes one statement per Exec) applies every statement.
func TestSplitStatements(t *testing.T) {
	in := "CREATE TABLE a (id INT);\n-- a comment\nCREATE TABLE b (id INT);\n"
	got := splitStatements(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(got), got)
	}
}

// TestSplitStatementsSemicolonInComment is a regression test: a semicolon inside
// a `--` line comment must NOT terminate a statement. Comments are stripped
// before splitting on ';'.
func TestSplitStatementsSemicolonInComment(t *testing.T) {
	in := "CREATE TABLE a (\n  id INT -- lower wins; first match\n);\n"
	got := splitStatements(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 statement (comment semicolon ignored), got %d: %#v", len(got), got)
	}
}
