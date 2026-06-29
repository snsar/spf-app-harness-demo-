package service

// TestWriteComplianceMetafields_* — unit tests for the metafield writer service.
// TDD Iron Law: these tests are written BEFORE the production code (RED first).
// All tests exercise the interface contract against a fake HTTP client; no real
// Shopify API call is ever made from unit tests (live round-trip deferred to F9).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gpsr/backend/internal/model"
)

// --- fakes -------------------------------------------------------------------

// fakeShopToken is a stub ShopTokenReader that returns canned shop data.
type fakeShopToken struct {
	domain string
	token  string
	err    error
}

func (f *fakeShopToken) GetShopCredentials(ctx context.Context, shopID int64) (domain, token string, err error) {
	return f.domain, f.token, f.err
}

// fakeMetafieldHTTP captures the request body and headers sent to its httptest server.
type fakeMetafieldHTTP struct {
	srv        *httptest.Server
	gotBody    string
	gotToken   string
	statusCode int
	respBody   string
}

func newFakeMetafieldHTTP(t *testing.T, statusCode int, respBody string) *fakeMetafieldHTTP {
	t.Helper()
	f := &fakeMetafieldHTTP{statusCode: statusCode, respBody: respBody}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.gotToken = r.Header.Get("X-Shopify-Access-Token")
		b, _ := io.ReadAll(r.Body)
		f.gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.statusCode)
		_, _ = io.WriteString(w, f.respBody)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// goodMetafieldsSetResp is a minimal Shopify metafieldsSet success response.
const goodMetafieldsSetResp = `{
  "data": {
    "metafieldsSet": {
      "metafields": [{"key":"gpsr_status","value":"ok"}],
      "userErrors": []
    }
  }
}`

// --- TestWriteComplianceMetafields_BuildsCorrectMetafields -------------------

