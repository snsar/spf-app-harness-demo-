package service

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/gpsr/backend/internal/model"
)

// fakeRepo is an in-memory ComplianceRepository for unit-testing the classifier
// without a database. It records save calls so idempotency can be asserted.
type fakeRepo struct {
	products   map[int64]model.Product
	records    map[int64]model.ComplianceRecord // product_id -> record
	saveLog    []model.ComplianceRecord         // every save, in order (for C6)
	lastShopID int64                            // last shop scope passed (F3b)
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		products: map[int64]model.Product{},
		records:  map[int64]model.ComplianceRecord{},
	}
}

// shopID is recorded on every call so shop-scoping can be asserted.
const testShopID = int64(1)

func (f *fakeRepo) GetProducts(_ context.Context, shopID int64, ids []int64) ([]model.Product, error) {
	f.lastShopID = shopID
	out := make([]model.Product, 0, len(ids))
	for _, id := range ids {
		if p, ok := f.products[id]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeRepo) GetRecord(_ context.Context, shopID, productID int64) (*model.ComplianceRecord, error) {
	f.lastShopID = shopID
	if r, ok := f.records[productID]; ok {
		cp := r
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeRepo) SaveRecord(_ context.Context, shopID int64, r model.ComplianceRecord) error {
	f.lastShopID = shopID
	f.records[r.ProductID] = r
	f.saveLog = append(f.saveLog, r)
	return nil
}

func (f *fakeRepo) DeleteRecord(_ context.Context, shopID, productID int64) error {
	f.lastShopID = shopID
	delete(f.records, productID)
	return nil
}

// ---- helpers -----------------------------------------------------------------

func sortedRecords(m map[int64]model.ComplianceRecord) []model.ComplianceRecord {
	out := make([]model.ComplianceRecord, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProductID < out[j].ProductID })
	return out
}

// ---- tests -------------------------------------------------------------------

// TestApplyRuleset_BasicInference proves the classifier persists ok/needs_review
// records for a batch via the repository.
func TestApplyRuleset_BasicInference(t *testing.T) {
	repo := newFakeRepo()
	repo.products[1] = model.Product{ID: 1, Tags: []string{"toys"}}
	repo.products[2] = model.Product{ID: 2, Tags: []string{"furniture"}}

	rules := []model.Rule{
		{ID: 10, Priority: 10, EntityID: 100,
			MatchConditions:    model.MatchConditions{Tags: []string{"toys"}},
			WarningTemplateIDs: []int64{1}},
	}
	c := NewClassifier(repo)

	if err := c.ApplyRuleset(context.Background(), testShopID, []int64{1, 2}, rules); err != nil {
		t.Fatalf("ApplyRuleset: %v", err)
	}

	if got := repo.records[1].Status; got != model.StatusOK {
		t.Errorf("product 1 status = %q, want ok", got)
	}
	if repo.records[1].MatchedRuleID == nil || *repo.records[1].MatchedRuleID != 10 {
		t.Errorf("product 1 matched_rule_id = %v, want 10", repo.records[1].MatchedRuleID)
	}
	if got := repo.records[2].Status; got != model.StatusNeedsReview {
		t.Errorf("product 2 status = %q, want needs_review", got)
	}
}

// TestApplyRuleset_OverrideSurvivesRerun is C3 (forward): an existing override
// must NOT be re-classified by a bulk run — it stays override, untouched.
func TestApplyRuleset_OverrideSurvivesRerun(t *testing.T) {
	repo := newFakeRepo()
	repo.products[1] = model.Product{ID: 1, Tags: []string{"toys"}}

	overrideEntity := int64(999)
	repo.records[1] = model.ComplianceRecord{
		ProductID:          1,
		MatchedRuleID:      nil,
		EntityID:           &overrideEntity,
		Status:             model.StatusOverride,
		WarningTemplateIDs: []int64{42},
	}

	rules := []model.Rule{
		{ID: 10, Priority: 10, EntityID: 100,
			MatchConditions:    model.MatchConditions{Tags: []string{"toys"}},
			WarningTemplateIDs: []int64{1}},
	}
	c := NewClassifier(repo)
	if err := c.ApplyRuleset(context.Background(), testShopID, []int64{1}, rules); err != nil {
		t.Fatalf("ApplyRuleset: %v", err)
	}

	r := repo.records[1]
	if r.Status != model.StatusOverride {
		t.Fatalf("status = %q, want override to survive re-run (C3)", r.Status)
	}
	if r.EntityID == nil || *r.EntityID != 999 {
		t.Errorf("override entity changed: got %v, want 999", r.EntityID)
	}
	if !reflect.DeepEqual(r.WarningTemplateIDs, []int64{42}) {
		t.Errorf("override warnings changed: got %v, want [42]", r.WarningTemplateIDs)
	}
}

// TestSetAndClearOverride is C3 (both directions): setting an override wins; then
// clearing it lets a subsequent bulk run re-infer (override no longer present).
func TestSetAndClearOverride(t *testing.T) {
	repo := newFakeRepo()
	repo.products[1] = model.Product{ID: 1, Tags: []string{"toys"}}
	rules := []model.Rule{
		{ID: 10, Priority: 10, EntityID: 100,
			MatchConditions:    model.MatchConditions{Tags: []string{"toys"}},
			WarningTemplateIDs: []int64{1}},
	}
	c := NewClassifier(repo)
	ctx := context.Background()

	// Set override.
	if err := c.SetOverride(ctx, testShopID, 1, 999, []int64{42}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	if repo.records[1].Status != model.StatusOverride {
		t.Fatalf("after SetOverride status = %q, want override", repo.records[1].Status)
	}

	// Re-run: override wins.
	if err := c.ApplyRuleset(ctx, testShopID, []int64{1}, rules); err != nil {
		t.Fatalf("ApplyRuleset: %v", err)
	}
	if repo.records[1].Status != model.StatusOverride {
		t.Fatalf("override should survive re-run, got %q", repo.records[1].Status)
	}

	// Clear override, then re-run: inference takes over.
	if err := c.ClearOverride(ctx, testShopID, 1); err != nil {
		t.Fatalf("ClearOverride: %v", err)
	}
	if err := c.ApplyRuleset(ctx, testShopID, []int64{1}, rules); err != nil {
		t.Fatalf("ApplyRuleset after clear: %v", err)
	}
	r := repo.records[1]
	if r.Status != model.StatusOK {
		t.Fatalf("after clear+rerun status = %q, want ok (inference resumed, C3)", r.Status)
	}
	if r.MatchedRuleID == nil || *r.MatchedRuleID != 10 {
		t.Errorf("after clear matched_rule_id = %v, want 10", r.MatchedRuleID)
	}
}

// TestApplyRuleset_Idempotent is C6: re-running the same ruleset on the same
// products yields identical records (state-identical, regardless of how many
// times it runs).
func TestApplyRuleset_Idempotent(t *testing.T) {
	repo := newFakeRepo()
	repo.products[1] = model.Product{ID: 1, Tags: []string{"toys"}}
	repo.products[2] = model.Product{ID: 2, Category: strp("electronics")}
	repo.products[3] = model.Product{ID: 3, Tags: []string{"unknown"}} // needs_review

	rules := []model.Rule{
		{ID: 10, Priority: 10, EntityID: 100,
			MatchConditions:    model.MatchConditions{Tags: []string{"toys"}},
			WarningTemplateIDs: []int64{1, 2}},
		{ID: 20, Priority: 20, EntityID: 200,
			MatchConditions: model.MatchConditions{Category: strp("electronics")}},
	}
	c := NewClassifier(repo)
	ctx := context.Background()

	if err := c.ApplyRuleset(ctx, testShopID, []int64{1, 2, 3}, rules); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first := sortedRecords(repo.records)

	if err := c.ApplyRuleset(ctx, testShopID, []int64{1, 2, 3}, rules); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	second := sortedRecords(repo.records)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("not idempotent (C6):\n first =%+v\n second=%+v", first, second)
	}
}
