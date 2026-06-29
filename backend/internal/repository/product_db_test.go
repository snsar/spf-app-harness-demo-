package repository_test

import (
	"context"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/repository"
)

// strptr is a local helper for optional product fields.
func strptr(s string) *string { return &s }

// TestProductRepo_UpsertIdempotent proves Upsert inserts on first call and
// updates (not duplicates) on a second call with the same shopify_product_id (C7).
func TestProductRepo_UpsertIdempotent(t *testing.T) {
	db := openTestDB(t)
	seedFixture(t, db) // establishes testShopID + a shop row
	ctx := context.Background()
	repo := repository.NewProductRepository(db)

	p := model.Product{
		Title:    "Wooden Toy Train",
		Tags:     []string{"toys", "wood"},
		Category: strptr("toys"),
		Material: strptr("wood"),
		Origin:   strptr("CN"),
	}
	const shopifyPID = int64(880001)

	id1, err := repo.Upsert(ctx, testShopID, shopifyPID, p)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Second upsert with changed title must update the SAME row.
	p.Title = "Wooden Toy Train (v2)"
	id2, err := repo.Upsert(ctx, testShopID, shopifyPID, p)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("upsert produced a new row: id1=%d id2=%d (must be idempotent, C7)", id1, id2)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM product WHERE shop_id = ? AND shopify_product_id = ?",
		testShopID, shopifyPID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("rows = %d, want exactly 1 (upsert not insert)", n)
	}

	var title string
	if err := db.QueryRowContext(ctx, "SELECT title FROM product WHERE id = ?", id2).Scan(&title); err != nil {
		t.Fatalf("read title: %v", err)
	}
	if title != "Wooden Toy Train (v2)" {
		t.Errorf("title = %q, want updated value", title)
	}
}

// TestProductRepo_AbsentMetafields_Null proves nil material/origin store as NULL
// so the engine reads them as absent (C4).
func TestProductRepo_AbsentMetafields_Null(t *testing.T) {
	db := openTestDB(t)
	seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewProductRepository(db)

	id, err := repo.Upsert(ctx, testShopID, 880002, model.Product{Title: "Bare", Tags: nil})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var cat, mat, ori, tags interface{}
	if err := db.QueryRowContext(ctx,
		"SELECT category, material, origin, tags FROM product WHERE id = ?", id).
		Scan(&cat, &mat, &ori, &tags); err != nil {
		t.Fatalf("read: %v", err)
	}
	if cat != nil || mat != nil || ori != nil {
		t.Errorf("absent fields must be NULL, got cat=%v mat=%v ori=%v", cat, mat, ori)
	}
}

// TestProductRepo_List_Paginated proves List returns shop-scoped products with
// page/limit offset pagination and a correct has_next (Q4).
func TestProductRepo_List_Paginated(t *testing.T) {
	db := openTestDB(t)
	seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewProductRepository(db)

	// seedFixture left one product (7001). Add 4 more -> 5 total.
	for i := int64(0); i < 4; i++ {
		if _, err := repo.Upsert(ctx, testShopID, 900100+i, model.Product{Title: "P"}); err != nil {
			t.Fatalf("seed product: %v", err)
		}
	}

	page1, hasNext1, err := repo.ListWithCompliance(ctx, testShopID, 1, 2)
	if err != nil {
		t.Fatalf("list page1: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1))
	}
	if !hasNext1 {
		t.Errorf("hasNext1 = false, want true (5 rows, limit 2)")
	}
	page3, hasNext3, err := repo.ListWithCompliance(ctx, testShopID, 3, 2)
	if err != nil {
		t.Fatalf("list page3: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("page3 len = %d, want 1 (the 5th row)", len(page3))
	}
	if hasNext3 {
		t.Errorf("hasNext3 = true, want false (last page)")
	}
}

// TestProductRepo_GetWithCompliance_NilWhenNoRecord proves a product with no
// compliance record returns compliance == nil (never a synthesized record, C2).
func TestProductRepo_GetWithCompliance_NilWhenNoRecord(t *testing.T) {
	db := openTestDB(t)
	_, _, _, productID, _ := seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewProductRepository(db)

	got, err := repo.GetWithCompliance(ctx, testShopID, productID)
	if err != nil {
		t.Fatalf("GetWithCompliance: %v", err)
	}
	if got == nil {
		t.Fatal("product not found")
	}
	if got.Compliance != nil {
		t.Errorf("compliance = %+v, want nil (no record yet, C2)", got.Compliance)
	}
	if got.Product.ID != productID {
		t.Errorf("id = %d, want %d", got.Product.ID, productID)
	}
}

// TestProductRepo_GetWithCompliance_CrossShop returns nil for another shop's id.
func TestProductRepo_GetWithCompliance_CrossShop(t *testing.T) {
	db := openTestDB(t)
	_, _, _, productID, _ := seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewProductRepository(db)

	got, err := repo.GetWithCompliance(ctx, testShopID+99999, productID)
	if err != nil {
		t.Fatalf("GetWithCompliance: %v", err)
	}
	if got != nil {
		t.Errorf("cross-shop access returned a product: %+v (want nil)", got)
	}
}