// For status=ok, the writer must send the correct 3 metafields to Shopify:
//   - gpsr_status = "ok"
//   - gpsr_entity_json = {name, address, role} (NO id/shop_id/timestamps)
//   - gpsr_warnings_json = ["warning1", "warning2"]
func TestWriteComplianceMetafields_BuildsCorrectMetafields_OK(t *testing.T) {
	fake := newFakeMetafieldHTTP(t, http.StatusOK, goodMetafieldsSetResp)
	shopToken := &fakeShopToken{domain: "demo.myshopify.com", token: "tok-abc"}

	svc := NewShopifyMetafieldService(fake.srv.Client(), "2026-04", shopToken)
	svc.baseOverride = fake.srv.URL

	entity := &model.Entity{
		ID:      999, // must NOT appear in the metafield payload
		Name:    "Acme EU GmbH",
		Address: "Musterstraße 1, 10115 Berlin, DE",
		Role:    "importer",
		IsEU:    true,
	}
	warnings := []string{"Warning one.", "Warning two."}

	err := svc.WriteComplianceMetafields(context.Background(), 1, 7001, model.StatusOK, entity, warnings)
	if err != nil {
		t.Fatalf("WriteComplianceMetafields: %v", err)
	}

	// Access token must be the one from the shop (DB-sourced), NOT the entity id.
	if fake.gotToken != "tok-abc" {
		t.Errorf("X-Shopify-Access-Token = %q, want tok-abc", fake.gotToken)
	}

	// The request body must be a GraphQL metafieldsSet mutation.
	if !strings.Contains(fake.gotBody, "metafieldsSet") {
		t.Errorf("body missing metafieldsSet: %s", fake.gotBody)
	}
	if !strings.Contains(fake.gotBody, "gpsr_status") {
		t.Errorf("body missing gpsr_status: %s", fake.gotBody)
	}

	// Decode variables to inspect the metafields array.
	var req struct {
		Variables struct {
			Metafields []struct {
				OwnerId   string `json:"ownerId"`
				Namespace string `json:"namespace"`
				Key       string `json:"key"`
				Value     string `json:"value"`
				Type      string `json:"type"`
			} `json:"metafields"`
		} `json:"variables"`
	}
	if err := json.Unmarshal([]byte(fake.gotBody), &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	mfs := req.Variables.Metafields
	if len(mfs) != 3 {
		t.Fatalf("metafields count = %d, want 3; body: %s", len(mfs), fake.gotBody)
	}

	byKey := map[string]struct {
		Value string
		Type  string
	}{}
	for _, m := range mfs {
		byKey[m.Key] = struct {
			Value string
			Type  string
		}{m.Value, m.Type}
	}

	// gpsr_status
	if s, ok := byKey["gpsr_status"]; !ok {
		t.Error("gpsr_status missing")
	} else if s.Value != "ok" {
		t.Errorf("gpsr_status value = %q, want ok", s.Value)
	}

	// gpsr_entity_json — must NOT contain internal fields (id, shop_id, is_eu, timestamps)
	if e, ok := byKey["gpsr_entity_json"]; !ok {
		t.Error("gpsr_entity_json missing")
	} else {
		var ent map[string]any
		if err := json.Unmarshal([]byte(e.Value), &ent); err != nil {
			t.Fatalf("decode gpsr_entity_json: %v", err)
		}
		if ent["name"] != "Acme EU GmbH" {
			t.Errorf("entity.name = %v", ent["name"])
		}
		if ent["address"] != "Musterstraße 1, 10115 Berlin, DE" {
			t.Errorf("entity.address = %v", ent["address"])
		}
		if ent["role"] != "importer" {
			t.Errorf("entity.role = %v", ent["role"])
		}
		// Internal fields must be absent.
		for _, forbidden := range []string{"id", "shop_id", "is_eu", "created_at", "updated_at"} {
			if _, exists := ent[forbidden]; exists {
				t.Errorf("gpsr_entity_json must not contain %q", forbidden)
			}
		}
	}

	// gpsr_warnings_json
	if w, ok := byKey["gpsr_warnings_json"]; !ok {
		t.Error("gpsr_warnings_json missing")
	} else {
		var warnArr []string
		if err := json.Unmarshal([]byte(w.Value), &warnArr); err != nil {
			t.Fatalf("decode gpsr_warnings_json: %v", err)
		}
		if len(warnArr) != 2 || warnArr[0] != "Warning one." || warnArr[1] != "Warning two." {
			t.Errorf("gpsr_warnings_json = %v, want [Warning one. Warning two.]", warnArr)
		}
	}
}

// For status=override, same metafield shape as ok (override entity + warnings).
func TestWriteComplianceMetafields_BuildsCorrectMetafields_Override(t *testing.T) {
	fake := newFakeMetafieldHTTP(t, http.StatusOK, goodMetafieldsSetResp)
	shopToken := &fakeShopToken{domain: "demo.myshopify.com", token: "tok-override"}

	svc := NewShopifyMetafieldService(fake.srv.Client(), "2026-04", shopToken)
	svc.baseOverride = fake.srv.URL

	entity := &model.Entity{ID: 5, Name: "Override Co", Address: "Paris", Role: "manufacturer"}
	warnings := []string{"Override warning."}

	err := svc.WriteComplianceMetafields(context.Background(), 1, 7002, model.StatusOverride, entity, warnings)
	if err != nil {
		t.Fatalf("WriteComplianceMetafields override: %v", err)
	}

	var req struct {
		Variables struct {
			Metafields []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"metafields"`
		} `json:"variables"`
	}
	if err := json.Unmarshal([]byte(fake.gotBody), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byKey := map[string]string{}
	for _, m := range req.Variables.Metafields {
		byKey[m.Key] = m.Value
	}
	if byKey["gpsr_status"] != "override" {
		t.Errorf("gpsr_status = %q, want override", byKey["gpsr_status"])
	}
}

// --- TestWriteComplianceMetafields_NeedsReview_ClearsOrEmpty -----------------

// For status=needs_review, the spec says: write gpsr_status="needs_review" and
// CLEAR (null/empty) the entity and warnings metafields — so no affirmative claim
// lingers on the storefront. The metafields count must still be 3 (all keys sent,
// entity + warnings with null/empty values).
func TestWriteComplianceMetafields_NeedsReview_ClearsOrEmpty(t *testing.T) {
	fake := newFakeMetafieldHTTP(t, http.StatusOK, goodMetafieldsSetResp)
	shopToken := &fakeShopToken{domain: "demo.myshopify.com", token: "tok-nr"}

	svc := NewShopifyMetafieldService(fake.srv.Client(), "2026-04", shopToken)
	svc.baseOverride = fake.srv.URL

	// entity=nil, warnings=nil — the spec contract for needs_review.
	err := svc.WriteComplianceMetafields(context.Background(), 1, 7003, model.StatusNeedsReview, nil, nil)
	if err != nil {
		t.Fatalf("WriteComplianceMetafields needs_review: %v", err)
	}

	var req struct {
		Variables struct {
			Metafields []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"metafields"`
		} `json:"variables"`
	}
	if err := json.Unmarshal([]byte(fake.gotBody), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byKey := map[string]string{}
	for _, m := range req.Variables.Metafields {
		byKey[m.Key] = m.Value
	}

	if byKey["gpsr_status"] != "needs_review" {
		t.Errorf("gpsr_status = %q, want needs_review", byKey["gpsr_status"])
	}
	// entity and warnings must be null/empty — not an affirmative entity object.
	entityVal := byKey["gpsr_entity_json"]
	if entityVal != "" && entityVal != "null" {
		// Verify it's not a populated object
		var ent map[string]any
		if err := json.Unmarshal([]byte(entityVal), &ent); err == nil && len(ent) > 0 {
			t.Errorf("gpsr_entity_json must be null/empty for needs_review, got: %q", entityVal)
		}
	}
	warningsVal := byKey["gpsr_warnings_json"]
	if warningsVal != "" && warningsVal != "null" && warningsVal != "[]" {
		var arr []string
		if err := json.Unmarshal([]byte(warningsVal), &arr); err == nil && len(arr) > 0 {
			t.Errorf("gpsr_warnings_json must be null/empty for needs_review, got: %q", warningsVal)
		}
	}
}

// --- TestWriteComplianceMetafields_ValidatesShopDomain -----------------------

// SSRF guard (mirrors F3b-SEC-2): a hostile shop domain returned by the token
// reader must produce an error WITHOUT making any HTTP request (no egress, no
// token leak). The http.Client is wired with a tripwire that fails the test if
// any request is sent.
func TestWriteComplianceMetafields_ValidatesShopDomain(t *testing.T) {
	hostile := []string{
		"evil.com",
		"169.254.169.254",
		"localhost:6379",
		"good.myshopify.com@evil.com",
		"",
		"../etc/passwd",
	}

	for _, dom := range hostile {
		dom := dom
		t.Run(dom, func(t *testing.T) {
			var egress bool
			tripwire := &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					egress = true
					t.Fatalf("SSRF: client sent a request to %q for hostile shop domain %q", r.URL.String(), dom)
					return nil, nil
				}),
			}
			shopToken := &fakeShopToken{domain: dom, token: "secret-token"}
			svc := NewShopifyMetafieldService(tripwire, "2026-04", shopToken)
			// No baseOverride — this exercises the real production URL path.

			entity := &model.Entity{Name: "X", Address: "Y", Role: "importer"}
			err := svc.WriteComplianceMetafields(context.Background(), 1, 42, model.StatusOK, entity, nil)
			if err == nil {
				t.Errorf("WriteComplianceMetafields(%q) returned nil error, want validation error", dom)
			}
			if egress {
				t.Fatalf("SSRF: request was made for hostile domain %q", dom)
			}
		})
	}
}

