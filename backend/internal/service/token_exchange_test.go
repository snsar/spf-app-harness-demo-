package service_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gpsr/backend/internal/service"
)

// fakeDoer is an injected HTTP client so the token exchange is tested WITHOUT
// real network access to Shopify.
type fakeDoer struct {
	resp *http.Response
	err  error
	got  *http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.got = req
	return f.resp, f.err
}

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestExchangeCode_Success(t *testing.T) {
	doer := &fakeDoer{
		resp: jsonResp(http.StatusOK,
			`{"access_token":"shpat_live_token","scope":"read_products,write_products"}`),
	}
	ex := service.NewShopifyTokenExchanger(doer, "the-api-key", "the-api-secret")

	tok, err := ex.ExchangeCode(context.Background(), "acme.myshopify.com", "auth-code-1")
	if err != nil {
		t.Fatalf("exchange failed: %v", err)
	}
	if tok.AccessToken != "shpat_live_token" {
		t.Fatalf("access_token = %q", tok.AccessToken)
	}
	if tok.Scope != "read_products,write_products" {
		t.Fatalf("scope = %q", tok.Scope)
	}

	// It must POST to the shop's token endpoint, never anywhere else.
	if f := doer.got; f == nil {
		t.Fatal("no request was made")
	} else {
		if f.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", f.Method)
		}
		want := "https://acme.myshopify.com/admin/oauth/access_token"
		if f.URL.String() != want {
			t.Errorf("url = %s, want %s", f.URL.String(), want)
		}
	}
}

func TestExchangeCode_RejectsBadShop(t *testing.T) {
	doer := &fakeDoer{resp: jsonResp(http.StatusOK, `{}`)}
	ex := service.NewShopifyTokenExchanger(doer, "k", "s")
	// A non-myshopify shop must be refused BEFORE any HTTP call (SSRF / open host).
	if _, err := ex.ExchangeCode(context.Background(), "evil.com", "code"); err == nil {
		t.Fatal("expected exchange to reject a non-myshopify shop")
	}
	if doer.got != nil {
		t.Fatal("must not make any HTTP request for an invalid shop")
	}
}

func TestExchangeCode_Non200Errors(t *testing.T) {
	doer := &fakeDoer{resp: jsonResp(http.StatusUnauthorized, `{"error":"invalid_request"}`)}
	ex := service.NewShopifyTokenExchanger(doer, "k", "s")
	if _, err := ex.ExchangeCode(context.Background(), "acme.myshopify.com", "code"); err == nil {
		t.Fatal("expected non-200 token response to error")
	}
}

func TestExchangeCode_TransportError(t *testing.T) {
	doer := &fakeDoer{err: errors.New("dial tcp: connection refused")}
	ex := service.NewShopifyTokenExchanger(doer, "k", "s")
	if _, err := ex.ExchangeCode(context.Background(), "acme.myshopify.com", "code"); err == nil {
		t.Fatal("expected transport error to propagate")
	}
}
