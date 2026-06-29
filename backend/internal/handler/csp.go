package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/service"
)

// EmbeddedCSP returns middleware that sets a Content-Security-Policy
// frame-ancestors directive for the Shopify embedded app iframe (clickjacking
// guard). The merchant shop is read from the `shop` query param, validated with
// service.ValidateShopDomain, and reflected ONLY when valid. An absent or invalid
// shop falls back to admin.shopify.com only — the header NEVER reflects an
// unvalidated origin and NEVER uses `frame-ancestors *`.
//
// nginx must not strip or override this header (see deploy/nginx conf comments).
func EmbeddedCSP() gin.HandlerFunc {
	const adminOnly = "frame-ancestors https://admin.shopify.com"
	return func(c *gin.Context) {
		policy := adminOnly
		shop := c.Query("shop")
		if service.ValidateShopDomain(shop) == nil {
			shop = service.NormalizeShopDomain(shop)
			policy = "frame-ancestors https://" + shop + " https://admin.shopify.com"
		}
		c.Header("Content-Security-Policy", policy)
		c.Next()
	}
}
