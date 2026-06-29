package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/handler"
	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/repository"
	"github.com/gpsr/backend/internal/service"
)

// apiEnv bundles a router + db + the two shops used to prove isolation.
type apiEnv struct {
	db     *sql.DB
	r      *gin.Engine
	shopA  *model.Shop
	shopB  *model.Shop
}

// apiDeps builds the full API deps from real repositories/services for a db.
func apiDeps(db *sql.DB) handler.APIDeps {
	return handler.APIDeps{
		Products:   repository.NewProductRepository(db),
		Entities:   repository.NewEntityRepository(db),
		Warnings:   repository.NewWarningTemplateRepository(db),
		Rules:      repository.NewRuleRepository(db),
		Classifier: service.NewClassifier(repository.NewComplianceRepository(db)),
	}
}

// newAPIEnv seeds two installed shops and mounts the API group behind a TEST
// middleware that injects shopA (so handler logic is exercised without minting a
// JWT per test; the real RequireSessionToken 401 path is covered by F3 + the
// dedicated auth test below). A `?as=B` query selects shop B for isolation tests.
func newAPIEnv(t *testing.T) *apiEnv {
	t.Helper()
	db, shopA := hookDB(t) // reuses the webhook DB harness (lock + clean + shopA)
	res, _ := db.Exec("INSERT INTO shop (shop_domain, access_token, scope) VALUES (?,?,?)",
		"shopb.myshopify.com", "tok", "read_products")
	bID, _ := res.LastInsertId()
	shopB := &model.Shop{ID: bID, ShopDomain: "shopb.myshopify.com"}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(func(c *gin.Context) {
		shop := shopA
		if c.Query("as") == "B" {
			shop = shopB
		}
		c.Set("gpsr_shop", shop)
		c.Next()
	})
	handler.RegisterAPIRoutes(api, apiDeps(db))
	return &apiEnv{db: db, r: r, shopA: shopA, shopB: shopB}
}

func (e *apiEnv) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
}

// --- Entity CRUD --------------------------------------------------------------

