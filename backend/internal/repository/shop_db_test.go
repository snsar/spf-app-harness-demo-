package repository_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/config"
	"github.com/gpsr/backend/internal/dbtest"
	"github.com/gpsr/backend/internal/migrate"
	"github.com/gpsr/backend/internal/repository"
)

// openShopTestDB mirrors the compliance suite: gate on a live MySQL (port 3308)
// via dbtest.SkipOrFail, serialize DB suites with the shared named lock, and
// ensure the schema is applied so the `shop` table exists.
func openShopTestDB(t *testing.T) *sql.DB {
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
	db.SetMaxOpenConns(1)
	var got sql.NullInt64
	if err := db.QueryRow("SELECT GET_LOCK(?, 30)", "gpsr_schema_test_lock").Scan(&got); err != nil {
		db.Close()
		t.Fatalf("acquire test lock: %v", err)
	}
	if !got.Valid || got.Int64 != 1 {
		db.Close()
		t.Fatalf("could not obtain test lock (timeout)")
	}
	if _, err := migrate.Up(db, "../../migrations"); err != nil {
		_, _ = db.Exec("SELECT RELEASE_LOCK(?)", "gpsr_schema_test_lock")
		db.Close()
		t.Fatalf("migrate up: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("SELECT RELEASE_LOCK(?)", "gpsr_schema_test_lock")
		db.Close()
	})
	return db
}

func TestShopRepo_UpsertAndGet(t *testing.T) {
	db := openShopTestDB(t)
	ctx := context.Background()
	repo := repository.NewShopRepository(db)

	const domain = "repo-test-shop.myshopify.com"
	_, _ = db.ExecContext(ctx, "DELETE FROM shop WHERE shop_domain = ?", domain)

	// Insert.
	if err := repo.Upsert(ctx, domain, "shpat_token_1", "read_products"); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	got, err := repo.GetByDomain(ctx, domain)
	if err != nil {
		t.Fatalf("get after insert: %v", err)
	}
	if got == nil || got.ShopDomain != domain {
		t.Fatalf("got = %+v, want domain %q", got, domain)
	}
	if got.AccessToken != "shpat_token_1" || got.Scope != "read_products" {
		t.Fatalf("token/scope mismatch: %+v", got)
	}
	firstID := got.ID

	// Re-install: upsert must update in place (same row), not duplicate.
	if err := repo.Upsert(ctx, domain, "shpat_token_2", "read_products,write_products"); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got2, err := repo.GetByDomain(ctx, domain)
	if err != nil {
		t.Fatalf("get after re-upsert: %v", err)
	}
	if got2.ID != firstID {
		t.Fatalf("re-install created a new row: id %d -> %d", firstID, got2.ID)
	}
	if got2.AccessToken != "shpat_token_2" || got2.Scope != "read_products,write_products" {
		t.Fatalf("re-install did not refresh token/scope: %+v", got2)
	}

	// Cleanup.
	_, _ = db.ExecContext(ctx, "DELETE FROM shop WHERE shop_domain = ?", domain)
}

func TestShopRepo_GetByDomain_Absent(t *testing.T) {
	db := openShopTestDB(t)
	repo := repository.NewShopRepository(db)
	got, err := repo.GetByDomain(context.Background(), "no-such-shop-xyz.myshopify.com")
	if err != nil {
		t.Fatalf("absent lookup should not error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for absent shop, got %+v", got)
	}
}
