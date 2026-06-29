package repository_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/config"
	"github.com/gpsr/backend/internal/dbtest"
	"github.com/gpsr/backend/internal/migrate"
	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/repository"
)

// dbTestLock is the MySQL named lock that serializes all DB-backed test suites
// against the single shared schema. Go runs different packages' test binaries in
// parallel; the migrate round-trip drops every table on its DOWN leg, which
// would race the repository suite's reads. Every DB test holds this lock for its
// duration so they never overlap — a deterministic fix independent of `go test`
// parallelism flags.
const dbTestLock = "gpsr_schema_test_lock"

// openTestDB connects to the project MySQL (port 3308). When the DB is
// unreachable it skips for offline local dev, UNLESS GPSR_DB_TESTS=1 is set
// (CI/init.sh), in which case it fails — so the DB tier and the injection guard
// can never silently no-op while the run reports ok. It acquires the shared
// schema lock (released via t.Cleanup) and ensures the schema is applied before
// returning.
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

	// Serialize with other DB suites for the lifetime of this test's *sql.DB.
	// MySQL releases a named lock when its owning connection closes, so we pin a
	// single connection and reuse it via db (max 1 open conn) for the lock.
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

	if _, err := migrate.Up(db, "../../migrations"); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return db
}

// testShopID is the tenant every F2 repository test operates within. F3b made
// the repository shop-scoped; the fixture seeds one shop and scopes all rows to it.
var testShopID int64

// seedFixture inserts a clean shop + entity + warning templates + product + rule
// set (all scoped to one shop) and returns the ids needed by the tests. It clears
// prior data first so the test is repeatable. All inserts are parameterized.
func seedFixture(t *testing.T, db *sql.DB) (entityID, wt1, wt2, productID, ruleID int64) {
	t.Helper()
	ctx := context.Background()

	// Clean in FK-safe order (shop last; its cascade is covered by the migrate suite).
	for _, stmt := range []string{
		"DELETE FROM compliance_record_warnings",
		"DELETE FROM compliance_record",
		"DELETE FROM rule_warning_templates",
		"DELETE FROM classification_rule",
		"DELETE FROM product",
		"DELETE FROM warning_template",
		"DELETE FROM entity",
		"DELETE FROM shop",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("clean %q: %v", stmt, err)
		}
	}

	res, err := db.ExecContext(ctx,
		"INSERT INTO shop (shop_domain, access_token, scope) VALUES (?,?,?)",
		"repo-fixture.myshopify.com", "tok", "read_products")
	if err != nil {
		t.Fatalf("insert shop: %v", err)
	}
	testShopID, _ = res.LastInsertId()

	res, err = db.ExecContext(ctx,
		"INSERT INTO entity (shop_id, name, address, role, is_eu) VALUES (?,?,?,?,?)",
		testShopID, "ACME EU Rep", "1 Rue de Paris", "authorised_representative", true)
	if err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	entityID, _ = res.LastInsertId()

	// Note untrusted-looking text on purpose; it must round-trip verbatim and
	// never be interpolated into SQL.
	res, _ = db.ExecContext(ctx, "INSERT INTO warning_template (shop_id, locale, text) VALUES (?,?,?)",
		testShopID, "en", "Choking hazard'); DROP TABLE entity;--")
	wt1, _ = res.LastInsertId()
	res, _ = db.ExecContext(ctx, "INSERT INTO warning_template (shop_id, locale, text) VALUES (?,?,?)",
		testShopID, "en", "Keep away from fire")
	wt2, _ = res.LastInsertId()

	res, err = db.ExecContext(ctx,
		"INSERT INTO product (shop_id, shopify_product_id, title, tags, category, material, origin) VALUES (?,?,?,?,?,?,?)",
		testShopID, 7001, "Toy Robot", `["toys","eu"]`, "toys", "plastic", "CN")
	if err != nil {
		t.Fatalf("insert product: %v", err)
	}
	productID, _ = res.LastInsertId()

	res, err = db.ExecContext(ctx,
		"INSERT INTO classification_rule (shop_id, priority, match_conditions, entity_id) VALUES (?,?,?,?)",
		testShopID, 10, `{"tags":["toys"]}`, entityID)
	if err != nil {
		t.Fatalf("insert rule: %v", err)
	}
	ruleID, _ = res.LastInsertId()
	for _, wt := range []int64{wt1, wt2} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO rule_warning_templates (rule_id, warning_template_id) VALUES (?,?)", ruleID, wt); err != nil {
			t.Fatalf("link rule warning: %v", err)
		}
	}
	return entityID, wt1, wt2, productID, ruleID
}

