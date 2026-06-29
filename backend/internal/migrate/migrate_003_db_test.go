package migrate_test

import (
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/migrate"
)

// migration 003 introduces multi-tenant scoping. These DB-backed tests assert
// the shape (shop_id + FKs), per-shop uniqueness, reversibility, and the
// ON DELETE CASCADE that ties every domain row to its shop.

// applyAll rolls everything back to a clean slate then applies all migrations.
func applyAll(t *testing.T, db *sql.DB) {
	t.Helper()
	const dir = "../../migrations"
	// Ensure the schema is present before clearing rows (a prior suite may have
	// left it fully down). Up is idempotent.
	if _, err := migrate.Up(db, dir); err != nil {
		t.Fatalf("ensure up before clean: %v", err)
	}
	// Clear all domain + shop rows so the DOWN leg's PK/unique restoration is not
	// blocked by leftover multi-tenant fixture data (e.g. two shops sharing a
	// shopify_product_id, which collides when shopify_product_id becomes the PK).
	cleanDomain(t, db)
	for {
		v, err := migrate.Down(db, dir)
		if err != nil {
			t.Fatalf("pre-clean down: %v", err)
		}
		if v == 0 {
			break
		}
	}
	if _, err := migrate.Up(db, dir); err != nil {
		t.Fatalf("up: %v", err)
	}
}

// columnExists reports whether table.column exists in the current schema.
func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
		table, column,
	).Scan(&n); err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	return n > 0
}

