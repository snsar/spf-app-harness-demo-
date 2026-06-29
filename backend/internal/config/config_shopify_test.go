package config_test

import (
	"testing"

	"github.com/gpsr/backend/internal/config"
)

func TestLoad_ShopifyFields(t *testing.T) {
	t.Setenv("SHOPIFY_API_KEY", "key123")
	t.Setenv("SHOPIFY_API_SECRET", "secret456")
	t.Setenv("SHOPIFY_APP_URL", "https://gpsr.example.com")
	t.Setenv("SHOPIFY_SCOPES", "read_products,write_products")

	cfg := config.Load()
	if cfg.ShopifyAPIKey != "key123" {
		t.Errorf("ShopifyAPIKey = %q", cfg.ShopifyAPIKey)
	}
	if cfg.ShopifyAPISecret != "secret456" {
		t.Errorf("ShopifyAPISecret = %q", cfg.ShopifyAPISecret)
	}
	if cfg.ShopifyAppURL != "https://gpsr.example.com" {
		t.Errorf("ShopifyAppURL = %q", cfg.ShopifyAppURL)
	}
	if cfg.ShopifyScopes != "read_products,write_products" {
		t.Errorf("ShopifyScopes = %q", cfg.ShopifyScopes)
	}
}

func TestRedirectURI(t *testing.T) {
	cases := []struct {
		appURL string
		want   string
	}{
		{"https://gpsr.example.com", "https://gpsr.example.com/auth/callback"},
		{"https://gpsr.example.com/", "https://gpsr.example.com/auth/callback"}, // trailing slash trimmed
		{"", "/auth/callback"}, // unset base — caller must treat as not-configured
	}
	for _, tc := range cases {
		t.Setenv("SHOPIFY_APP_URL", tc.appURL)
		cfg := config.Load()
		if got := cfg.RedirectURI(); got != tc.want {
			t.Errorf("RedirectURI() with appURL=%q = %q, want %q", tc.appURL, got, tc.want)
		}
	}
}
