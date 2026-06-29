package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/config"
	"github.com/gpsr/backend/internal/dbtest"
	"github.com/gpsr/backend/internal/handler"
	"github.com/gpsr/backend/internal/migrate"
	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/repository"
)

const hookSecret = "shpss_webhook_secret"
const hookDBLock = "gpsr_schema_test_lock"

func hookSign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(hookSecret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// hookDB opens the project MySQL, serializes via the shared schema lock, applies
// migrations, and seeds one installed shop. Returns db + the shop.
func hookDB(t *testing.T) (*sql.DB, *model.Shop) {
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
	if err := db.QueryRow("SELECT GET_LOCK(?, 30)", hookDBLock).Scan(&got); err != nil || !got.Valid || got.Int64 != 1 {
		db.Close()
		t.Fatalf("acquire lock: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec("SELECT RELEASE_LOCK(?)", hookDBLock); db.Close() })

	if _, err := migrate.Up(db, "../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, stmt := range []string{
		"DELETE FROM compliance_record_warnings", "DELETE FROM compliance_record",
		"DELETE FROM rule_warning_templates", "DELETE FROM classification_rule",
		"DELETE FROM product", "DELETE FROM warning_template", "DELETE FROM entity",
		"DELETE FROM shop",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("clean %q: %v", stmt, err)
		}
	}
	res, err := db.Exec("INSERT INTO shop (shop_domain, access_token, scope) VALUES (?,?,?)",
		"hook.myshopify.com", "tok", "read_products")
	if err != nil {
		t.Fatalf("seed shop: %v", err)
	}
	id, _ := res.LastInsertId()
	return db, &model.Shop{ID: id, ShopDomain: "hook.myshopify.com"}
}

// hookRouter builds a Gin router with the webhook routes wired to real repos.
func hookRouter(db *sql.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler.RegisterWebhookRoutes(r, handler.WebhookDeps{
		APISecret: hookSecret,
		Shops:     repository.NewShopRepository(db),
		Products:  repository.NewProductRepository(db),
		Compliance: repository.NewComplianceRepository(db),
	})
	return r
}

func postWebhook(t *testing.T, r *gin.Engine, path, shopDomain, hmacHeader string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Shop-Domain", shopDomain)
	if hmacHeader != "" {
		req.Header.Set("X-Shopify-Hmac-Sha256", hmacHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const samplePayload = `{"id":880099,"title":"Webhook Toy","tags":"toys, wood","product_type":"toys"}`

func TestWebhook_RejectsBadHMAC(t *testing.T) {
	db, shop := hookDB(t)
	r := hookRouter(db)
	body := []byte(samplePayload)

	w := postWebhook(t, r, "/webhooks/products/create", shop.ShopDomain, "AAAA", body)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad hmac -> %d, want 401", w.Code)
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM product WHERE shop_id = ?", shop.ID).Scan(&n)
	if n != 0 {
		t.Errorf("bad hmac wrote %d products, want 0 (verify before write)", n)
	}
}

func TestWebhook_AcceptsValidHMAC(t *testing.T) {
	db, shop := hookDB(t)
	r := hookRouter(db)
	body := []byte(samplePayload)

	w := postWebhook(t, r, "/webhooks/products/create", shop.ShopDomain, hookSign(body), body)
	if w.Code != http.StatusOK {
		t.Fatalf("valid hmac -> %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var title string
	if err := db.QueryRow("SELECT title FROM product WHERE shop_id = ? AND shopify_product_id = 880099", shop.ID).Scan(&title); err != nil {
		t.Fatalf("product not upserted: %v", err)
	}
	if title != "Webhook Toy" {
		t.Errorf("title = %q", title)
	}
}

func TestWebhook_EmptySecret_FailClosed(t *testing.T) {
	db, shop := hookDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler.RegisterWebhookRoutes(r, handler.WebhookDeps{
		APISecret:  "", // empty -> must reject all
		Shops:      repository.NewShopRepository(db),
		Products:   repository.NewProductRepository(db),
		Compliance: repository.NewComplianceRepository(db),
	})
	body := []byte(samplePayload)
	w := postWebhook(t, r, "/webhooks/products/create", shop.ShopDomain, hookSign(body), body)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("empty secret -> %d, want 401 (fail closed)", w.Code)
	}
}

func TestWebhook_UnknownShop_Rejected(t *testing.T) {
	db, _ := hookDB(t)
	r := hookRouter(db)
	body := []byte(samplePayload)
	w := postWebhook(t, r, "/webhooks/products/create", "stranger.myshopify.com", hookSign(body), body)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown shop -> %d, want 401", w.Code)
	}
}

func TestWebhook_IdempotentUpsert(t *testing.T) {
	db, shop := hookDB(t)
	r := hookRouter(db)
	body := []byte(samplePayload)
	for i := 0; i < 2; i++ {
		w := postWebhook(t, r, "/webhooks/products/update", shop.ShopDomain, hookSign(body), body)
		if w.Code != http.StatusOK {
			t.Fatalf("upsert %d -> %d", i, w.Code)
		}
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM product WHERE shop_id = ? AND shopify_product_id = 880099", shop.ID).Scan(&n)
	if n != 1 {
		t.Errorf("rows = %d, want 1 (idempotent C7)", n)
	}
}

func TestWebhook_OutOfOrderReplay(t *testing.T) {
	db, shop := hookDB(t)
	r := hookRouter(db)
	newer := []byte(`{"id":880099,"title":"Newer","product_type":"toys"}`)
	older := []byte(`{"id":880099,"title":"Older","product_type":"toys"}`)
	// Apply newer then replay older — upsert is by id, so it stays safe (no dup).
	postWebhook(t, r, "/webhooks/products/update", shop.ShopDomain, hookSign(newer), newer)
	w := postWebhook(t, r, "/webhooks/products/update", shop.ShopDomain, hookSign(older), older)
	if w.Code != http.StatusOK {
		t.Fatalf("replay -> %d, want 200", w.Code)
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM product WHERE shop_id = ? AND shopify_product_id = 880099", shop.ID).Scan(&n)
	if n != 1 {
		t.Errorf("rows = %d, want 1 (replay safe)", n)
	}
}

// seedRecord upserts a product + compliance record with a given status, returns surrogate product id.
func seedHookRecord(t *testing.T, db *sql.DB, shopID, shopifyID int64, status model.Status) int64 {
	t.Helper()
	ctx := context.Background()
	prepo := repository.NewProductRepository(db)
	pid, err := prepo.Upsert(ctx, shopID, shopifyID, model.Product{Title: "Seed"})
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}
	// entity for ok/override records (NULL allowed for needs_review)
	var entID any
	if status != model.StatusNeedsReview {
		res, _ := db.Exec("INSERT INTO entity (shop_id, name, address, role, is_eu) VALUES (?,?,?,?,?)",
			shopID, "E", "A", "importer", true)
		id, _ := res.LastInsertId()
		entID = id
	}
	if _, err := db.Exec("INSERT INTO compliance_record (shop_id, product_id, entity_id, status) VALUES (?,?,?,?)",
		shopID, pid, entID, string(status)); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	return pid
}

func TestWebhook_MarksNeedsReview(t *testing.T) {
	db, shop := hookDB(t)
	r := hookRouter(db)
	pid := seedHookRecord(t, db, shop.ID, 880099, model.StatusOK)

	body := []byte(`{"id":880099,"title":"Changed","product_type":"toys"}`)
	w := postWebhook(t, r, "/webhooks/products/update", shop.ShopDomain, hookSign(body), body)
	if w.Code != http.StatusOK {
		t.Fatalf("update -> %d", w.Code)
	}
	var status string
	db.QueryRow("SELECT status FROM compliance_record WHERE shop_id = ? AND product_id = ?", shop.ID, pid).Scan(&status)
	if status != string(model.StatusNeedsReview) {
		t.Errorf("status = %q, want needs_review (C8)", status)
	}
}

func TestWebhook_OverrideNotTouched(t *testing.T) {
	db, shop := hookDB(t)
	r := hookRouter(db)
	pid := seedHookRecord(t, db, shop.ID, 880099, model.StatusOverride)

	body := []byte(`{"id":880099,"title":"Changed","product_type":"toys"}`)
	postWebhook(t, r, "/webhooks/products/update", shop.ShopDomain, hookSign(body), body)
	var status string
	db.QueryRow("SELECT status FROM compliance_record WHERE shop_id = ? AND product_id = ?", shop.ID, pid).Scan(&status)
	if status != string(model.StatusOverride) {
		t.Errorf("status = %q, want override untouched (C3)", status)
	}
}

func TestWebhook_ScopedToShop(t *testing.T) {
	db, shopA := hookDB(t)
	r := hookRouter(db)
	// Seed a second installed shop B.
	res, _ := db.Exec("INSERT INTO shop (shop_domain, access_token, scope) VALUES (?,?,?)",
		"hookb.myshopify.com", "tok", "read_products")
	shopBID, _ := res.LastInsertId()

	body := []byte(samplePayload)
	w := postWebhook(t, r, "/webhooks/products/create", shopA.ShopDomain, hookSign(body), body)
	if w.Code != http.StatusOK {
		t.Fatalf("create -> %d", w.Code)
	}
	var nA, nB int
	db.QueryRow("SELECT COUNT(*) FROM product WHERE shop_id = ?", shopA.ID).Scan(&nA)
	db.QueryRow("SELECT COUNT(*) FROM product WHERE shop_id = ?", shopBID).Scan(&nB)
	if nA != 1 || nB != 0 {
		t.Errorf("shopA=%d shopB=%d, want 1 and 0 (webhook scoped to shop A)", nA, nB)
	}
}
