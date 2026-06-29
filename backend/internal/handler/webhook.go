// Package handler — webhook.go handles Shopify product create/update webhooks.
// These routes live OUTSIDE /api (Shopify is the caller, not the browser): they
// are protected by raw-body HMAC verification, not the session-token middleware.
//
// Security: the raw request body is read and HMAC-verified BEFORE any JSON
// decode (Gin's ShouldBindJSON would consume the body and defeat byte-exact
// HMAC). Verification + shop resolution are fail-closed. On success we return 200
// so Shopify does not retry; only a genuine persistence failure returns non-200.
package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/service"
)

// WebhookProductUpserter is the persistence port for mirroring a product
// (idempotent, shop-scoped — C7). Satisfied by *repository.ProductRepository.
type WebhookProductUpserter interface {
	Upsert(ctx context.Context, shopID, shopifyProductID int64, p model.Product) (int64, error)
}

// WebhookComplianceMarker marks a product's record needs_review unless override.
// Satisfied by *repository.ComplianceRepository.
type WebhookComplianceMarker interface {
	MarkNeedsReviewIfNotOverride(ctx context.Context, shopID, productID int64) error
}

// ShopTeardown is the port for removing all of a shop's data during app/uninstalled.
// It runs the proven F3b delete order in a single transaction:
//
//	compliance_record → classification_rule → shop (CASCADE for the rest).
//
// Satisfied by *repository.ShopRepository.TeardownShop.
type ShopTeardown interface {
	TeardownShop(ctx context.Context, shopID int64) error
}

// WebhookDeps carries the webhook handler's dependencies.
type WebhookDeps struct {
	APISecret       string // SECRET — verifies the raw-body HMAC; never logged
	Shops           ShopStore
	Products        WebhookProductUpserter
	Compliance      WebhookComplianceMarker
	MetafieldWriter MetafieldWriter        // optional; nil disables metafield sync
	ShopifyProdIDs  shopifyProductIDGetter // optional; needed for metafield sync
	ShopTeardown    ShopTeardown           // required for app/uninstalled
}

// RegisterWebhookRoutes mounts the product and app/uninstalled webhook routes on
// the root router. All routes are protected by raw-body HMAC verification.
func RegisterWebhookRoutes(r gin.IRouter, deps WebhookDeps) {
	r.POST("/webhooks/products/create", deps.handleProductWebhook)
	r.POST("/webhooks/products/update", deps.handleProductWebhook)
	r.POST("/webhooks/app/uninstalled", deps.handleAppUninstalled)
}

// shopifyProductPayload is the subset of the Shopify REST webhook product JSON we
// mirror. Webhook payloads carry `tags` as a CSV string and `product_type`;
// metafields are NOT in the default payload, so material/origin are left to the
// GraphQL /api/sync path (Q3) and the record is marked needs_review.
type shopifyProductPayload struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Tags        string `json:"tags"`
	ProductType string `json:"product_type"`
}

