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

// WebhookDeps carries the webhook handler's dependencies.
type WebhookDeps struct {
	APISecret  string // SECRET — verifies the raw-body HMAC; never logged
	Shops      ShopStore
	Products   WebhookProductUpserter
	Compliance WebhookComplianceMarker
}

// RegisterWebhookRoutes mounts the two product webhook routes on the root router.
func RegisterWebhookRoutes(r gin.IRouter, deps WebhookDeps) {
	r.POST("/webhooks/products/create", deps.handleProductWebhook)
	r.POST("/webhooks/products/update", deps.handleProductWebhook)
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

	// 7. Success: 200 so Shopify does not retry.
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
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
