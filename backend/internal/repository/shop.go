package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gpsr/backend/internal/model"
)

// ShopRepository persists per-shop OAuth install state in the `shop` table
// (migration 002). Every query is parameterized; the access token is stored but
// never logged here.
type ShopRepository struct {
	db *sql.DB
}

// NewShopRepository wires the repository to an open *sql.DB (MySQL:3308).
func NewShopRepository(db *sql.DB) *ShopRepository {
	return &ShopRepository{db: db}
}

// Upsert inserts a shop or refreshes its token + scope on re-install, keyed by
// the UNIQUE shop_domain. Idempotent: re-running with the same values is a no-op
// beyond touching updated_at.
func (r *ShopRepository) Upsert(ctx context.Context, shopDomain, accessToken, scope string) error {
	const q = `
		INSERT INTO shop (shop_domain, access_token, scope)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE
			access_token = VALUES(access_token),
			scope        = VALUES(scope),
			updated_at   = CURRENT_TIMESTAMP`
	if _, err := r.db.ExecContext(ctx, q, shopDomain, accessToken, scope); err != nil {
		// Never include the token in the error message.
		return fmt.Errorf("upsert shop %q: %w", shopDomain, err)
	}
	return nil
}

// GetShopCredentials returns the shop domain and offline access token for a
// shop by its surrogate id. It satisfies service.ShopTokenReader.
// The token is SECRET and must never be logged by the caller.
// Returns ("", "", nil) when no such shop exists.
func (r *ShopRepository) GetShopCredentials(ctx context.Context, shopID int64) (domain, token string, err error) {
	err = r.db.QueryRowContext(ctx,
		"SELECT shop_domain, COALESCE(access_token, '') FROM shop WHERE id = ?", shopID,
	).Scan(&domain, &token)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get shop credentials for shop %d: %w", shopID, err)
	}
	return domain, token, nil
}

// TeardownShop removes all data for the given shop in the order required by the
// FK constraints proven in F3b (TestMigration003_ShopCascade):
//  1. DELETE compliance_record WHERE shop_id (removes rule→entity RESTRICT refs
//     held by compliance_record.entity_id SET NULL — cleared first so RESTRICT on
//     rule.entity_id is unblocked for step 2).
//  2. DELETE classification_rule WHERE shop_id (removes rule→entity RESTRICT refs).
//  3. DELETE shop WHERE id (ON DELETE CASCADE removes entity / warning_template /
//     product and all their join rows in one shot).
//
// All three statements run in a single transaction so a partial failure leaves
// the DB consistent and the shop row intact for retry.
func (r *ShopRepository) TeardownShop(ctx context.Context, shopID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin teardown tx for shop %d: %w", shopID, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Step 1: compliance_record (its entity_id FK is SET NULL, but we delete it
	// explicitly before the rule rows so the rule->entity RESTRICT is clear).
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM compliance_record WHERE shop_id = ?", shopID); err != nil {
		return fmt.Errorf("teardown compliance_record for shop %d: %w", shopID, err)
	}

	// Step 2: classification_rule (references entity via RESTRICT FK).
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM classification_rule WHERE shop_id = ?", shopID); err != nil {
		return fmt.Errorf("teardown classification_rule for shop %d: %w", shopID, err)
	}

	// Step 3: shop row — ON DELETE CASCADE handles entity, warning_template,
	// product, and all remaining join rows (rule_warning_templates,
	// compliance_record_warnings).
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM shop WHERE id = ?", shopID); err != nil {
		return fmt.Errorf("teardown shop %d: %w", shopID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit teardown for shop %d: %w", shopID, err)
	}
	return nil
}

// GetByDomain loads a shop by its myshopify domain, or returns (nil, nil) when
// no such shop is installed.
func (r *ShopRepository) GetByDomain(ctx context.Context, shopDomain string) (*model.Shop, error) {
	const q = `
		SELECT id, shop_domain, COALESCE(access_token, ''), COALESCE(scope, ''),
		       installed_at, updated_at
		FROM shop WHERE shop_domain = ?`
	var (
		s           model.Shop
		installedAt sql.NullString
		updatedAt   sql.NullString
	)
	err := r.db.QueryRowContext(ctx, q, shopDomain).Scan(
		&s.ID, &s.ShopDomain, &s.AccessToken, &s.Scope, &installedAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get shop %q: %w", shopDomain, err)
	}
	s.InstalledAt = installedAt.String
	s.UpdatedAt = updatedAt.String
	return &s, nil
}