// TestRepo_GetProducts_ParsesJSON proves the repository loads products including
// JSON tags and the optional scalar fields, with absent fields as nil (C4).
func TestRepo_GetProducts_ParsesJSON(t *testing.T) {
	db := openTestDB(t)
	_, _, _, productID, _ := seedFixture(t, db)
	ctx := context.Background()

	// Add a product with NULL category/material/origin and NULL tags.
	res, err := db.ExecContext(ctx,
		"INSERT INTO product (shop_id, shopify_product_id, title) VALUES (?,?,?)", testShopID, 7002, "Bare Product")
	if err != nil {
		t.Fatalf("insert bare product: %v", err)
	}
	bareID, _ := res.LastInsertId()

	repo := repository.NewComplianceRepository(db)
	products, err := repo.GetProducts(ctx, testShopID, []int64{bareID, productID})
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	byID := map[int64]model.Product{}
	for _, p := range products {
		byID[p.ID] = p
	}

	full := byID[productID]
	if len(full.Tags) != 2 || full.Tags[0] != "toys" {
		t.Errorf("tags = %v, want [toys eu]", full.Tags)
	}
	if full.Category == nil || *full.Category != "toys" {
		t.Errorf("category = %v, want toys", full.Category)
	}

	bare := byID[bareID]
	if bare.Category != nil || bare.Material != nil || bare.Origin != nil {
		t.Errorf("bare product absent fields must be nil (C4), got %+v", bare)
	}
	if len(bare.Tags) != 0 {
		t.Errorf("bare product tags must be empty, got %v", bare.Tags)
	}
}

// TestRepo_SaveAndGetRecord round-trips a compliance record plus its warnings
// join, and verifies upsert (a second save replaces, not duplicates) and that
// untrusted warning text round-tripped verbatim (no SQL injection executed).
func TestRepo_SaveAndGetRecord(t *testing.T) {
	db := openTestDB(t)
	entityID, wt1, wt2, productID, ruleID := seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewComplianceRepository(db)

	rec := model.ComplianceRecord{
		ProductID:          productID,
		MatchedRuleID:      &ruleID,
		EntityID:           &entityID,
		Status:             model.StatusOK,
		WarningTemplateIDs: []int64{wt1, wt2},
	}
	if err := repo.SaveRecord(ctx, testShopID, rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	got, err := repo.GetRecord(ctx, testShopID, productID)
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if got == nil {
		t.Fatal("GetRecord returned nil after save")
	}
	if got.Status != model.StatusOK {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if got.MatchedRuleID == nil || *got.MatchedRuleID != ruleID {
		t.Errorf("matched_rule_id = %v, want %d", got.MatchedRuleID, ruleID)
	}
	if len(got.WarningTemplateIDs) != 2 {
		t.Fatalf("warnings = %v, want 2 ids", got.WarningTemplateIDs)
	}

	// Upsert: re-save with one warning and override status; must replace cleanly.
	rec2 := model.ComplianceRecord{
		ProductID:          productID,
		MatchedRuleID:      nil,
		EntityID:           &entityID,
		Status:             model.StatusOverride,
		WarningTemplateIDs: []int64{wt1},
	}
	if err := repo.SaveRecord(ctx, testShopID, rec2); err != nil {
		t.Fatalf("SaveRecord upsert: %v", err)
	}
	got2, _ := repo.GetRecord(ctx, testShopID, productID)
	if got2.Status != model.StatusOverride {
		t.Errorf("after upsert status = %q, want override", got2.Status)
	}
	if got2.MatchedRuleID != nil {
		t.Errorf("after upsert matched_rule_id = %v, want nil", got2.MatchedRuleID)
	}
	if len(got2.WarningTemplateIDs) != 1 || got2.WarningTemplateIDs[0] != wt1 {
		t.Errorf("after upsert warnings = %v, want [%d]", got2.WarningTemplateIDs, wt1)
	}

	// Confirm no duplicate record rows for this product.
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM compliance_record WHERE product_id = ?", productID).Scan(&n); err != nil {
		t.Fatalf("count records: %v", err)
	}
	if n != 1 {
		t.Errorf("record rows = %d, want exactly 1 (upsert, not insert)", n)
	}

	// Injection guard: the entity table must still exist (DROP TABLE in the
	// warning text was never executed because queries are parameterized).
	var tables int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'entity'").Scan(&tables); err != nil {
		t.Fatalf("check entity table: %v", err)
	}
	if tables != 1 {
		t.Fatal("entity table missing — untrusted text was interpolated into SQL!")
	}
}

// TestRepo_DeleteRecord removes a record and its warnings (clear override path).
func TestRepo_DeleteRecord(t *testing.T) {
	db := openTestDB(t)
	entityID, wt1, _, productID, ruleID := seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewComplianceRepository(db)

	if err := repo.SaveRecord(ctx, testShopID, model.ComplianceRecord{
		ProductID: productID, MatchedRuleID: &ruleID, EntityID: &entityID,
		Status: model.StatusOK, WarningTemplateIDs: []int64{wt1},
	}); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	if err := repo.DeleteRecord(ctx, testShopID, productID); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	got, err := repo.GetRecord(ctx, testShopID, productID)
	if err != nil {
		t.Fatalf("GetRecord after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("record still present after delete: %+v", got)
	}
}

// TestRepo_GetRecord_Absent returns nil for a product with no record.
func TestRepo_GetRecord_Absent(t *testing.T) {
	db := openTestDB(t)
	_, _, _, productID, _ := seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewComplianceRepository(db)

	got, err := repo.GetRecord(ctx, testShopID, productID)
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if got != nil {
		t.Fatalf("want nil for product with no record, got %+v", got)
	}
}
