package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestShopifyAdminHTTP_RequestShape verifies the live client POSTs the GraphQL
// query with the access-token header and parses the products response into a
// ProductPage (gid id, tags, productType, gpsr metafields). The live round-trip
// against real Shopify is deferred; this exercises the request/response contract
// against an httptest server.
func TestShopifyAdminHTTP_RequestShape(t *testing.T) {
	var gotToken, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Shopify-Access-Token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
		  "data": { "products": {
		    "pageInfo": { "hasNextPage": true, "endCursor": "CUR" },
		    "edges": [
		      { "node": {
		          "id": "gid://shopify/Product/777",
		          "title": "Lamp",
		          "tags": ["home","lighting"],
		          "productType": "lighting",
		          "vendor": "ACME",
		          "metafields": { "edges": [
		            { "node": { "key": "material", "value": "metal" } },
		            { "node": { "key": "origin", "value": "DE" } }
		          ] }
		      } }
		    ]
		  } }
		}`)
	}))
	defer srv.Close()

	c := NewShopifyAdminHTTP(srv.Client(), "2024-10")
	// Override the base so the client targets the test server.
	c.baseOverride = srv.URL

	page, err := c.FetchProducts(context.Background(), "demo.myshopify.com", "tok-123", "")
	if err != nil {
		t.Fatalf("FetchProducts: %v", err)
	}
	if gotToken != "tok-123" {
		t.Errorf("X-Shopify-Access-Token = %q, want tok-123", gotToken)
	}
	if !strings.Contains(gotBody, "products(") {
		t.Errorf("request body missing products query: %s", gotBody)
	}
	if len(page.Products) != 1 {
		t.Fatalf("products = %d, want 1", len(page.Products))
	}
	p := page.Products[0]
	if p.ID != "gid://shopify/Product/777" || p.ProductType != "lighting" {
		t.Errorf("product mapping wrong: %+v", p)
	}
	if p.Metafields["gpsr.material"] != "metal" || p.Metafields["gpsr.origin"] != "DE" {
		t.Errorf("metafields = %v, want gpsr.material=metal gpsr.origin=DE", p.Metafields)
	}
	if !page.HasNext || page.EndCursor != "CUR" {
		t.Errorf("pageInfo = hasNext %v cursor %q", page.HasNext, page.EndCursor)
	}

	// Sanity: the body is valid JSON with a query field.
	var req map[string]any
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if _, ok := req["query"]; !ok {
		t.Errorf("request body missing \"query\" field")
	}
	_ = time.Second
}
