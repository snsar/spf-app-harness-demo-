package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/handler"
)

// TestAPI_NoSession_401 mounts the API routes behind the REAL RequireSessionToken
// middleware with NO Authorization header — every protected route must 401 (the
// §8 D auth path), proving the API group is gated and handlers never run unscoped.
func TestAPI_NoSession_401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(handler.RequireSessionToken(handler.AuthDeps{
		APIKey: "key", APISecret: "secret", Shops: nil,
	}))
	handler.RegisterAPIRoutes(api, handler.APIDeps{})

	for _, route := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/products"},
		{http.MethodGet, "/api/entities"},
		{http.MethodPost, "/api/entities"},
		{http.MethodGet, "/api/rules"},
		{http.MethodPost, "/api/compliance/apply"},
		{http.MethodPost, "/api/sync"},
	} {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s -> %d, want 401 (no session)", route.method, route.path, w.Code)
		}
	}
}
