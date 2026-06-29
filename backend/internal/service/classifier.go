package service

import (
	"context"
	"fmt"

	"github.com/gpsr/backend/internal/model"
)

// ComplianceRepository is the persistence boundary the classifier depends on.
// It is an interface (not a concrete type) so the classifier stays unit-testable
// with an in-memory fake; the real MySQL implementation lives in package
// repository. Implementations must use parameterized queries — all values here
// (product fields, warning ids) originate from untrusted merchant input.
// Every method takes a leading shopID so persistence is always tenant-scoped:
// scoping is explicit and impossible to forget (F3b multi-tenant contract). The
// pure engine (Classify) is unchanged — only persistence gained the scope.
type ComplianceRepository interface {
	// GetProducts returns the products for the given ids within the shop (missing
	// or cross-shop ids omitted).
	GetProducts(ctx context.Context, shopID int64, ids []int64) ([]model.Product, error)
	// GetRecord returns the existing compliance record for a product in the shop,
	// or nil if none exists yet.
	GetRecord(ctx context.Context, shopID, productID int64) (*model.ComplianceRecord, error)
	// SaveRecord upserts a compliance record (one per product per shop).
	SaveRecord(ctx context.Context, shopID int64, r model.ComplianceRecord) error
	// DeleteRecord removes a product's compliance record in the shop (used to
	// clear an override so inference can resume).
	DeleteRecord(ctx context.Context, shopID, productID int64) error
}

// Classifier applies the rules engine to products and persists the results. It
// owns the business decisions around overrides (C3) and bulk idempotency (C6);
// the pure matching/precedence logic is delegated to Classify in engine.go.
type Classifier struct {
	repo ComplianceRepository
}

// NewClassifier wires a Classifier to its persistence backend.
func NewClassifier(repo ComplianceRepository) *Classifier {
	return &Classifier{repo: repo}
}

// ApplyRuleset infers and persists a compliance record for each product id,
// using the given ordered ruleset. This is the primary bulk operation.
//
//   - A product with an existing manual override is left untouched: the override
//     always wins over inference and stays `override` across re-runs (C3).
//   - Every other product is (re)classified by the pure engine and upserted.
//   - Re-running with the same ruleset and product state yields identical records
//     (C6 idempotency) because Classify is deterministic and SaveRecord upserts.
//
// Products whose id is unknown to the repository are skipped silently — there is
// nothing to classify. The operation is best-effort per product but fails fast:
// the first persistence error aborts and is returned with context.
func (c *Classifier) ApplyRuleset(ctx context.Context, shopID int64, productIDs []int64, rules []model.Rule) error {
	products, err := c.repo.GetProducts(ctx, shopID, productIDs)
	if err != nil {
		return fmt.Errorf("load products: %w", err)
	}

	for _, p := range products {
		existing, err := c.repo.GetRecord(ctx, shopID, p.ID)
		if err != nil {
			return fmt.Errorf("load record for product %d: %w", p.ID, err)
		}
		// C3: a manual override wins over inference; never overwrite it.
		if existing != nil && existing.Status == model.StatusOverride {
			continue
		}

		rec := Classify(p, rules)
		if err := c.repo.SaveRecord(ctx, shopID, rec); err != nil {
			return fmt.Errorf("save record for product %d: %w", p.ID, err)
		}
	}
	return nil
}

// SetOverride records a manual override for a product: the merchant-chosen entity
// and warning templates win over inference, and the record is marked `override`
// with no matched rule id (audit: NULL means override or no-match — C2). An
// override set this way survives subsequent ApplyRuleset runs (C3).
func (c *Classifier) SetOverride(ctx context.Context, shopID, productID, entityID int64, warningTemplateIDs []int64) error {
	rec := model.ComplianceRecord{
		ProductID:          productID,
		MatchedRuleID:      nil,
		EntityID:           &entityID,
		Status:             model.StatusOverride,
		WarningTemplateIDs: append([]int64(nil), warningTemplateIDs...),
	}
	if err := c.repo.SaveRecord(ctx, shopID, rec); err != nil {
		return fmt.Errorf("set override for product %d: %w", productID, err)
	}
	return nil
}

// ClearOverride removes a product's record so that a subsequent ApplyRuleset
// re-infers it from the ruleset (C3, reverse direction). Deleting (rather than
// rewriting to needs_review) keeps the operation simple and lets the next run
// produce a fresh, fully-audited record.
func (c *Classifier) ClearOverride(ctx context.Context, shopID, productID int64) error {
	if err := c.repo.DeleteRecord(ctx, shopID, productID); err != nil {
		return fmt.Errorf("clear override for product %d: %w", productID, err)
	}
	return nil
}