// fkExists reports whether a FK from table.column references shop(id).
func fkToShopExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.key_column_usage
		 WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?
		   AND referenced_table_name = 'shop' AND referenced_column_name = 'id'`,
		table, column,
	).Scan(&n); err != nil {
		t.Fatalf("check fk %s.%s -> shop: %v", table, column, err)
	}
	return n > 0
}

// seedShop inserts a shop row and returns its id.
func seedShop(t *testing.T, db *sql.DB, domain string) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO shop (shop_domain, access_token, scope) VALUES (?,?,?)",
		domain, "tok", "read_products")
	if err != nil {
		t.Fatalf("seed shop %q: %v", domain, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// TestMigration003_AddsShopIdColumns asserts every domain table gains a NOT NULL
// shop_id column with a FK to shop(id), and product gains shopify_product_id.
func TestMigration003_AddsShopIdColumns(t *testing.T) {
	db := openTestDB(t)
	applyAll(t, db)

	tables := []string{"entity", "warning_template", "product", "classification_rule", "compliance_record"}
	for _, tbl := range tables {
		if !columnExists(t, db, tbl, "shop_id") {
			t.Errorf("table %q missing shop_id column", tbl)
		}
		if !fkToShopExists(t, db, tbl, "shop_id") {
			t.Errorf("table %q shop_id has no FK to shop(id)", tbl)
		}
	}
	// Q1 Option A: product gains a surrogate id PK + shopify_product_id column.
	if !columnExists(t, db, "product", "shopify_product_id") {
		t.Errorf("product missing shopify_product_id column (Q1 Option A)")
	}
}

// TestMigration003_PerShopUniqueness proves two shops can each mirror a product
// with the same Shopify product id, and each carry their own compliance record.
func TestMigration003_PerShopUniqueness(t *testing.T) {
	db := openTestDB(t)
	applyAll(t, db)

	cleanDomain(t, db)
	// Leave no cross-shop-duplicate shopify_product_id rows behind: they would
	// collide when a later suite's DOWN leg restores shopify_product_id as the PK.
	t.Cleanup(func() { cleanDomain(t, db) })
	shopA := seedShop(t, db, "a-uniq.myshopify.com")
	shopB := seedShop(t, db, "b-uniq.myshopify.com")

	const shopifyPID = int64(555000111)
	insProduct := func(shopID int64) int64 {
		t.Helper()
		res, err := db.Exec(
			"INSERT INTO product (shop_id, shopify_product_id, title) VALUES (?,?,?)",
			shopID, shopifyPID, "Same Shopify Product")
		if err != nil {
			t.Fatalf("insert product for shop %d: %v", shopID, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	pA := insProduct(shopA)
	pB := insProduct(shopB)
	if pA == pB {
		t.Fatalf("expected distinct surrogate product ids, got %d == %d", pA, pB)
	}

	// A second product with the same (shop_id, shopify_product_id) must violate
	// the per-shop unique key.
	if _, err := db.Exec(
		"INSERT INTO product (shop_id, shopify_product_id, title) VALUES (?,?,?)",
		shopA, shopifyPID, "Duplicate"); err == nil {
		t.Errorf("expected UNIQUE(shop_id, shopify_product_id) violation, got nil")
	}

	// Each shop gets its own compliance record per product (UNIQUE(shop_id, product_id)).
	entA := seedEntity(t, db, shopA)
	entB := seedEntity(t, db, shopB)
	if _, err := db.Exec(
		"INSERT INTO compliance_record (shop_id, product_id, entity_id, status) VALUES (?,?,?,?)",
		shopA, pA, entA, "needs_review"); err != nil {
		t.Fatalf("insert record A: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO compliance_record (shop_id, product_id, entity_id, status) VALUES (?,?,?,?)",
		shopB, pB, entB, "needs_review"); err != nil {
		t.Fatalf("insert record B: %v", err)
	}
	// Duplicate record for the same (shop, product) must fail.
	if _, err := db.Exec(
		"INSERT INTO compliance_record (shop_id, product_id, entity_id, status) VALUES (?,?,?,?)",
		shopA, pA, entA, "ok"); err == nil {
		t.Errorf("expected UNIQUE(shop_id, product_id) violation on compliance_record, got nil")
	}
}

// TestMigration003_DownRestores proves down removes shop_id + FKs and that an
// up->down->up cycle is clean (extends the round-trip reversibility contract).
func TestMigration003_DownRestores(t *testing.T) {
	db := openTestDB(t)
	applyAll(t, db)

	const dir = "../../migrations"
	// Down the latest migration (003) and confirm shop_id is gone.
	v, err := migrate.Down(db, dir)
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	if v == 0 {
		t.Fatal("down reverted nothing; expected to revert migration 003")
	}
	if columnExists(t, db, "product", "shop_id") {
		t.Errorf("product.shop_id still present after down")
	}
	if columnExists(t, db, "entity", "shop_id") {
		t.Errorf("entity.shop_id still present after down")
	}
	// Re-up must succeed and restore the column.
	if _, err := migrate.Up(db, dir); err != nil {
		t.Fatalf("re-up after down: %v", err)
	}
	if !columnExists(t, db, "product", "shop_id") {
		t.Errorf("product.shop_id missing after down+up cycle")
	}
}

// TestMigration003_ShopCascade proves the multi-tenant teardown guarantee: once
// the C5-protected referencing rows (classification_rule -> entity RESTRICT) are
// removed in dependency order, a single DELETE on the shop cascades EVERY
// remaining shop-scoped row via the shop_id FKs (ON DELETE CASCADE).
//
// Design note (deliberate, not a workaround): classification_rule.entity_id and
// compliance_record.entity_id keep their referential guards (C5 — a referenced
// entity cannot be silently deleted; that backs the API's 409). Because entity
// and its referencing rows are all direct children of shop, MySQL's RESTRICT/
// NO-ACTION check fires before sibling cascades complete, so a raw DELETE shop
// while a rule still points at an entity is correctly refused. Real teardown
// (the F9 app/uninstalled handler) therefore deletes rules+records first, then
// the shop — exactly what this test exercises. The shop_id cascade itself is
// proven below for product/warning_template and (after the rule is gone) entity.
func TestMigration003_ShopCascade(t *testing.T) {
	db := openTestDB(t)
	applyAll(t, db)

	cleanDomain(t, db)
	shopID := seedShop(t, db, "cascade.myshopify.com")
	entID := seedEntity(t, db, shopID)

	res, err := db.Exec(
		"INSERT INTO product (shop_id, shopify_product_id, title) VALUES (?,?,?)",
		shopID, 9001, "Cascade Product")
	if err != nil {
		t.Fatalf("insert product: %v", err)
	}
	prodID, _ := res.LastInsertId()
	if _, err := db.Exec(
		"INSERT INTO warning_template (shop_id, locale, text) VALUES (?,?,?)",
		shopID, "en", "Warning"); err != nil {
		t.Fatalf("insert warning_template: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO classification_rule (shop_id, priority, match_conditions, entity_id) VALUES (?,?,?,?)",
		shopID, 10, `{"tags":["x"]}`, entID); err != nil {
		t.Fatalf("insert rule: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO compliance_record (shop_id, product_id, entity_id, status) VALUES (?,?,?,?)",
		shopID, prodID, entID, "ok"); err != nil {
		t.Fatalf("insert record: %v", err)
	}

	// A raw DELETE shop while a rule references an entity is correctly RESTRICTed
	// (C5 referential guard) — proving the guard survives the multi-tenant rework.
	if _, err := db.Exec("DELETE FROM shop WHERE id = ?", shopID); err == nil {
		t.Errorf("expected DELETE shop to be RESTRICTed by rule->entity (C5), got nil")
	}

	// Teardown order: remove the C5-protected referencing rows first (as the
	// app/uninstalled handler does), then delete the shop. Everything cascades.
	if _, err := db.Exec("DELETE FROM compliance_record WHERE shop_id = ?", shopID); err != nil {
		t.Fatalf("delete records: %v", err)
	}
	if _, err := db.Exec("DELETE FROM classification_rule WHERE shop_id = ?", shopID); err != nil {
		t.Fatalf("delete rules: %v", err)
	}
	if _, err := db.Exec("DELETE FROM shop WHERE id = ?", shopID); err != nil {
		t.Fatalf("delete shop: %v", err)
	}
	for _, tbl := range []string{"product", "entity", "warning_template", "classification_rule", "compliance_record"} {
		var n int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM "+tbl+" WHERE shop_id = ?", shopID).Scan(&n); err != nil {
			t.Fatalf("count %s after cascade: %v", tbl, err)
		}
		if n != 0 {
			t.Errorf("table %q still has %d rows for deleted shop (cascade failed)", tbl, n)
		}
	}
}

// cleanDomain clears all domain + shop rows in FK-safe order for a repeatable test.
func cleanDomain(t *testing.T, db *sql.DB) {
	t.Helper()
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
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("clean %q: %v", stmt, err)
		}
	}
}

// seedEntity inserts a shop-scoped entity and returns its id.
func seedEntity(t *testing.T, db *sql.DB, shopID int64) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO entity (shop_id, name, address, role, is_eu) VALUES (?,?,?,?,?)",
		shopID, "Entity", "Addr", "importer", true)
	if err != nil {
		t.Fatalf("seed entity for shop %d: %v", shopID, err)
	}
	id, _ := res.LastInsertId()
	return id
}
