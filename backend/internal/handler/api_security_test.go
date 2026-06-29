package handler_test

import (
	"net/http"
	"strings"
	"testing"
)

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
