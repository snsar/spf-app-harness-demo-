// Package migrate is a minimal, dependency-light SQL migration runner for the
// GPSR Compliance Engine. It applies plain SQL files
// (migrations/NNN_name.up.sql + .down.sql) against MySQL on port 3308, tracking
// applied versions in a schema_migrations table so runs are idempotent.
//
// A purpose-built Go runner is used (rather than the golang-migrate CLI) because
// the harness host has the Go toolchain but no `migrate` binary, and init.sh's
// backend block already drives Go commands. Schema changes still live only as
// versioned SQL files — this runner never edits a live schema by hand.
package migrate

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Migration is a single up/down SQL pair identified by a numeric version.
type Migration struct {
	Version int
	Name    string // file stem, e.g. "001_initial_schema"
	Up      string
	Down    string
}

// Load discovers *.up.sql / *.down.sql pairs in dir, parses the leading numeric
// version (NNN_), and returns them sorted ascending by version. Every up file
// must have a matching down file (reversibility contract); otherwise it errors.
func Load(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %q: %w", dir, err)
	}

	type pair struct {
		name string
		up   string
		down string
	}
	pairs := map[int]*pair{}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fn := e.Name()
		var suffix string
		switch {
		case strings.HasSuffix(fn, ".up.sql"):
			suffix = ".up.sql"
		case strings.HasSuffix(fn, ".down.sql"):
			suffix = ".down.sql"
		default:
			continue // ignore README.md and other noise
		}
		stem := strings.TrimSuffix(fn, suffix)
		version, err := parseVersion(stem)
		if err != nil {
			return nil, fmt.Errorf("migration %q: %w", fn, err)
		}
		body, err := os.ReadFile(filepath.Join(dir, fn))
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", fn, err)
		}
		p := pairs[version]
		if p == nil {
			p = &pair{name: stem}
			pairs[version] = p
		}
		if suffix == ".up.sql" {
			p.up = string(body)
		} else {
			p.down = string(body)
		}
	}

	versions := make([]int, 0, len(pairs))
	for v := range pairs {
		versions = append(versions, v)
	}
	sort.Ints(versions)

	migs := make([]Migration, 0, len(versions))
	for _, v := range versions {
		p := pairs[v]
		if strings.TrimSpace(p.up) == "" {
			return nil, fmt.Errorf("migration version %d (%s): missing or empty .up.sql", v, p.name)
		}
		if strings.TrimSpace(p.down) == "" {
			return nil, fmt.Errorf("migration version %d (%s): missing or empty .down.sql", v, p.name)
		}
		migs = append(migs, Migration{Version: v, Name: p.name, Up: p.up, Down: p.down})
	}
	return migs, nil
}

// parseVersion extracts the integer before the first underscore in a file stem.
func parseVersion(stem string) (int, error) {
	idx := strings.IndexByte(stem, '_')
	if idx <= 0 {
		return 0, fmt.Errorf("name %q must start with NNN_ version prefix", stem)
	}
	v, err := strconv.Atoi(stem[:idx])
	if err != nil {
		return 0, fmt.Errorf("name %q has non-numeric version prefix: %w", stem, err)
	}
	return v, nil
}

// splitStatements splits a SQL file into individual statements on semicolons.
// Line comments (`--`) are stripped FIRST so a semicolon inside a comment never
// terminates a statement. Blank statements are dropped. The MySQL driver
// executes one statement per Exec, so multi-statement files must be split first.
func splitStatements(sqlText string) []string {
	clean := stripComments(sqlText)
	raw := strings.Split(clean, ";")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// stripComments removes `--` line comments (full-line and trailing) from SQL.
// It operates on the whole text before statement splitting. String-literal
// awareness is not needed here: GPSR migrations contain no `--` inside literals.
func stripComments(sqlText string) string {
	lines := strings.Split(sqlText, "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		if idx := strings.Index(ln, "--"); idx >= 0 {
			ln = ln[:idx]
		}
		kept = append(kept, ln)
	}
	return strings.Join(kept, "\n")
}

// ensureVersionTable creates the bookkeeping table if absent.
func ensureVersionTable(db *sql.DB) error {
	const ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT NOT NULL PRIMARY KEY,
		name    VARCHAR(255) NOT NULL,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

// appliedVersions returns the set of versions already recorded as applied.
func appliedVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()
	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// Up applies every not-yet-applied migration in ascending version order. It is
// idempotent: already-applied versions are skipped. Returns the versions newly
// applied during this call.
func Up(db *sql.DB, dir string) ([]int, error) {
	migs, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if err := ensureVersionTable(db); err != nil {
		return nil, err
	}
	applied, err := appliedVersions(db)
	if err != nil {
		return nil, err
	}

	var done []int
	for _, m := range migs {
		if applied[m.Version] {
			continue
		}
		if err := execAll(db, m.Up); err != nil {
			return done, fmt.Errorf("apply up %s: %w", m.Name, err)
		}
		if _, err := db.Exec("INSERT INTO schema_migrations (version, name) VALUES (?, ?)", m.Version, m.Name); err != nil {
			return done, fmt.Errorf("record version %d: %w", m.Version, err)
		}
		done = append(done, m.Version)
	}
	return done, nil
}

// Down rolls back the single highest applied migration. Returns the version
// reverted, or 0 if nothing was applied.
func Down(db *sql.DB, dir string) (int, error) {
	migs, err := Load(dir)
	if err != nil {
		return 0, err
	}
	if err := ensureVersionTable(db); err != nil {
		return 0, err
	}
	applied, err := appliedVersions(db)
	if err != nil {
		return 0, err
	}

	// Find the highest applied version.
	target := -1
	for i := len(migs) - 1; i >= 0; i-- {
		if applied[migs[i].Version] {
			target = i
			break
		}
	}
	if target < 0 {
		return 0, nil
	}
	m := migs[target]
	if err := execAll(db, m.Down); err != nil {
		return 0, fmt.Errorf("apply down %s: %w", m.Name, err)
	}
	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", m.Version); err != nil {
		return 0, fmt.Errorf("remove version %d: %w", m.Version, err)
	}
	return m.Version, nil
}

// execAll runs each statement in a SQL file as a separate Exec.
func execAll(db *sql.DB, sqlText string) error {
	for _, stmt := range splitStatements(sqlText) {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", strings.TrimSpace(firstLine(stmt)), err)
		}
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