// --- TestWriteComplianceMetafields_UsesShopDBToken ---------------------------

// Security contract: the token passed to Shopify must come from the ShopTokenReader
// (DB-sourced), NOT from the entity or any caller-supplied value.
func TestWriteComplianceMetafields_UsesShopDBToken(t *testing.T) {
	fake := newFakeMetafieldHTTP(t, http.StatusOK, goodMetafieldsSetResp)
	// The DB returns a specific token for this shop.
	shopToken := &fakeShopToken{domain: "test.myshopify.com", token: "db-stored-token-xyz"}

	svc := NewShopifyMetafieldService(fake.srv.Client(), "2026-04", shopToken)
	svc.baseOverride = fake.srv.URL

	entity := &model.Entity{Name: "E", Address: "A", Role: "importer"}
	if err := svc.WriteComplianceMetafields(context.Background(), 42, 100, model.StatusOK, entity, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.gotToken != "db-stored-token-xyz" {
		t.Errorf("X-Shopify-Access-Token = %q, want db-stored-token-xyz (must come from DB, not caller)", fake.gotToken)
	}
}

// --- TestWriteComplianceMetafields_ShopifyError_ReturnsError -----------------

// When Shopify returns userErrors in the mutation response, the service returns an error.
func TestWriteComplianceMetafields_ShopifyUserError_ReturnsError(t *testing.T) {
	userErrResp := `{"data":{"metafieldsSet":{"metafields":[],"userErrors":[{"field":["metafields","0","value"],"message":"Value is invalid"}]}}}`
	fake := newFakeMetafieldHTTP(t, http.StatusOK, userErrResp)
	shopToken := &fakeShopToken{domain: "demo.myshopify.com", token: "tok"}

	svc := NewShopifyMetafieldService(fake.srv.Client(), "2026-04", shopToken)
	svc.baseOverride = fake.srv.URL

	entity := &model.Entity{Name: "X", Address: "Y", Role: "importer"}
	err := svc.WriteComplianceMetafields(context.Background(), 1, 50, model.StatusOK, entity, nil)
	if err == nil {
		t.Error("expected error from userErrors, got nil")
	}
}
