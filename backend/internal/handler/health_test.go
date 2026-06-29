package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestHealthz expresses the /healthz contract: GET returns 200 with the exact
// JSON body {"status":"ok"}. healthz must not depend on a live DB connection.
func TestHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	RegisterHealthRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v (raw: %q)", err, rec.Body.String())
	}

	if got, want := body["status"], "ok"; got != want {
		t.Fatalf("expected status field %q, got %q", want, got)
	}

	if len(body) != 1 {
		t.Fatalf("expected exactly one field in response, got %d: %v", len(body), body)
	}
}
