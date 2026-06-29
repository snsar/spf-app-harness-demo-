// Package service — shopify_admin.go is the Shopify Admin API (GraphQL) product
// sync. It is contract-based: the ProductSyncService depends on the
// ShopifyAdminClient INTERFACE so it is unit-tested against a fake; the live HTTP
// implementation is a thin POST behind the same interface (live round-trip
// deferred — F3b green does not require a real API call).
package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/gpsr/backend/internal/model"
)

// ShopifyAdminClient fetches one page of products for a shop, given an offline
// access token and an optional GraphQL cursor (cursor == "" => first page).
type ShopifyAdminClient interface {
	FetchProducts(ctx context.Context, shopDomain, accessToken, cursor string) (ProductPage, error)
}

// ProductPage is one page of synced products plus the cursor to continue.
type ProductPage struct {
	Products  []ShopifyProduct
	HasNext   bool
	EndCursor string
}

// ShopifyProduct is the subset of a Shopify product the rules engine matches on.
// Metafields is namespace.key -> value (we read gpsr.material / gpsr.origin).
type ShopifyProduct struct {
	ID          string // gid://shopify/Product/1234567890
	Title       string
	Tags        []string
	ProductType string
	Vendor      string
	Metafields  map[string]string
}

// ProductUpserter is the persistence port the sync service needs: an idempotent,
// shop-scoped product upsert (satisfied by *repository.ProductRepository).
type ProductUpserter interface {
	Upsert(ctx context.Context, shopID, shopifyProductID int64, p model.Product) (int64, error)
}

// ProductSyncService pages through a shop's Shopify products and mirrors each
// into the local product table, scoped to the shop (idempotent — C7).
type ProductSyncService struct {
	client ShopifyAdminClient
	repo   ProductUpserter
}

// NewProductSyncService wires the sync service to its client + persistence port.
func NewProductSyncService(client ShopifyAdminClient, repo ProductUpserter) *ProductSyncService {
	return &ProductSyncService{client: client, repo: repo}
}

// SyncProducts pulls every product page for the shop and upserts each into the
// local mirror scoped to shop.ID. Returns the number of products synced.
func (s *ProductSyncService) SyncProducts(ctx context.Context, shop *model.Shop) (int, error) {
	synced := 0
	cursor := ""
	for {
		page, err := s.client.FetchProducts(ctx, shop.ShopDomain, shop.AccessToken, cursor)
		if err != nil {
			return synced, err
		}
		for _, sp := range page.Products {
			shopifyID, ok := parseShopifyProductID(sp.ID)
			if !ok {
				continue // skip malformed gids rather than invent an id
			}
			p := mapShopifyProduct(sp)
			if _, err := s.repo.Upsert(ctx, shop.ID, shopifyID, p); err != nil {
				return synced, err
			}
			synced++
		}
		if !page.HasNext {
			break
		}
		cursor = page.EndCursor
	}
	return synced, nil
}

// mapShopifyProduct maps a Shopify product to the local engine-match shape.
// category <- productType; material/origin <- gpsr metafields; absent -> nil (C4).
func mapShopifyProduct(sp ShopifyProduct) model.Product {
	p := model.Product{
		Title: sp.Title,
		Tags:  append([]string(nil), sp.Tags...),
	}
	if t := strings.TrimSpace(sp.ProductType); t != "" {
		p.Category = &t
	}
	if v, ok := nonEmptyMetafield(sp.Metafields, "gpsr.material"); ok {
		p.Material = &v
	}
	if v, ok := nonEmptyMetafield(sp.Metafields, "gpsr.origin"); ok {
		p.Origin = &v
	}
	return p
}

// nonEmptyMetafield returns the trimmed value for key if present and non-empty.
func nonEmptyMetafield(m map[string]string, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

// parseShopifyProductID extracts the numeric id from a Shopify product gid
// (gid://shopify/Product/1234567890). A bare numeric string is also accepted
// (REST webhook payloads carry a plain integer id). Returns ok=false on garbage.
func parseShopifyProductID(gid string) (int64, bool) {
	s := strings.TrimSpace(gid)
	if s == "" {
		return 0, false
	}
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
