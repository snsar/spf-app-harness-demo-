package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/repository"
)

// TestAPI_Override_CrossShopProduct_Rejected reproduces F3b-SEC-1: shop A must not
// be able to set a compliance override on a product owned by shop B. The product
// FK references the global surrogate product(id), so without an ownership check the
// handler writes a compliance_record(shop_id=A, product_id=<B's id>). The override
// must be rejected (404, consistent with the rest of the API hiding cross-shop
// existence) and NO compliance_record may be created for that product.
func TestAPI_Override_CrossShopProduct_Rejected(t *testing.T) {
	e := newAPIEnv(t)
	ctx := context.Background()
	prepo := repository.NewProductRepository(e.db)

	// Product belongs to shop B.
	pidB, _ := prepo.Upsert(ctx, e.shopB.ID, 7500, model.Product{Title: "B-product"})
	// Entity belongs to shop A (so the entity check passes; only the product is cross-shop).
	entA := seedAPIEntity(t, e.db, e.shopA.ID)

	// Shop A (default tenant) sets an override on shop B's product id.
	w := e.do(t, http.MethodPost, "/api/compliance/override", map[string]any{
		"product_id": pidB, "entity_id": entA, "warning_template_ids": []int64{}})
	if w.Code != http.StatusNotFound {
		t.Fatalf("override on cross-shop product -> %d, want 404 (%s)", w.Code, w.Body.String())
	}

	// No compliance_record may have been created for that product under shop A.
	var n int
	if err := e.db.QueryRow(
		"SELECT COUNT(*) FROM compliance_record WHERE shop_id=? AND product_id=?",
		e.shopA.ID, pidB).Scan(&n); err != nil {
		t.Fatalf("count records: %v", err)
	}
	if n != 0 {
		t.Fatalf("phantom compliance_record created for shop A on shop B's product id %d (count=%d)", pidB, n)
	}
}

// TestAPI_InjectionGuard_WritePaths sends hostile SQL-looking text through the
// entity/warning/rule create endpoints; parameterized SQL must store it verbatim
// and the schema must survive (§8 G — injection guard on the new write paths).
func TestAPI_InjectionGuard_WritePaths(t *testing.T) {
	e := newAPIEnv(t)
	hostile := "'); DROP TABLE entity;-- <script>alert(1)</script>"

	w := e.do(t, http.MethodPost, "/api/entities", map[string]any{
		"name": hostile, "address": hostile, "role": "importer", "is_eu": false})
	if w.Code != http.StatusCreated {
		t.Fatalf("entity create -> %d", w.Code)
	}
	w = e.do(t, http.MethodPost, "/api/warning-templates", map[string]any{
		"locale": "en", "text": hostile})
	if w.Code != http.StatusCreated {
		t.Fatalf("warning create -> %d", w.Code)
	}

	// The schema must still exist — nothing was interpolated/executed.
	for _, tbl := range []string{"entity", "warning_template", "classification_rule", "product"} {
		var n int
		if err := e.db.QueryRow(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
			tbl).Scan(&n); err != nil {
			t.Fatalf("check table %q: %v", tbl, err)
		}
		if n != 1 {
			t.Fatalf("table %q missing — hostile text was interpolated into SQL!", tbl)
		}
	}

	// The hostile text must round-trip verbatim through the API (stored as data).
	w = e.do(t, http.MethodGet, "/api/warning-templates", nil)
	if !strings.Contains(w.Body.String(), "DROP TABLE entity") {
		t.Errorf("hostile text not stored verbatim: %s", w.Body.String())
	}
}

// TestAPI_NoSecretInResponses proves the shop's access token / shop_id never
// appear in any API response body (the shop is scoped by auth, not exposed).
func TestAPI_NoSecretInResponses(t *testing.T) {
	e := newAPIEnv(t)
	seedAPIEntity(t, e.db, e.shopA.ID)

	for _, path := range []string{"/api/products", "/api/entities", "/api/rules", "/api/warning-templates"} {
		w := e.do(t, http.MethodGet, path, nil)
		body := w.Body.String()
		if strings.Contains(body, "access_token") || strings.Contains(body, "\"shop_id\"") {
			t.Errorf("%s leaked shop secret/scope: %s", path, body)
		}
	}
}
