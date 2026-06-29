package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/service"
)

// shopContextKey is the Gin context key under which the authenticated shop is
// stored by RequireSessionToken and read by handlers via ShopFromContext.
const shopContextKey = "gpsr_shop"

// RequireSessionToken returns Gin middleware that verifies the App Bridge
// session token (Authorization: Bearer <HS256 JWT>) on protected routes,
// resolves the calling shop, and stores it in the context. Any failure ends the
// request with 401 and never echoes the token.
//
// Checks (delegated to service.VerifySessionToken): HS256 signature against the
// app secret, aud == api key, exp/nbf within leeway, and a resolvable
// *.myshopify.com shop. The resolved shop must additionally be INSTALLED (have a
// row), otherwise the request is rejected — a valid JWT for an uninstalled or
// unknown tenant gets no access.
func RequireSessionToken(deps AuthDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			unauthorized(c)
			return
		}
		token := strings.TrimSpace(auth[len(prefix):])
		if token == "" {
			unauthorized(c)
			return
		}

		claims, err := service.VerifySessionToken(token, deps.APISecret, deps.APIKey)
		if err != nil {
			unauthorized(c)
			return
		}

		shopDomain := claims.ShopDomain()
		shop, err := deps.Shops.GetByDomain(c.Request.Context(), shopDomain)
		if err != nil || shop == nil {
			// DB error or uninstalled tenant — fail closed.
			unauthorized(c)
			return
		}

		c.Set(shopContextKey, shop)
		c.Next()
	}
}

// ShopFromContext returns the authenticated shop placed by RequireSessionToken.
// ok is false when the middleware did not run (e.g. on an unprotected route).
func ShopFromContext(c *gin.Context) (*model.Shop, bool) {
	v, exists := c.Get(shopContextKey)
	if !exists {
		return nil, false
	}
	shop, ok := v.(*model.Shop)
	return shop, ok
}

func unauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}

// RegisterMeRoute wires GET /me onto a (protected) router group. It returns the
// authenticated shop_domain — proving the session-token middleware end-to-end.
// The access token is deliberately never serialized (model.Shop hides it).
func RegisterMeRoute(r gin.IRouter) {
	r.GET("/me", func(c *gin.Context) {
		shop, ok := ShopFromContext(c)
		if !ok {
			unauthorized(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"shop_domain": shop.ShopDomain})
	})
}
