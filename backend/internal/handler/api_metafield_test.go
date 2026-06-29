package handler_test

// TestApply_MetafieldFailure_DoesNotFailRequest — when the metafield writer
// returns an error, the apply endpoint must still return 200 and include a
// "metafield_sync_warning" field in the JSON body. The DB record is already
// committed; only the Shopify metafield sync failed (best-effort).
//
// TestApply_MetafieldSuccess_NoWarning — on a successful metafield write, the
// "metafield_sync_warning" field must be absent (or null) in the response.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/handler"
	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/repository"
)

// --- fake metafield writer ---------------------------------------------------

// fakeMetafieldWriter is a test double for handler.MetafieldWriter.
type fakeMetafieldWriter struct {
	err        error
	calls      int
	lastStatus model.Status
}

func (f *fakeMetafieldWriter) WriteComplianceMetafields(
	ctx context.Context,
	shopID, shopifyProductID int64,
	status model.Status,
	entity *model.Entity,
	warnings []string,
) error {
	f.calls++
	f.lastStatus = status
	return f.err
}

// --- helpers -----------------------------------------------------------------

// newAPIEnvWithMetafield builds an API test environment that includes a
// MetafieldWriter AND the ComplianceRecs + ShopifyProdIDs ports so the
// syncMetafieldsForProducts path actually runs (rather than being skipped when
// optional deps are nil).
func newAPIEnvWithMetafield(t *testing.T, mw *fakeMetafieldWriter) *apiEnv {
	t.Helper()
	env := newAPIEnv(t) // sets up DB, seeds shopA/shopB

	gin.SetMode(gin.TestMode)
	r2 := gin.New()
	api2 := r2.Group("/api")
	api2.Use(func(c *gin.Context) {
		c.Set("gpsr_shop", env.shopA)
		c.Next()
	})
	deps := apiDeps(env.db) // real DB deps (entities, warnings, rules, classifier, etc.)
	deps.MetafieldWriter = mw
	// Wire the two optional ports so syncMetafieldsForProducts runs.
	deps.ComplianceRecs = repository.NewComplianceRepository(env.db)
	deps.ShopifyProdIDs = repository.NewProductRepository(env.db)
	handler.RegisterAPIRoutes(api2, deps)
	env.r = r2
	return env
}

// seedProductDirect inserts a product row directly (mirrors api_test.go helpers).
func seedProductDirect(t *testing.T, db *sql.DB, shopID, shopifyProductID int64, title string) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO product (shop_id, shopify_product_id, title) VALUES (?,?,?)",
		shopID, shopifyProductID, title,
	)
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// --- tests -------------------------------------------------------------------

// TestApply_MetafieldFailure_DoesNotFailRequest verifies the best-effort contract:
// when WriteComplianceMetafields returns an error, the HTTP response is still 200,
// and "metafield_sync_warning" is present and non-empty in the JSON body.
func TestApply_MetafieldFailure_DoesNotFailRequest(t *testing.T) {
	mw := &fakeMetafieldWriter{err: errors.New("shopify api error: rate limit")}
	env := newAPIEnvWithMetafield(t, mw)

	// Seed a product so apply has something to classify.
	seedProductDirect(t, env.db, env.shopA.ID, 88001, "Test Lamp")

	w := env.do(t, http.MethodPost, "/api/compliance/apply", map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("apply with metafield failure -> %d, want 200 (best-effort); body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	decode(t, w, &resp)

	// metafield_sync_warning must be present and non-empty.
	warn, ok := resp["metafield_sync_warning"]
	if !ok {
		t.Errorf("response missing metafield_sync_warning field; got: %v", resp)
	} else if warn == nil || warn == "" {
		t.Errorf("metafield_sync_warning is empty, want a non-empty warning message; got: %v", warn)
	}

	// The classifier must still have run (applied field present).
	if _, ok := resp["applied"]; !ok {
		t.Errorf("response missing 'applied' field; got: %v", resp)
	}
}

// TestApply_MetafieldSuccess_NoWarning verifies that when WriteComplianceMetafields
// succeeds, the "metafield_sync_warning" field is absent (or null) in the response.
func TestApply_MetafieldSuccess_NoWarning(t *testing.T) {
	mw := &fakeMetafieldWriter{err: nil}
	env := newAPIEnvWithMetafield(t, mw)

	seedProductDirect(t, env.db, env.shopA.ID, 88002, "Test Chair")

	w := env.do(t, http.MethodPost, "/api/compliance/apply", map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("apply with metafield success -> %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	decode(t, w, &resp)

	warn, exists := resp["metafield_sync_warning"]
	if exists && warn != nil && warn != "" {
		t.Errorf("metafield_sync_warning should be absent on success, got: %v", warn)
	}
}
