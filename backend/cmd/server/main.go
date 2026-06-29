// Command server is the GPSR Compliance Engine backend entrypoint.
// It wires Gin, loads env-driven config, opens MySQL (port 3308), and mounts the
// Shopify per-shop OAuth install flow (F3) plus a session-token-protected API.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/config"
	"github.com/gpsr/backend/internal/handler"
	"github.com/gpsr/backend/internal/repository"
	"github.com/gpsr/backend/internal/service"
)

func main() {
	cfg := config.Load()
	// Fail closed (F3-SEC-1): refuse to start without the Shopify auth secret,
	// API key, and app URL. An empty secret makes the whole signature trust
	// chain (HMAC, signed state, App Bridge JWT) forgeable.
	if err := cfg.Validate(); err != nil {
		log.Fatalf("refusing to start: %v", err)
	}
	gin.SetMode(cfg.GinMode)

	db, err := sql.Open("mysql", cfg.MySQLDSN())
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("ping db (is MySQL up on %s:%s?): %v", cfg.DBHost, cfg.DBPort, err)
	}

	// Auth dependencies. Secrets come from config (env) — never hardcoded, and
	// never logged below.
	shopRepo := repository.NewShopRepository(db)
	authDeps := handler.AuthDeps{
		APIKey:    cfg.ShopifyAPIKey,
		APISecret: cfg.ShopifyAPISecret,
		AppURL:    cfg.ShopifyAppURL,
		Scopes:    cfg.ShopifyScopes,
		Shops:     shopRepo,
		Exchanger: service.NewShopifyTokenExchanger(
			&http.Client{Timeout: 15 * time.Second},
			cfg.ShopifyAPIKey, cfg.ShopifyAPISecret,
		),
	}

	router := gin.New()
	// Access log (F3-SEC-2): log the request PATH only, never the raw query
	// string. /auth/callback carries the single-use OAuth `code` and `hmac` in
	// the query; gin.Logger() would write those credentials to stdout/logs.
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Formatter: func(p gin.LogFormatterParams) string {
			// gin populates p.Path as "<path>?<rawquery>"; strip the query so
			// the single-use OAuth code/hmac/state are never written to logs.
			path := p.Path
			if i := strings.IndexByte(path, '?'); i >= 0 {
				path = path[:i]
			}
			return fmt.Sprintf("[GIN] %s | %3d | %13v | %15s | %-7s %s\n",
				p.TimeStamp.Format("2006/01/02 - 15:04:05"),
				p.StatusCode,
				p.Latency,
				p.ClientIP,
				p.Method,
				path, // path only — RawQuery (code/hmac/state) deliberately dropped
			)
		},
	}), gin.Recovery())

	// CSP for the embedded iframe: every backend-served response carries a
	// frame-ancestors directive derived from the validated `shop` param (or
	// admin.shopify.com only when absent/invalid). nginx must not strip it.
	router.Use(handler.EmbeddedCSP())

	handler.RegisterHealthRoutes(router)
	handler.RegisterAuthRoutes(router, authDeps)

	// F3b repositories + services (all shop-scoped).
	productRepo := repository.NewProductRepository(db)
	entityRepo := repository.NewEntityRepository(db)
	warningRepo := repository.NewWarningTemplateRepository(db)
	ruleRepo := repository.NewRuleRepository(db)
	complianceRepo := repository.NewComplianceRepository(db)
	classifier := service.NewClassifier(complianceRepo)
	syncService := service.NewProductSyncService(
		service.NewShopifyAdminHTTP(&http.Client{Timeout: 30 * time.Second}, "2024-10"),
		productRepo,
	)

	// Webhooks (root router, NOT session-protected — Shopify is the caller).
	// Protected by raw-body HMAC verification inside the handler.
	handler.RegisterWebhookRoutes(router, handler.WebhookDeps{
		APISecret:  cfg.ShopifyAPISecret,
		Shops:      shopRepo,
		Products:   productRepo,
		Compliance: complianceRepo,
	})

	// Protected API group — every request must carry a valid App Bridge session
	// token; the middleware resolves the calling shop into the context.
	api := router.Group("/api")
	api.Use(handler.RequireSessionToken(authDeps))
	handler.RegisterMeRoute(api)
	handler.RegisterAPIRoutes(api, handler.APIDeps{
		Products:   productRepo,
		Entities:   entityRepo,
		Warnings:   warningRepo,
		Rules:      ruleRepo,
		Classifier: classifier,
		Sync:       syncService,
	})

	addr := ":" + cfg.BackendPort
	// Log non-secret facts only. The redirect_uri is operationally useful and
	// contains no secret; the API secret/token are never logged.
	log.Printf("GPSR backend listening on %s (db %s:%s/%s; oauth redirect_uri=%s)",
		addr, cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.RedirectURI())

	if err := router.Run(addr); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
