package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HTTPDoer is the minimal HTTP client port. The real flow injects
// *http.Client; tests inject a fake so the token exchange runs without any
// network access to Shopify.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// TokenExchanger turns an OAuth `code` into a Shopify access token. It is an
// interface so handlers depend on the behavior, not on net/http, and so the
// callback handler is testable without real network calls.
type TokenExchanger interface {
	ExchangeCode(ctx context.Context, shop, code string) (AccessToken, error)
}

// AccessToken is the result of a successful OAuth code exchange.
type AccessToken struct {
	AccessToken string `json:"access_token"` // SECRET — never log
	Scope       string `json:"scope"`
}

// ShopifyTokenExchanger calls the real Shopify token endpoint over an injected
// HTTPDoer.
type ShopifyTokenExchanger struct {
	doer      HTTPDoer
	apiKey    string
	apiSecret string
}

// NewShopifyTokenExchanger wires the exchanger. apiKey/apiSecret are the app
// credentials (read from env by the caller — never hardcoded here).
func NewShopifyTokenExchanger(doer HTTPDoer, apiKey, apiSecret string) *ShopifyTokenExchanger {
	return &ShopifyTokenExchanger{doer: doer, apiKey: apiKey, apiSecret: apiSecret}
}

// ExchangeCode POSTs the authorization code to https://<shop>/admin/oauth/access_token
// and returns the offline access token + granted scope. The shop is validated as
// a *.myshopify.com host BEFORE any request is made, so a forged `shop` cannot
// turn this into an SSRF to an arbitrary host.
func (e *ShopifyTokenExchanger) ExchangeCode(ctx context.Context, shop, code string) (AccessToken, error) {
	var zero AccessToken
	if err := ValidateShopDomain(shop); err != nil {
		return zero, fmt.Errorf("exchange refused: %w", err)
	}

	body, err := json.Marshal(map[string]string{
		"client_id":     e.apiKey,
		"client_secret": e.apiSecret,
		"code":          code,
	})
	if err != nil {
		return zero, fmt.Errorf("marshal token request: %w", err)
	}

	endpoint := "https://" + NormalizeShopDomain(shop) + "/admin/oauth/access_token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return zero, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := e.doer.Do(req)
	if err != nil {
		return zero, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return zero, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Do not include the response body verbatim — it may echo the code.
		return zero, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var tok AccessToken
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return zero, fmt.Errorf("decode token response: %w", err)
	}
	if tok.AccessToken == "" {
		return zero, fmt.Errorf("token endpoint returned empty access_token")
	}
	return tok, nil
}
