package handler_test

// webhook_uninstall_test.go — TDD for POST /webhooks/app/uninstalled (F9).
//
// Teardown order (proven by F3b TestMigration003_ShopCascade):
//   1. DELETE compliance_record WHERE shop_id = ?   (RESTRICT via rule->entity)
//   2. DELETE classification_rule WHERE shop_id = ? (removes rule->entity RESTRICT ref)
//   3. DELETE shop WHERE id = ?                     (CASCADE removes entity / warning_template / product)
//
// This file covers three test cases:
//   TestWebhookUninstall_BadHMAC        — 401, no data deleted
//   TestWebhookUninstall_DeletesShopData — full teardown of seeded shop
//   TestWebhookUninstall_OnlyTargetShop  — second shop's data survives (multi-tenant)

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/config"
	"github.com/gpsr/backend/internal/dbtest"
	"github.com/gpsr/backend/internal/handler"
	"github.com/gpsr/backend/internal/migrate"
	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/repository"
)

const uninstallDBLock = "gpsr_uninstall_test_lock"
const uninstallSecret = "shpss_uninstall_test_secret"

// uninstallSign computes the Shopify webhook HMAC for a raw body, base64-encoded.
func uninstallSign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(uninstallSecret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// uninstallDB opens MySQL, acquires the schema lock, migrates, and clears all
// domain tables. It returns the db handle; cleanup is registered on t.
func uninstallDB(t *testing.T) *sql.DB {
	t.Helper()
	cfg := config.Load()
	db, err := sql.Open("mysql", cfg.MySQLDSN())
	if err != nil {
		dbtest.SkipOrFail(t, "open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		dbtest.SkipOrFail(t, "MySQL not reachable: %v", err)
	}
	db.SetMaxOpenConns(1)
	var got sql.NullInt64
	if err := db.QueryRow("SELECT GET_LOCK(?, 30)", uninstallDBLock).Scan(&got); err != nil || !got.Valid || got.Int64 != 1 {
		db.Close()
		t.Fatalf("acquire schema lock %q: %v", uninstallDBLock, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("SELECT RELEASE_LOCK(?)", uninstallDBLock)
		db.Close()
	})

	if _, err := migrate.Up(db, "../../migrations"); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
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
	return db
}

// seedShop inserts an installed shop and returns its surrogate id.
func seedUninstallShop(t *testing.T, db *sql.DB, domain string) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		"INSERT INTO shop (shop_domain, access_token, scope) VALUES (?,?,?)",
		domain, "tok_test", "read_products,write_products")
	if err != nil {
		t.Fatalf("seed shop %q: %v", domain, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedFullShopData inserts entity, warning_template, classification_rule, product,
// and compliance_record rows for the given shop, exercising the FK web the teardown
// must unwind. Returns (entityID, productID).
func seedFullShopData(t *testing.T, db *sql.DB, shopID int64) (entityID, productID int64) {
	t.Helper()
	ctx := context.Background()

	// entity
	res, err := db.ExecContext(ctx,
		"INSERT INTO entity (shop_id, name, address, role, is_eu) VALUES (?,?,?,?,?)",
		shopID, "Test Operator", "1 Main St", "importer", true)
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	entityID, _ = res.LastInsertId()

	// warning_template
	res, err = db.ExecContext(ctx,
		`INSERT INTO warning_template (shop_id, locale, text) VALUES (?,?,?)`,
		shopID, "en", "Keep away from children")
	if err != nil {
		t.Fatalf("seed warning_template: %v", err)
	}
	wtID, _ := res.LastInsertId()

	// classification_rule (references entity — RESTRICT FK)
	res, err = db.ExecContext(ctx,
		`INSERT INTO classification_rule (shop_id, priority, match_conditions, entity_id) VALUES (?,?,?,?)`,
		shopID, 10, `{"tags":["toy"]}`, entityID)
	if err != nil {
		t.Fatalf("seed classification_rule: %v", err)
	}
	ruleID, _ := res.LastInsertId()

	// rule_warning_templates join
	if _, err := db.ExecContext(ctx,
		"INSERT INTO rule_warning_templates (rule_id, warning_template_id) VALUES (?,?)",
		ruleID, wtID); err != nil {
		t.Fatalf("seed rule_warning_templates: %v", err)
	}

	// product (CASCADE from shop)
	prepo := repository.NewProductRepository(db)
	productID, err = prepo.Upsert(ctx, shopID, 77001, model.Product{Title: "Test Toy"})
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}

	// compliance_record (references entity — SET NULL on CASCADE; RESTRICT via rule)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO compliance_record (shop_id, product_id, entity_id, matched_rule_id, status) VALUES (?,?,?,?,?)`,
		shopID, productID, entityID, ruleID, "ok"); err != nil {
		t.Fatalf("seed compliance_record: %v", err)
	}

	return entityID, productID
}

// uninstallRouter builds a Gin router with the uninstall route wired.
func uninstallRouter(db *sql.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler.RegisterWebhookRoutes(r, handler.WebhookDeps{
		APISecret:  uninstallSecret,
		Shops:      repository.NewShopRepository(db),
		Products:   repository.NewProductRepository(db),
		Compliance: repository.NewComplianceRepository(db),
		ShopTeardown: repository.NewShopRepository(db),
	})
	return r
}

// postUninstall sends POST /webhooks/app/uninstalled with the given HMAC header.
func postUninstall(t *testing.T, r *gin.Engine, shopDomain, hmacHeader string) *httptest.ResponseRecorder {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"domain": shopDomain})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/app/uninstalled", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Shop-Domain", shopDomain)
	if hmacHeader != "" {
		req.Header.Set("X-Shopify-Hmac-Sha256", hmacHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestWebhookUninstall_BadHMAC verifies that a tampered HMAC yields 401 and
// leaves all shop data intact (fail-closed before any write).
func TestWebhookUninstall_BadHMAC(t *testing.T) {
	db := uninstallDB(t)
	shopID := seedUninstallShop(t, db, "uninstall-a.myshopify.com")
	seedFullShopData(t, db, shopID)
	r := uninstallRouter(db)

	w := postUninstall(t, r, "uninstall-a.myshopify.com", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad HMAC -> %d, want 401", w.Code)
	}

	// Shop row must still exist — no data deleted on bad HMAC.
	var n int
	db.QueryRow("SELECT COUNT(*) FROM shop WHERE id = ?", shopID).Scan(&n)
	if n != 1 {
		t.Errorf("shop deleted on bad HMAC, want 1 row remaining")
	}
	db.QueryRow("SELECT COUNT(*) FROM entity WHERE shop_id = ?", shopID).Scan(&n)
	if n != 1 {
		t.Errorf("entity deleted on bad HMAC, want 1 row remaining")
	}
}

// TestWebhookUninstall_DeletesShopData verifies that a valid uninstall request
// tears down all of the shop's data in the proven F3b order:
//
//	compliance_record → classification_rule → shop (cascade entity/warning_template/product)
func TestWebhookUninstall_DeletesShopData(t *testing.T) {
	db := uninstallDB(t)
	shopID := seedUninstallShop(t, db, "uninstall-b.myshopify.com")
	seedFullShopData(t, db, shopID)
	r := uninstallRouter(db)

	payload, _ := json.Marshal(map[string]string{"domain": "uninstall-b.myshopify.com"})
	sig := uninstallSign(payload)
	w := postUninstall(t, r, "uninstall-b.myshopify.com", sig)
	if w.Code != http.StatusOK {
		t.Fatalf("valid uninstall -> %d want 200 (body=%s)", w.Code, w.Body.String())
	}

	// All rows for this shop must be gone.
	for _, check := range []struct {
		table string
		query string
	}{
		{"shop", "SELECT COUNT(*) FROM shop WHERE id = ?"},
		{"entity", "SELECT COUNT(*) FROM entity WHERE shop_id = ?"},
		{"warning_template", "SELECT COUNT(*) FROM warning_template WHERE shop_id = ?"},
		{"classification_rule", "SELECT COUNT(*) FROM classification_rule WHERE shop_id = ?"},
		{"product", "SELECT COUNT(*) FROM product WHERE shop_id = ?"},
		{"compliance_record", "SELECT COUNT(*) FROM compliance_record WHERE shop_id = ?"},
	} {
		var n int
		db.QueryRow(check.query, shopID).Scan(&n)
		if n != 0 {
			t.Errorf("after uninstall: %s has %d rows for shop %d, want 0", check.table, n, shopID)
		}
	}
}

// TestWebhookUninstall_OnlyTargetShop verifies that uninstalling shop A leaves
// shop B's data untouched (multi-tenant isolation).
func TestWebhookUninstall_OnlyTargetShop(t *testing.T) {
	db := uninstallDB(t)

	shopAID := seedUninstallShop(t, db, "uninstall-c.myshopify.com")
	shopBID := seedUninstallShop(t, db, "uninstall-d.myshopify.com")
	seedFullShopData(t, db, shopAID)
	seedFullShopData(t, db, shopBID)
	r := uninstallRouter(db)

	// Uninstall shop A only.
	payload, _ := json.Marshal(map[string]string{"domain": "uninstall-c.myshopify.com"})
	sig := uninstallSign(payload)
	w := postUninstall(t, r, "uninstall-c.myshopify.com", sig)
	if w.Code != http.StatusOK {
		t.Fatalf("uninstall shop A -> %d want 200 (body=%s)", w.Code, w.Body.String())
	}

	// Shop A is gone.
	var n int
	db.QueryRow("SELECT COUNT(*) FROM shop WHERE id = ?", shopAID).Scan(&n)
	if n != 0 {
		t.Errorf("shop A still present after uninstall, want 0")
	}

	// Shop B's data survives.
	for _, check := range []struct {
		table string
		query string
	}{
		{"shop", "SELECT COUNT(*) FROM shop WHERE id = ?"},
		{"entity", "SELECT COUNT(*) FROM entity WHERE shop_id = ?"},
		{"warning_template", "SELECT COUNT(*) FROM warning_template WHERE shop_id = ?"},
		{"classification_rule", "SELECT COUNT(*) FROM classification_rule WHERE shop_id = ?"},
		{"product", "SELECT COUNT(*) FROM product WHERE shop_id = ?"},
		{"compliance_record", "SELECT COUNT(*) FROM compliance_record WHERE shop_id = ?"},
	} {
		db.QueryRow(check.query, shopBID).Scan(&n)
		if n == 0 {
			t.Errorf("shop B's %s was deleted when shop A was uninstalled — multi-tenant violation", check.table)
		}
	}
}