func (d WebhookDeps) handleProductWebhook(c *gin.Context) {
	// 1. Read the RAW body BEFORE any JSON decode (byte-exact HMAC).
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	// 2. Verify the HMAC (fail-closed; empty secret rejected by the primitive).
	headerHMAC := c.GetHeader("X-Shopify-Hmac-Sha256")
	if err := service.VerifyWebhookHMAC(rawBody, headerHMAC, d.APISecret); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
		return
	}

	// 3. Resolve the shop from the validated domain header; not installed -> 401.
	shopDomain := c.GetHeader("X-Shopify-Shop-Domain")
	if service.ValidateShopDomain(shopDomain) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid shop domain"})
		return
	}
	shopDomain = service.NormalizeShopDomain(shopDomain)
	shop, err := d.Shops.GetByDomain(c.Request.Context(), shopDomain)
	if err != nil || shop == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "shop not installed"})
		return
	}

	// 4. Parse the product payload and map to the local shape.
	var payload shopifyProductPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil || payload.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product payload"})
		return
	}
	product := model.Product{
		Title: payload.Title,
		Tags:  splitTags(payload.Tags),
	}
	if t := strings.TrimSpace(payload.ProductType); t != "" {
		product.Category = &t
	}

	// 5. Idempotent upsert scoped to the shop (C7).
	productID, err := d.Products.Upsert(c.Request.Context(), shop.ID, payload.ID, product)
	if err != nil {
		// Genuine failure: return non-200 so Shopify retries.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not mirror product"})
		return
	}

	// 6. Mark the record needs_review unless it is an override (C8/C3). Idempotent.
	if err := d.Compliance.MarkNeedsReviewIfNotOverride(c.Request.Context(), shop.ID, productID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not mark review"})
		return
	}

	// 7. Metafield sync — best-effort after DB commit. For needs_review, we
	// write gpsr_status=needs_review and clear entity/warnings. Failure is
	// non-fatal for webhooks (Shopify already got the DB-committed product update).
	if d.MetafieldWriter != nil && d.ShopifyProdIDs != nil {
		shopifyProdID, idErr := d.ShopifyProdIDs.GetShopifyProductID(c.Request.Context(), shop.ID, productID)
		if idErr == nil && shopifyProdID != 0 {
			// Ignore error — best-effort; log would go here in production.
			_ = d.MetafieldWriter.WriteComplianceMetafields(
				c.Request.Context(), shop.ID, shopifyProdID,
				model.StatusNeedsReview, nil, nil,
			)
		}
	}

	// 8. Success: 200 so Shopify does not retry.
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// handleAppUninstalled handles POST /webhooks/app/uninstalled.
//
// When a merchant uninstalls the app, Shopify POSTs a raw-body HMAC-signed
// request to this route. The handler:
//  1. Reads the raw body and verifies the HMAC (same primitive as product webhooks,
//     fail-closed on empty secret — F3-SEC-1 defence in depth).
//  2. Resolves the shop from X-Shopify-Shop-Domain (validated, then DB-looked-up).
//  3. Tears down the shop's data in the proven F3b order via ShopTeardown:
//     compliance_record → classification_rule → shop (CASCADE for the rest).
//
// Returns 200 on success so Shopify does not retry. Returns 401 on HMAC / shop
// failures (fail-closed). Returns 500 only on a genuine persistence failure.
func (d WebhookDeps) handleAppUninstalled(c *gin.Context) {
	// 1. Read raw body BEFORE any decode (byte-exact HMAC).
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	// 2. Verify HMAC (fail-closed; empty secret rejected by the primitive).
	headerHMAC := c.GetHeader("X-Shopify-Hmac-Sha256")
	if err := service.VerifyWebhookHMAC(rawBody, headerHMAC, d.APISecret); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
		return
	}

	// 3. Resolve shop from the validated domain header.
	shopDomain := c.GetHeader("X-Shopify-Shop-Domain")
	if service.ValidateShopDomain(shopDomain) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid shop domain"})
		return
	}
	shopDomain = service.NormalizeShopDomain(shopDomain)
	shop, err := d.Shops.GetByDomain(c.Request.Context(), shopDomain)
	if err != nil || shop == nil {
		// Unknown shop — already gone or never installed. Treat as success so
		// Shopify does not keep retrying an already-clean state.
		c.JSON(http.StatusOK, gin.H{"status": "already_uninstalled"})
		return
	}

	// 4. Tear down in the proven order (F3b: compliance_record → rule → shop → CASCADE).
	if d.ShopTeardown == nil {
		// Misconfigured server — fail so the operator knows.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "teardown not configured"})
		return
	}
	if err := d.ShopTeardown.TeardownShop(c.Request.Context(), shop.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not remove shop data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "uninstalled"})
}

// splitTags splits Shopify's CSV `tags` string into trimmed, non-empty tags.
func splitTags(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
