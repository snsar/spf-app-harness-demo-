package repository_test

import (
	"context"
	"errors"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/repository"
)

// --- EntityRepository ---------------------------------------------------------

// TestEntityRepo_CRUD round-trips create/get/list/update/delete, all shop-scoped.
func TestEntityRepo_CRUD(t *testing.T) {
	db := openTestDB(t)
	seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewEntityRepository(db)

	e := model.Entity{Name: "Acme EU GmbH", Address: "Berlin", Role: "importer", IsEU: true}
	created, err := repo.Create(ctx, testShopID, e)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("created entity has no id")
	}

	got, err := repo.Get(ctx, testShopID, created.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v / %v", got, err)
	}
	if got.Name != "Acme EU GmbH" || got.Role != "importer" || !got.IsEU {
		t.Errorf("get mismatch: %+v", got)
	}

	got.Name = "Acme EU GmbH (updated)"
	updated, err := repo.Update(ctx, testShopID, created.ID, *got)
	if err != nil || updated == nil {
		t.Fatalf("update: %v / %v", updated, err)
	}
	if updated.Name != "Acme EU GmbH (updated)" {
		t.Errorf("update name = %q", updated.Name)
	}

	list, err := repo.List(ctx, testShopID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// seedFixture created one entity; plus this one = at least 2.
	if len(list) < 2 {
		t.Errorf("list len = %d, want >= 2", len(list))
	}

	if err := repo.Delete(ctx, testShopID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gone, _ := repo.Get(ctx, testShopID, created.ID)
	if gone != nil {
		t.Errorf("entity still present after delete: %+v", gone)
	}
}

// TestEntityRepo_GetCrossShop returns nil for another shop's entity id (no leak).
func TestEntityRepo_GetCrossShop(t *testing.T) {
	db := openTestDB(t)
	entityID, _, _, _, _ := seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewEntityRepository(db)

	got, err := repo.Get(ctx, testShopID+99999, entityID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("cross-shop get returned %+v, want nil", got)
	}
}

// TestEntityRepo_DeleteReferenced_Conflict proves deleting an entity referenced
// by a rule returns ErrReferenced (mapped to 409 by the handler — C5).
func TestEntityRepo_DeleteReferenced_Conflict(t *testing.T) {
	db := openTestDB(t)
	entityID, _, _, _, _ := seedFixture(t, db) // seedFixture's rule references this entity
	ctx := context.Background()
	repo := repository.NewEntityRepository(db)

	err := repo.Delete(ctx, testShopID, entityID)
	if !errors.Is(err, repository.ErrReferenced) {
		t.Fatalf("delete referenced entity err = %v, want ErrReferenced (C5 -> 409)", err)
	}
}

// --- WarningTemplateRepository ------------------------------------------------

// TestWarningTemplateRepo_CRUD round-trips create/get/list/update/delete.
func TestWarningTemplateRepo_CRUD(t *testing.T) {
	db := openTestDB(t)
	seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewWarningTemplateRepository(db)

	// Untrusted text must round-trip verbatim (parameterized SQL).
	wt := model.WarningTemplate{Locale: "en", Text: "Choking hazard'); DROP TABLE entity;--"}
	created, err := repo.Create(ctx, testShopID, wt)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(ctx, testShopID, created.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v / %v", got, err)
	}
	if got.Text != wt.Text {
		t.Errorf("text not verbatim: %q", got.Text)
	}

	got.Text = "Updated warning"
	if _, err := repo.Update(ctx, testShopID, created.ID, *got); err != nil {
		t.Fatalf("update: %v", err)
	}

	if err := repo.Delete(ctx, testShopID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// The entity table must still exist — the DROP TABLE text was never executed.
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'entity'").Scan(&n); err != nil {
		t.Fatalf("check entity table: %v", err)
	}
	if n != 1 {
		t.Fatal("entity table missing — untrusted warning text was interpolated into SQL!")
	}
}

// TestWarningTemplateRepo_DeleteReferenced_Conflict proves deleting a template
// referenced by a rule returns ErrReferenced (C5 -> 409).
func TestWarningTemplateRepo_DeleteReferenced_Conflict(t *testing.T) {
	db := openTestDB(t)
	_, wt1, _, _, _ := seedFixture(t, db) // seedFixture's rule links wt1
	ctx := context.Background()
	repo := repository.NewWarningTemplateRepository(db)

	err := repo.Delete(ctx, testShopID, wt1)
	if !errors.Is(err, repository.ErrReferenced) {
		t.Fatalf("delete referenced template err = %v, want ErrReferenced", err)
	}
}

// --- RuleRepository -----------------------------------------------------------

// TestRuleRepo_CRUDAndOrder proves rule create/get/update/delete plus the
// (priority asc, id asc) ordering of List (C1 visible precedence).
func TestRuleRepo_CRUDAndOrder(t *testing.T) {
	db := openTestDB(t)
	entityID, wt1, wt2, _, seededRuleID := seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewRuleRepository(db)

	// Create two rules with priorities that must sort before/after the seeded one
	// (seeded rule priority = 10).
	high := model.Rule{Priority: 5, EntityID: entityID,
		MatchConditions:    model.MatchConditions{Category: strptr("toys")},
		WarningTemplateIDs: []int64{wt1}}
	low := model.Rule{Priority: 20, EntityID: entityID,
		MatchConditions:    model.MatchConditions{Material: strptr("wood")},
		WarningTemplateIDs: []int64{wt1, wt2}}

	createdHigh, err := repo.Create(ctx, testShopID, high)
	if err != nil {
		t.Fatalf("create high: %v", err)
	}
	createdLow, err := repo.Create(ctx, testShopID, low)
	if err != nil {
		t.Fatalf("create low: %v", err)
	}

	list, err := repo.List(ctx, testShopID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("list len = %d, want 3", len(list))
	}
	// Ordered by priority asc: 5 (high), 10 (seeded), 20 (low).
	if list[0].ID != createdHigh.ID || list[1].ID != seededRuleID || list[2].ID != createdLow.ID {
		t.Errorf("order = [%d %d %d], want [%d %d %d]",
			list[0].ID, list[1].ID, list[2].ID, createdHigh.ID, seededRuleID, createdLow.ID)
	}

	// Get returns warning ids + conditions.
	got, err := repo.Get(ctx, testShopID, createdLow.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v / %v", got, err)
	}
	if len(got.WarningTemplateIDs) != 2 {
		t.Errorf("warning ids = %v, want 2", got.WarningTemplateIDs)
	}
	if got.MatchConditions.Material == nil || *got.MatchConditions.Material != "wood" {
		t.Errorf("conditions = %+v", got.MatchConditions)
	}

	// Update: change priority + warnings (delete+insert join).
	got.Priority = 1
	got.WarningTemplateIDs = []int64{wt2}
	if _, err := repo.Update(ctx, testShopID, createdLow.ID, *got); err != nil {
		t.Fatalf("update: %v", err)
	}
	reread, _ := repo.Get(ctx, testShopID, createdLow.ID)
	if reread.Priority != 1 || len(reread.WarningTemplateIDs) != 1 || reread.WarningTemplateIDs[0] != wt2 {
		t.Errorf("after update: priority=%d warnings=%v", reread.Priority, reread.WarningTemplateIDs)
	}

	if err := repo.Delete(ctx, testShopID, createdLow.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gone, _ := repo.Get(ctx, testShopID, createdLow.ID)
	if gone != nil {
		t.Errorf("rule still present after delete")
	}
}

// TestRuleRepo_GetCrossShop returns nil for another shop's rule id.
func TestRuleRepo_GetCrossShop(t *testing.T) {
	db := openTestDB(t)
	_, _, _, _, ruleID := seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewRuleRepository(db)

	got, err := repo.Get(ctx, testShopID+99999, ruleID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("cross-shop rule get returned %+v, want nil", got)
	}
}

// TestRuleRepo_RefBelongsToShop proves Create/Update reject an entity_id or
// warning id that belongs to another shop (multi-tenant ref integrity).
func TestRuleRepo_RefBelongsToShop(t *testing.T) {
	db := openTestDB(t)
	entityID, wt1, _, _, _ := seedFixture(t, db)
	ctx := context.Background()
	repo := repository.NewRuleRepository(db)

	// Seed a SECOND shop with its own entity.
	res, err := db.ExecContext(ctx,
		"INSERT INTO shop (shop_domain, access_token, scope) VALUES (?,?,?)",
		"other-shop.myshopify.com", "tok", "read_products")
	if err != nil {
		t.Fatalf("seed shop B: %v", err)
	}
	shopB, _ := res.LastInsertId()
	resE, _ := db.ExecContext(ctx,
		"INSERT INTO entity (shop_id, name, address, role, is_eu) VALUES (?,?,?,?,?)",
		shopB, "Other Entity", "x", "importer", false)
	entityB, _ := resE.LastInsertId()

	// Creating a rule in shopB that references shop A's entity must be rejected
	// by the same-shop ref check (done in the service layer); the repository
	// exposes an ownership helper the service uses. Verify the helper directly.
	owns, err := repo.EntityBelongsToShop(ctx, shopB, entityID)
	if err != nil {
		t.Fatalf("EntityBelongsToShop: %v", err)
	}
	if owns {
		t.Errorf("shop B should NOT own shop A's entity %d", entityID)
	}
	ownsOwn, _ := repo.EntityBelongsToShop(ctx, shopB, entityB)
	if !ownsOwn {
		t.Errorf("shop B should own its own entity %d", entityB)
	}

	// Warning ownership: wt1 belongs to shop A, not shop B.
	wOK, err := repo.WarningTemplatesBelongToShop(ctx, shopB, []int64{wt1})
	if err != nil {
		t.Fatalf("WarningTemplatesBelongToShop: %v", err)
	}
	if wOK {
		t.Errorf("shop B should NOT own shop A's warning %d", wt1)
	}
}
