package service

import (
	"context"
	"testing"

	"github.com/gpsr/backend/internal/model"
)

// fakeAdminClient returns canned product pages for unit-testing the sync service
// without the network (live round-trip deferred per the spec).
type fakeAdminClient struct {
	pages    []ProductPage
	calls    []string // cursors requested, in order
	lastTok  string
	lastShop string
}

func (f *fakeAdminClient) FetchProducts(_ context.Context, shopDomain, accessToken, cursor string) (ProductPage, error) {
	f.calls = append(f.calls, cursor)
	f.lastTok = accessToken
	f.lastShop = shopDomain
	idx := len(f.calls) - 1
	if idx >= len(f.pages) {
		return ProductPage{}, nil
	}
	return f.pages[idx], nil
}

// fakeUpserter is an in-memory ProductUpserter recording every upsert + scope.
type fakeUpserter struct {
	upserts    []upsertCall
	byShopify  map[int64]int64 // shopify_product_id -> surrogate id (idempotency)
	nextID     int64
	lastShopID int64
}

type upsertCall struct {
	shopID    int64
	shopifyID int64
	product   model.Product
}

func newFakeUpserter() *fakeUpserter {
	return &fakeUpserter{byShopify: map[int64]int64{}}
}

func (f *fakeUpserter) Upsert(_ context.Context, shopID, shopifyProductID int64, p model.Product) (int64, error) {
	f.lastShopID = shopID
	f.upserts = append(f.upserts, upsertCall{shopID, shopifyProductID, p})
	if id, ok := f.byShopify[shopifyProductID]; ok {
		return id, nil // idempotent: same row
	}
	f.nextID++
	f.byShopify[shopifyProductID] = f.nextID
	return f.nextID, nil
}

func testShop() *model.Shop {
	return &model.Shop{ID: 42, ShopDomain: "demo.myshopify.com", AccessToken: "tok-secret"}
}

// TestSync_MapsShopifyFields proves gid->numeric id, title/tags/productType->category,
// and gpsr.material / gpsr.origin metafields map to material/origin.
func TestSync_MapsShopifyFields(t *testing.T) {
	client := &fakeAdminClient{pages: []ProductPage{{
		Products: []ShopifyProduct{{
			ID:          "gid://shopify/Product/1234567890",
			Title:       "Wooden Toy Train",
			Tags:        []string{"toys", "wood"},
			ProductType: "toys",
			Metafields:  map[string]string{"gpsr.material": "wood", "gpsr.origin": "CN"},
		}},
		HasNext: false,
	}}}
	up := newFakeUpserter()
	svc := NewProductSyncService(client, up)

	n, err := svc.SyncProducts(context.Background(), testShop())
	if err != nil {
		t.Fatalf("SyncProducts: %v", err)
	}
	if n != 1 {
		t.Fatalf("synced = %d, want 1", n)
	}
	got := up.upserts[0]
	if got.shopifyID != 1234567890 {
		t.Errorf("shopify id = %d, want 1234567890 (parsed from gid)", got.shopifyID)
	}
	if got.product.Title != "Wooden Toy Train" {
		t.Errorf("title = %q", got.product.Title)
	}
	if got.product.Category == nil || *got.product.Category != "toys" {
		t.Errorf("category = %v, want toys (from productType)", got.product.Category)
	}
	if got.product.Material == nil || *got.product.Material != "wood" {
		t.Errorf("material = %v, want wood (gpsr.material)", got.product.Material)
	}
	if got.product.Origin == nil || *got.product.Origin != "CN" {
		t.Errorf("origin = %v, want CN (gpsr.origin)", got.product.Origin)
	}
	if len(got.product.Tags) != 2 {
		t.Errorf("tags = %v, want 2", got.product.Tags)
	}
}

// TestSync_AbsentMetafields_NullFields proves missing gpsr metafields -> nil
// material/origin so the engine reads them as absent (C4).
func TestSync_AbsentMetafields_NullFields(t *testing.T) {
	client := &fakeAdminClient{pages: []ProductPage{{
		Products: []ShopifyProduct{{
			ID:          "gid://shopify/Product/55",
			Title:       "Bare",
			ProductType: "",
			Metafields:  nil,
		}},
	}}}
	up := newFakeUpserter()
	svc := NewProductSyncService(client, up)
	if _, err := svc.SyncProducts(context.Background(), testShop()); err != nil {
		t.Fatalf("SyncProducts: %v", err)
	}
	got := up.upserts[0].product
	if got.Material != nil || got.Origin != nil {
		t.Errorf("absent metafields must be nil, got material=%v origin=%v", got.Material, got.Origin)
	}
	if got.Category != nil {
		t.Errorf("empty productType must map to nil category, got %v", got.Category)
	}
}

// TestSync_Paginates proves the service follows the cursor across pages.
func TestSync_Paginates(t *testing.T) {
	client := &fakeAdminClient{pages: []ProductPage{
		{Products: []ShopifyProduct{{ID: "gid://shopify/Product/1", Title: "A"}}, HasNext: true, EndCursor: "CUR1"},
		{Products: []ShopifyProduct{{ID: "gid://shopify/Product/2", Title: "B"}}, HasNext: false, EndCursor: ""},
	}}
	up := newFakeUpserter()
	svc := NewProductSyncService(client, up)

	n, err := svc.SyncProducts(context.Background(), testShop())
	if err != nil {
		t.Fatalf("SyncProducts: %v", err)
	}
	if n != 2 {
		t.Errorf("synced = %d, want 2 (both pages)", n)
	}
	if len(client.calls) != 2 || client.calls[0] != "" || client.calls[1] != "CUR1" {
		t.Errorf("cursor flow = %v, want [\"\" \"CUR1\"]", client.calls)
	}
}

// TestSync_Idempotent proves syncing the same product twice yields one row (C7).
func TestSync_Idempotent(t *testing.T) {
	page := ProductPage{Products: []ShopifyProduct{{ID: "gid://shopify/Product/9", Title: "X"}}}
	client := &fakeAdminClient{pages: []ProductPage{page, page}}
	up := newFakeUpserter()
	svc := NewProductSyncService(client, up)

	_, _ = svc.SyncProducts(context.Background(), testShop())
	_, _ = svc.SyncProducts(context.Background(), testShop())
	if len(up.byShopify) != 1 {
		t.Errorf("distinct product rows = %d, want 1 (idempotent C7)", len(up.byShopify))
	}
}

// TestSync_ScopedToShop proves every upsert carries the caller shop's id, and the
// shop's offline token is what the client is called with.
func TestSync_ScopedToShop(t *testing.T) {
	client := &fakeAdminClient{pages: []ProductPage{{
		Products: []ShopifyProduct{{ID: "gid://shopify/Product/7", Title: "Y"}},
	}}}
	up := newFakeUpserter()
	svc := NewProductSyncService(client, up)

	sh := testShop()
	if _, err := svc.SyncProducts(context.Background(), sh); err != nil {
		t.Fatalf("SyncProducts: %v", err)
	}
	if up.lastShopID != sh.ID {
		t.Errorf("upsert shop id = %d, want %d", up.lastShopID, sh.ID)
	}
	if client.lastTok != sh.AccessToken {
		t.Errorf("client called with token %q, want the shop's offline token", client.lastTok)
	}
	if client.lastShop != sh.ShopDomain {
		t.Errorf("client called with shop %q, want %q", client.lastShop, sh.ShopDomain)
	}
}
