package config_test

import (
	"testing"

	"github.com/gpsr/backend/internal/config"
)

// TestValidate_FailsClosedOnEmptyAuthSecret is the fail-closed guard for
// F3-SEC-1: the server must refuse to start when any Shopify auth secret is
// missing, because an empty secret makes the whole signature trust chain
// (HMAC, state, JWT) forgeable.
func TestValidate_FailsClosedOnEmptyAuthSecret(t *testing.T) {
	full := config.Config{
		ShopifyAPIKey:    "key123",
		ShopifyAPISecret: "secret456",
		ShopifyAppURL:    "https://gpsr.example.com",
	}

	if err := full.Validate(); err != nil {
		t.Fatalf("Validate() with all auth fields set returned error: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(c *config.Config)
	}{
		{"empty SHOPIFY_API_SECRET", func(c *config.Config) { c.ShopifyAPISecret = "" }},
		{"empty SHOPIFY_API_KEY", func(c *config.Config) { c.ShopifyAPIKey = "" }},
		{"empty SHOPIFY_APP_URL", func(c *config.Config) { c.ShopifyAppURL = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := full
			tc.mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatalf("Validate() with %s returned nil error; want non-nil (fail closed)", tc.name)
			}
		})
	}
}
