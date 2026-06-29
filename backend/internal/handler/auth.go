// Package handler — auth.go implements the Shopify OAuth install flow (F3):
// GET /auth (begin) and GET /auth/callback (verify + token exchange + upsert).
// Handlers stay thin: all security primitives (shop validation, HMAC, state,
// token exchange) live in the service layer; the handler only orchestrates and
// maps results to HTTP. No secret is ever logged or returned in a body.
package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/service"
)

// stateCookieName holds the signed OAuth state nonce (CSRF / forged-callback
// guard). HttpOnly so client JS cannot read it.
const stateCookieName = "gpsr_oauth_state"

// ShopStore is the read port the middleware needs: resolve a shop by domain.
type ShopStore interface {
	GetByDomain(ctx context.Context, shopDomain string) (*model.Shop, error)
}

// ShopSaver is the write port the callback needs: persist the granted token.
type ShopSaver interface {
	Upsert(ctx context.Context, shopDomain, accessToken, scope string) error
}

// shopStoreSaver is the union the auth flow uses (a *repository.ShopRepository
// satisfies both halves).
type shopStoreSaver interface {
	ShopStore
	ShopSaver
}

// AuthDeps carries everything the auth handlers + middleware need. App secrets
// are passed in (read from env by main); never hardcoded.
type AuthDeps struct {
	APIKey    string
	APISecret string // SECRET — used to verify HMAC + JWT; never logged
	AppURL    string // public HTTPS base; redirect_uri = AppURL + /auth/callback
	Scopes    string
	Shops     shopStoreSaver
	Exchanger service.TokenExchanger
}

// RegisterAuthRoutes wires GET /auth and GET /auth/callback.
func RegisterAuthRoutes(r gin.IRouter, deps AuthDeps) {
	r.GET("/auth", deps.authBegin)
	r.GET("/auth/callback", deps.authCallback)
}

// redirectURI derives the OAuth callback URL from the configured app URL.
func (d AuthDeps) redirectURI() string {
	base := d.AppURL
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return base + "/auth/callback"
}

// authBegin validates the shop, mints a signed state nonce, sets it as an
// HttpOnly cookie, and 302-redirects to Shopify's authorize endpoint. A bad
// shop returns 400 with NO redirect (open-redirect guard).
func (d AuthDeps) authBegin(c *gin.Context) {
	shop := c.Query("shop")
	if err := service.ValidateShopDomain(shop); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid shop domain"})
		return
	}
	shop = service.NormalizeShopDomain(shop)

	nonce, err := randomNonce()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	signed := service.SignState(d.APISecret, nonce)
	// Secure + HttpOnly + Lax: the callback is a top-level GET redirect from
	// Shopify, so Lax lets the cookie ride along while blocking CSRF POSTs.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(stateCookieName, signed, 600 /*sec*/, "/", "", true /*secure*/, true /*httpOnly*/)

	authorize := url.URL{Scheme: "https", Host: shop, Path: "/admin/oauth/authorize"}
	q := url.Values{}
	q.Set("client_id", d.APIKey)
	q.Set("scope", d.Scopes)
	q.Set("redirect_uri", d.redirectURI())
	q.Set("state", nonce)
	authorize.RawQuery = q.Encode()

	c.Redirect(http.StatusFound, authorize.String())
}

// authCallback verifies the request HMAC, the state cookie, and the shop, then
// exchanges the code for a token and upserts the shop. Each verification is
// fail-closed.
func (d AuthDeps) authCallback(c *gin.Context) {
	q := c.Request.URL.Query()
	shop := q.Get("shop")

	// 1. Shop domain.
	if err := service.ValidateShopDomain(shop); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid shop domain"})
		return
	}
	shop = service.NormalizeShopDomain(shop)

	// 2. HMAC over the full query (critical: proves Shopify origin).
	if err := service.VerifyQueryHMAC(q, d.APISecret); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request signature"})
		return
	}

	// 3. State: signed cookie must decode and equal the query state nonce.
	signedCookie, err := c.Cookie(stateCookieName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing state"})
		return
	}
	cookieNonce, err := service.VerifyState(d.APISecret, signedCookie)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}
	if !service.ConstantTimeEqual(cookieNonce, q.Get("state")) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state mismatch"})
		return
	}

	// 4. Exchange the code for an offline token (behind an interface).
	tok, err := d.Exchanger.ExchangeCode(c.Request.Context(), shop, q.Get("code"))
	if err != nil {
		// Do not echo the underlying error (may reference code/token).
		c.JSON(http.StatusBadGateway, gin.H{"error": "token exchange failed"})
		return
	}

	// 5. Persist (upsert by shop_domain).
	if err := d.Shops.Upsert(c.Request.Context(), shop, tok.AccessToken, tok.Scope); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not persist shop"})
		return
	}

	// Clear the state cookie and redirect into the embedded app.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(stateCookieName, "", -1, "/", "", true, true)
	embedded := url.URL{Scheme: "https", Host: shop, Path: "/admin/apps/" + d.APIKey}
	c.Redirect(http.StatusFound, embedded.String())
}

// randomNonce returns a URL-safe 256-bit random string for OAuth state.
func randomNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
