// Package config loads runtime configuration from environment variables.
// Defaults mirror .env.example so the binary boots without a .env file.
// Reading config never opens a DB connection.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config holds all environment-driven settings for the backend.
type Config struct {
	// HTTP
	BackendPort string
	GinMode     string

	// MySQL (port 3308 — do not change)
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string

	// Shopify app credentials + OAuth (read from env only; never hardcoded).
	// ShopifyAPISecret is SECRET and must never be logged.
	ShopifyAPIKey    string
	ShopifyAPISecret string
	ShopifyAppURL    string // public HTTPS base URL of the app (e.g. nginx domain)
	ShopifyScopes    string // comma-separated, e.g. read_products,write_products
}

// Load reads configuration from the environment, applying defaults that match
// .env.example. It does not validate connectivity; it only resolves values.
func Load() Config {
	return Config{
		// BackendPort: our own BACKEND_PORT wins; the Shopify CLI exports PORT
		// for a backend-role process, so accept it as an alias.
		BackendPort: getEnvAny([]string{"BACKEND_PORT", "PORT"}, "8000"),
		GinMode:     getEnv("GIN_MODE", "debug"),

		DBHost:     getEnv("DB_HOST", "127.0.0.1"),
		DBPort:     getEnv("DB_PORT", "3308"),
		DBName:     getEnv("DB_NAME", "gpsr"),
		DBUser:     getEnv("DB_USER", "gpsr"),
		DBPassword: getEnv("DB_PASSWORD", ""),

		ShopifyAPIKey:    getEnv("SHOPIFY_API_KEY", ""),
		ShopifyAPISecret: getEnv("SHOPIFY_API_SECRET", ""),
		// ShopifyAppURL: our own SHOPIFY_APP_URL wins so local `.env` dev
		// (gpsr.quotesnap.local) stays authoritative; under `shopify app dev`
		// only APP_URL/HOST (the public tunnel base) are set, so the OAuth
		// redirect_uri follows the tunnel the CLI registers with Partners.
		ShopifyAppURL: getEnvAny([]string{"SHOPIFY_APP_URL", "APP_URL", "HOST"}, ""),
		// ShopifyScopes: SCOPES is the Shopify-CLI alias.
		ShopifyScopes: getEnvAny([]string{"SHOPIFY_SCOPES", "SCOPES"}, "read_products,write_products"),
	}
}

// Validate fails closed on the Shopify auth credentials (F3-SEC-1). An empty
// SHOPIFY_API_SECRET makes the entire signature trust chain (request HMAC,
// signed state, App Bridge JWT) forgeable, so the server must refuse to start
// when any auth field is missing. Only the server entrypoint calls this; the
// migrate command does not need Shopify secrets and must not be gated on them.
// Whitespace-only values are treated as empty (a literal " " secret is still a
// degenerate, forgeable key).
func (c Config) Validate() error {
	if strings.TrimSpace(c.ShopifyAPISecret) == "" {
		return errors.New("SHOPIFY_API_SECRET must be set")
	}
	if strings.TrimSpace(c.ShopifyAPIKey) == "" {
		return errors.New("SHOPIFY_API_KEY must be set")
	}
	if strings.TrimSpace(c.ShopifyAppURL) == "" {
		return errors.New("SHOPIFY_APP_URL must be set")
	}
	return nil
}

// RedirectURI is the OAuth callback URL to register in shopify.app.toml /
// Partners: SHOPIFY_APP_URL + "/auth/callback" (trailing slash on the base is
// trimmed). When ShopifyAppURL is unset it returns just the path, which the
// handler treats as "app URL not configured".
func (c Config) RedirectURI() string {
	base := c.ShopifyAppURL
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return base + "/auth/callback"
}

// MySQLDSN builds a go-sql-driver/mysql DSN from the resolved config.
// parseTime and multiStatements stay off here; the migration runner splits
// statements itself and scans timestamps as needed.
func (c Config) MySQLDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=UTC",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

// getEnv returns the value of key, or fallback when the variable is unset or empty.
func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// getEnvAny returns the first non-empty value among keys, in order, or fallback
// when none is set. It treats an empty value as unset (same semantics as
// getEnv), which is what lets the earlier-listed "own" name win over a later
// CLI alias only when the own name actually carries a value.
func getEnvAny(keys []string, fallback string) string {
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			return v
		}
	}
	return fallback
}
