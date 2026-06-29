package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// shopifyAdminHTTP is the live ShopifyAdminClient: a thin POST to
// https://<shop>/admin/api/<version>/graphql.json with the offline access token.
// Live round-trip is deferred (F3b is unit-tested with a fake); this exists so
// main.go can wire a real client and F9 can exercise it.
type shopifyAdminHTTP struct {
	http    *http.Client
	version string
	// baseOverride lets tests target an httptest server instead of the real
	// per-shop URL. Empty in production (the real URL is derived from the shop).
	baseOverride string
}

// Compile-time check: the live client satisfies the injected interface.
var _ ShopifyAdminClient = (*shopifyAdminHTTP)(nil)

// NewShopifyAdminHTTP constructs the live client. version is the Admin API
// version (e.g. "2024-10").
func NewShopifyAdminHTTP(c *http.Client, version string) *shopifyAdminHTTP {
	return &shopifyAdminHTTP{http: c, version: version}
}

// syncProductsQuery is the GraphQL query for one product page (spec §2.2).
const syncProductsQuery = `query SyncProducts($cursor: String) {
  products(first: 100, after: $cursor) {
    pageInfo { hasNextPage endCursor }
    edges {
      node {
        id
        title
        tags
        productType
        vendor
        metafields(first: 10, namespace: "gpsr") {
          edges { node { key value } }
        }
      }
    }
  }
}`

// graphqlResponse mirrors the products query response shape.
type graphqlResponse struct {
	Data struct {
		Products struct {
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
			Edges []struct {
				Node struct {
					ID          string   `json:"id"`
					Title       string   `json:"title"`
					Tags        []string `json:"tags"`
					ProductType string   `json:"productType"`
					Vendor      string   `json:"vendor"`
					Metafields  struct {
						Edges []struct {
							Node struct {
								Key   string `json:"key"`
								Value string `json:"value"`
							} `json:"node"`
						} `json:"edges"`
					} `json:"metafields"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"products"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// FetchProducts POSTs the GraphQL query and maps one page of products.
func (c *shopifyAdminHTTP) FetchProducts(ctx context.Context, shopDomain, accessToken, cursor string) (ProductPage, error) {
	var cur any
	if cursor != "" {
		cur = cursor
	}
	body, err := json.Marshal(map[string]any{
		"query":     syncProductsQuery,
		"variables": map[string]any{"cursor": cur},
	})
	if err != nil {
		return ProductPage{}, fmt.Errorf("encode graphql request: %w", err)
	}

	url := c.baseOverride
	if url == "" {
		url = fmt.Sprintf("https://%s/admin/api/%s/graphql.json", shopDomain, c.version)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ProductPage{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return ProductPage{}, fmt.Errorf("shopify admin request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProductPage{}, fmt.Errorf("shopify admin status %d", resp.StatusCode)
	}

	var gr graphqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return ProductPage{}, fmt.Errorf("decode graphql response: %w", err)
	}
	if len(gr.Errors) > 0 {
		return ProductPage{}, fmt.Errorf("shopify graphql error: %s", gr.Errors[0].Message)
	}

	page := ProductPage{
		HasNext:   gr.Data.Products.PageInfo.HasNextPage,
		EndCursor: gr.Data.Products.PageInfo.EndCursor,
	}
	for _, e := range gr.Data.Products.Edges {
		n := e.Node
		sp := ShopifyProduct{
			ID:          n.ID,
			Title:       n.Title,
			Tags:        n.Tags,
			ProductType: n.ProductType,
			Vendor:      n.Vendor,
		}
		// Namespace the metafield keys as "gpsr.<key>" so mapping reads
		// gpsr.material / gpsr.origin (the query already filters namespace=gpsr).
		if len(n.Metafields.Edges) > 0 {
			sp.Metafields = make(map[string]string, len(n.Metafields.Edges))
			for _, me := range n.Metafields.Edges {
				sp.Metafields["gpsr."+me.Node.Key] = me.Node.Value
			}
		}
		page.Products = append(page.Products, sp)
	}
	return page, nil
}
