package config_test

import (
	"testing"

	"github.com/gpsr/backend/internal/config"
)

// clearShopifyEnv blanks every env var that participates in the Shopify-app
// precedence chains so a stray value in the test runner's environment (or a
// previous t.Setenv) cannot leak into the case under test. t.Setenv restores
// the prior value automatically at the end of the test.
func clearShopifyEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"SHOPIFY_APP_URL", "APP_URL", "HOST",
		"BACKEND_PORT", "PORT",
		"SHOPIFY_SCOPES", "SCOPES",
	} {
		t.Setenv(k, "")
	}
}

// TestLoad_PrefersCLIAliases proves that when only the Shopify-CLI-provided
// alias is set (our own name unset), Load() resolves the field from the alias.
// This is what makes `shopify app dev` work: the CLI exports APP_URL/HOST/PORT/
// SCOPES (not our SHOPIFY_* names), and the OAuth redirect_uri must follow the
// tunnel URL the CLI registers with Partners.
func TestLoad_PrefersCLIAliases(t *testing.T) {
	t.Run("APP_URL resolves ShopifyAppURL", func(t *testing.T) {
		clearShopifyEnv(t)
		t.Setenv("APP_URL", "https://abc.trycloudflare.com")
		cfg := config.Load()
		if cfg.ShopifyAppURL != "https://abc.trycloudflare.com" {
			t.Errorf("ShopifyAppURL = %q, want %q", cfg.ShopifyAppURL, "https://abc.trycloudflare.com")
		}
	})

	t.Run("HOST resolves ShopifyAppURL", func(t *testing.T) {
		clearShopifyEnv(t)
		t.Setenv("HOST", "https://host.trycloudflare.com")
		cfg := config.Load()
		if cfg.ShopifyAppURL != "https://host.trycloudflare.com" {
			t.Errorf("ShopifyAppURL = %q, want %q", cfg.ShopifyAppURL, "https://host.trycloudflare.com")
		}
	})

	t.Run("PORT resolves BackendPort", func(t *testing.T) {
		clearShopifyEnv(t)
		t.Setenv("PORT", "41234")
		cfg := config.Load()
		if cfg.BackendPort != "41234" {
			t.Errorf("BackendPort = %q, want %q", cfg.BackendPort, "41234")
		}
	})

	t.Run("SCOPES resolves ShopifyScopes", func(t *testing.T) {
		clearShopifyEnv(t)
		t.Setenv("SCOPES", "read_products,write_products,read_themes")
		cfg := config.Load()
		if cfg.ShopifyScopes != "read_products,write_products,read_themes" {
			t.Errorf("ShopifyScopes = %q, want %q", cfg.ShopifyScopes, "read_products,write_products,read_themes")
		}
	})
}

// TestLoad_OwnVarWins proves the precedence detail: when BOTH our own name and
// the CLI alias are set, our own name wins. This keeps local `set -a; . .env`
// dev (gpsr.quotesnap.local) authoritative even if the shell also has APP_URL/
// HOST/PORT exported.
func TestLoad_OwnVarWins(t *testing.T) {
	t.Run("SHOPIFY_APP_URL beats APP_URL and HOST", func(t *testing.T) {
		clearShopifyEnv(t)
		t.Setenv("SHOPIFY_APP_URL", "https://gpsr.quotesnap.local")
		t.Setenv("APP_URL", "https://abc.trycloudflare.com")
		t.Setenv("HOST", "https://host.trycloudflare.com")
		cfg := config.Load()
		if cfg.ShopifyAppURL != "https://gpsr.quotesnap.local" {
			t.Errorf("ShopifyAppURL = %q, want SHOPIFY_APP_URL to win", cfg.ShopifyAppURL)
		}
	})

	t.Run("BACKEND_PORT beats PORT", func(t *testing.T) {
		clearShopifyEnv(t)
		t.Setenv("BACKEND_PORT", "8000")
		t.Setenv("PORT", "41234")
		cfg := config.Load()
		if cfg.BackendPort != "8000" {
			t.Errorf("BackendPort = %q, want BACKEND_PORT to win", cfg.BackendPort)
		}
	})

	t.Run("SHOPIFY_SCOPES beats SCOPES", func(t *testing.T) {
		clearShopifyEnv(t)
		t.Setenv("SHOPIFY_SCOPES", "read_products")
		t.Setenv("SCOPES", "read_products,write_products,read_themes")
		cfg := config.Load()
		if cfg.ShopifyScopes != "read_products" {
			t.Errorf("ShopifyScopes = %q, want SHOPIFY_SCOPES to win", cfg.ShopifyScopes)
		}
	})
}

// TestLoad_Defaults confirms the fallbacks still apply when neither our own
// name nor any CLI alias is set.
func TestLoad_Defaults(t *testing.T) {
	clearShopifyEnv(t)
	cfg := config.Load()
	if cfg.BackendPort != "8000" {
		t.Errorf("BackendPort default = %q, want %q", cfg.BackendPort, "8000")
	}
	if cfg.ShopifyScopes != "read_products,write_products" {
		t.Errorf("ShopifyScopes default = %q, want %q", cfg.ShopifyScopes, "read_products,write_products")
	}
	if cfg.ShopifyAppURL != "" {
		t.Errorf("ShopifyAppURL default = %q, want empty", cfg.ShopifyAppURL)
	}
}
