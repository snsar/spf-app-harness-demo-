package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/handler"
)

func cspRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(handler.EmbeddedCSP())
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "shell") })
	return r
}

// TestCSP_HeaderPresent proves a valid shop is reflected into frame-ancestors
// alongside admin.shopify.com.
func TestCSP_HeaderPresent(t *testing.T) {
	r := cspRouter()
	req := httptest.NewRequest(http.MethodGet, "/?shop=acme.myshopify.com", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	got := w.Header().Get("Content-Security-Policy")
	want := "frame-ancestors https://acme.myshopify.com https://admin.shopify.com"
	if got != want {
		t.Errorf("CSP = %q, want %q", got, want)
	}
}

// TestCSP_ValidatesShop proves an absent/invalid shop falls back to ONLY
// admin.shopify.com — never reflecting an unvalidated origin, never *.
func TestCSP_ValidatesShop(t *testing.T) {
	r := cspRouter()
	cases := []string{
		"/",                                   // absent
		"/?shop=evil.com",                     // not myshopify
		"/?shop=acme.myshopify.com.evil.com",  // suffix trick
		"/?shop=*",                            // wildcard attempt
		"/?shop=https://attacker.example",     // scheme injection
	}
	for _, path := range cases {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		got := w.Header().Get("Content-Security-Policy")
		want := "frame-ancestors https://admin.shopify.com"
		if got != want {
			t.Errorf("path %q: CSP = %q, want fallback %q", path, got, want)
		}
	}
}