func TestAPI_Entity_CRUD(t *testing.T) {
	e := newAPIEnv(t)

	// Create.
	w := e.do(t, http.MethodPost, "/api/entities", map[string]any{
		"name": "Acme EU GmbH", "address": "Berlin", "role": "importer", "is_eu": true})
	if w.Code != http.StatusCreated {
		t.Fatalf("create -> %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var created model.Entity
	decode(t, w, &created)
	if created.ID == 0 || created.Name != "Acme EU GmbH" {
		t.Fatalf("created = %+v", created)
	}

	// Get.
	w = e.do(t, http.MethodGet, "/api/entities/"+itoa(created.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get -> %d", w.Code)
	}

	// List wrapper shape.
	w = e.do(t, http.MethodGet, "/api/entities", nil)
	var listResp struct {
		Entities []model.Entity `json:"entities"`
	}
	decode(t, w, &listResp)
	if len(listResp.Entities) < 1 {
		t.Errorf("list entities empty")
	}

	// Update.
	w = e.do(t, http.MethodPut, "/api/entities/"+itoa(created.ID), map[string]any{
		"name": "Acme EU (v2)", "address": "Berlin", "role": "importer", "is_eu": true})
	if w.Code != http.StatusOK {
		t.Fatalf("update -> %d", w.Code)
	}
	var updated model.Entity
	decode(t, w, &updated)
	if updated.Name != "Acme EU (v2)" {
		t.Errorf("updated name = %q", updated.Name)
	}

	// Delete.
	w = e.do(t, http.MethodDelete, "/api/entities/"+itoa(created.ID), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete -> %d, want 204", w.Code)
	}

	// Get after delete -> 404.
	w = e.do(t, http.MethodGet, "/api/entities/"+itoa(created.ID), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get deleted -> %d, want 404", w.Code)
	}
}

func TestAPI_Entity_Validation(t *testing.T) {
	e := newAPIEnv(t)
	// Malformed body -> 400.
	req := httptest.NewRequest(http.MethodPost, "/api/entities", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body -> %d, want 400", w.Code)
	}
	// Bad id -> 404 (or 400); not a 500.
	w = e.do(t, http.MethodGet, "/api/entities/notanumber", nil)
	if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Fatalf("bad id -> %d, want 404/400", w.Code)
	}
}

func TestAPI_Entity_DeleteReferenced_409(t *testing.T) {
	e := newAPIEnv(t)
	entID := seedAPIEntity(t, e.db, e.shopA.ID)
	wt := seedAPIWarning(t, e.db, e.shopA.ID)
	seedAPIRule(t, e.db, e.shopA.ID, entID, wt)

	w := e.do(t, http.MethodDelete, "/api/entities/"+itoa(entID), nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("delete referenced entity -> %d, want 409", w.Code)
	}
}

// --- Warning template CRUD ----------------------------------------------------

func TestAPI_WarningTemplate_CRUD(t *testing.T) {
	e := newAPIEnv(t)
	w := e.do(t, http.MethodPost, "/api/warning-templates", map[string]any{
		"locale": "en", "text": "Choking hazard. Small parts."})
	if w.Code != http.StatusCreated {
		t.Fatalf("create -> %d (%s)", w.Code, w.Body.String())
	}
	var created model.WarningTemplate
	decode(t, w, &created)
	if created.Text != "Choking hazard. Small parts." {
		t.Errorf("text = %q", created.Text)
	}
	w = e.do(t, http.MethodGet, "/api/warning-templates", nil)
	var lr struct {
		WarningTemplates []model.WarningTemplate `json:"warning_templates"`
	}
	decode(t, w, &lr)
	if len(lr.WarningTemplates) < 1 {
		t.Errorf("list empty")
	}
	w = e.do(t, http.MethodDelete, "/api/warning-templates/"+itoa(created.ID), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete -> %d", w.Code)
	}
}

func TestAPI_WarningTemplate_DeleteReferenced_409(t *testing.T) {
	e := newAPIEnv(t)
	entID := seedAPIEntity(t, e.db, e.shopA.ID)
	wt := seedAPIWarning(t, e.db, e.shopA.ID)
	seedAPIRule(t, e.db, e.shopA.ID, entID, wt)
	w := e.do(t, http.MethodDelete, "/api/warning-templates/"+itoa(wt), nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("delete referenced template -> %d, want 409", w.Code)
	}
}

// --- Rule CRUD + ordering -----------------------------------------------------

func TestAPI_Rule_CRUDAndOrder(t *testing.T) {
	e := newAPIEnv(t)
	entID := seedAPIEntity(t, e.db, e.shopA.ID)
	wt := seedAPIWarning(t, e.db, e.shopA.ID)

	mk := func(priority int) int64 {
		w := e.do(t, http.MethodPost, "/api/rules", map[string]any{
			"priority":             priority,
			"match_conditions":     map[string]any{"tags": []string{"toys"}},
			"entity_id":            entID,
			"warning_template_ids": []int64{wt},
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("create rule p=%d -> %d (%s)", priority, w.Code, w.Body.String())
		}
		var r model.Rule
		decode(t, w, &r)
		return r.ID
	}
	id20 := mk(20)
	id5 := mk(5)

	w := e.do(t, http.MethodGet, "/api/rules", nil)
	var lr struct {
		Rules []model.Rule `json:"rules"`
	}
	decode(t, w, &lr)
	if len(lr.Rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(lr.Rules))
	}
	if lr.Rules[0].ID != id5 || lr.Rules[1].ID != id20 {
		t.Errorf("order = [%d %d], want [%d %d] (priority asc, C1)", lr.Rules[0].ID, lr.Rules[1].ID, id5, id20)
	}
}

func TestAPI_Rule_CrossShopRef_Rejected(t *testing.T) {
	e := newAPIEnv(t)
	// Entity belongs to shop B; shop A (default) creating a rule with it -> 400.
	entB := seedAPIEntity(t, e.db, e.shopB.ID)
	w := e.do(t, http.MethodPost, "/api/rules", map[string]any{
		"priority":         10,
		"match_conditions": map[string]any{"tags": []string{"toys"}},
		"entity_id":        entB,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cross-shop entity ref -> %d, want 400", w.Code)
	}
}

// --- Products list (paginated) ------------------------------------------------

func TestAPI_Products_ListShape(t *testing.T) {
	e := newAPIEnv(t)
	prepo := repository.NewProductRepository(e.db)
	ctx := context.Background()
	for i := int64(0); i < 3; i++ {
		prepo.Upsert(ctx, e.shopA.ID, 7000+i, model.Product{Title: "P", Tags: []string{"toys"}})
	}
	w := e.do(t, http.MethodGet, "/api/products?page=1&limit=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list -> %d", w.Code)
	}
	var resp struct {
		Products []json.RawMessage `json:"products"`
		HasNext  bool              `json:"has_next"`
		Page     int               `json:"page"`
	}
	decode(t, w, &resp)
	if len(resp.Products) != 2 {
		t.Errorf("products = %d, want 2", len(resp.Products))
	}
	if !resp.HasNext {
		t.Errorf("has_next = false, want true")
	}
	if resp.Page != 1 {
		t.Errorf("page echo = %d, want 1", resp.Page)
	}
}

// --- Apply ruleset + override -------------------------------------------------

func TestAPI_ApplyRuleset_And_Override(t *testing.T) {
	e := newAPIEnv(t)
	ctx := context.Background()
	prepo := repository.NewProductRepository(e.db)
	entID := seedAPIEntity(t, e.db, e.shopA.ID)
	wt := seedAPIWarning(t, e.db, e.shopA.ID)
	seedAPIRule(t, e.db, e.shopA.ID, entID, wt) // matches tags=toys

	pid, _ := prepo.Upsert(ctx, e.shopA.ID, 7001, model.Product{Title: "Toy", Tags: []string{"toys"}})
	pid2, _ := prepo.Upsert(ctx, e.shopA.ID, 7002, model.Product{Title: "Mystery"})

	// Apply to all.
	w := e.do(t, http.MethodPost, "/api/compliance/apply", map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("apply -> %d (%s)", w.Code, w.Body.String())
	}
	var ar struct {
		Applied int `json:"applied"`
	}
	decode(t, w, &ar)
	if ar.Applied < 2 {
		t.Errorf("applied = %d, want >= 2", ar.Applied)
	}
	var s1, s2 string
	e.db.QueryRow("SELECT status FROM compliance_record WHERE shop_id=? AND product_id=?", e.shopA.ID, pid).Scan(&s1)
	e.db.QueryRow("SELECT status FROM compliance_record WHERE shop_id=? AND product_id=?", e.shopA.ID, pid2).Scan(&s2)
	if s1 != "ok" {
		t.Errorf("pid status = %q, want ok", s1)
	}
	if s2 != "needs_review" {
		t.Errorf("pid2 status = %q, want needs_review", s2)
	}

	// Set override on pid2.
	w = e.do(t, http.MethodPost, "/api/compliance/override", map[string]any{
		"product_id": pid2, "entity_id": entID, "warning_template_ids": []int64{wt}})
	if w.Code != http.StatusOK {
		t.Fatalf("override -> %d (%s)", w.Code, w.Body.String())
	}
	var os string
	e.db.QueryRow("SELECT status FROM compliance_record WHERE shop_id=? AND product_id=?", e.shopA.ID, pid2).Scan(&os)
	if os != "override" {
		t.Errorf("after override status = %q", os)
	}

	// Re-apply: override must survive (C3).
	e.do(t, http.MethodPost, "/api/compliance/apply", map[string]any{})
	e.db.QueryRow("SELECT status FROM compliance_record WHERE shop_id=? AND product_id=?", e.shopA.ID, pid2).Scan(&os)
	if os != "override" {
		t.Errorf("override did not survive re-apply (C3): %q", os)
	}

	// Clear override -> 204, then re-apply re-infers needs_review.
	w = e.do(t, http.MethodDelete, "/api/compliance/override/"+itoa(pid2), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("clear override -> %d, want 204", w.Code)
	}
	e.do(t, http.MethodPost, "/api/compliance/apply", map[string]any{})
	e.db.QueryRow("SELECT status FROM compliance_record WHERE shop_id=? AND product_id=?", e.shopA.ID, pid2).Scan(&os)
	if os != "needs_review" {
		t.Errorf("after clear+apply status = %q, want needs_review", os)
	}
}

func TestAPI_Override_CrossShopEntity_Rejected(t *testing.T) {
	e := newAPIEnv(t)
	ctx := context.Background()
	prepo := repository.NewProductRepository(e.db)
	pid, _ := prepo.Upsert(ctx, e.shopA.ID, 7010, model.Product{Title: "X"})
	entB := seedAPIEntity(t, e.db, e.shopB.ID) // entity owned by shop B
	w := e.do(t, http.MethodPost, "/api/compliance/override", map[string]any{
		"product_id": pid, "entity_id": entB, "warning_template_ids": []int64{}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("override with cross-shop entity -> %d, want 400", w.Code)
	}
}

// --- Shop isolation (§8 F) ----------------------------------------------------

func TestIsolation_ListProducts(t *testing.T) {
	e := newAPIEnv(t)
	ctx := context.Background()
	prepo := repository.NewProductRepository(e.db)
	prepo.Upsert(ctx, e.shopA.ID, 9001, model.Product{Title: "A-only"})
	prepo.Upsert(ctx, e.shopB.ID, 9002, model.Product{Title: "B-only"})

	// Shop A's list must not contain shop B's product.
	w := e.do(t, http.MethodGet, "/api/products", nil)
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("A-only")) || bytes.Contains([]byte(body), []byte("B-only")) {
		t.Errorf("shop A list leaked B's product: %s", body)
	}
}

func TestIsolation_GetByIdCrossShop(t *testing.T) {
	e := newAPIEnv(t)
	entA := seedAPIEntity(t, e.db, e.shopA.ID)
	// Shop B (?as=B) requesting shop A's entity id -> 404 (no existence leak).
	w := e.do(t, http.MethodGet, "/api/entities/"+itoa(entA)+"?as=B", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-shop entity get -> %d, want 404", w.Code)
	}
}

func TestIsolation_ApplyRuleset(t *testing.T) {
	e := newAPIEnv(t)
	ctx := context.Background()
	prepo := repository.NewProductRepository(e.db)
	// Shop B product + record.
	pidB, _ := prepo.Upsert(ctx, e.shopB.ID, 9100, model.Product{Title: "Bprod", Tags: []string{"toys"}})
	entB := seedAPIEntity(t, e.db, e.shopB.ID)
	e.db.Exec("INSERT INTO compliance_record (shop_id, product_id, entity_id, status) VALUES (?,?,?,?)",
		e.shopB.ID, pidB, entB, "ok")

	// Shop A applies -> must not touch shop B's record.
	e.do(t, http.MethodPost, "/api/compliance/apply", map[string]any{})
	var s string
	e.db.QueryRow("SELECT status FROM compliance_record WHERE shop_id=? AND product_id=?", e.shopB.ID, pidB).Scan(&s)
	if s != "ok" {
		t.Errorf("shop A apply altered shop B record: %q", s)
	}
}

// --- helpers ------------------------------------------------------------------

func itoa(i int64) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func seedAPIEntity(t *testing.T, db *sql.DB, shopID int64) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO entity (shop_id, name, address, role, is_eu) VALUES (?,?,?,?,?)",
		shopID, "Seed Entity", "Addr", "importer", true)
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedAPIWarning(t *testing.T, db *sql.DB, shopID int64) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO warning_template (shop_id, locale, text) VALUES (?,?,?)",
		shopID, "en", "Warning")
	if err != nil {
		t.Fatalf("seed warning: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedAPIRule(t *testing.T, db *sql.DB, shopID, entID, wt int64) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO classification_rule (shop_id, priority, match_conditions, entity_id) VALUES (?,?,?,?)",
		shopID, 10, `{"tags":["toys"]}`, entID)
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	id, _ := res.LastInsertId()
	db.Exec("INSERT INTO rule_warning_templates (rule_id, warning_template_id) VALUES (?,?)", id, wt)
	return id
}
