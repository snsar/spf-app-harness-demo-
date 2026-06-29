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
