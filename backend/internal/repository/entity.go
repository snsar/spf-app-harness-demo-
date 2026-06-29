package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gpsr/backend/internal/model"
)

// EntityRepository is the shop-scoped CRUD store for responsible economic
// operators. Every query filters by shop_id; merchant text is parameterized.
type EntityRepository struct {
	db *sql.DB
}

// NewEntityRepository wires the repository to an open *sql.DB (MySQL:3308).
func NewEntityRepository(db *sql.DB) *EntityRepository {
	return &EntityRepository{db: db}
}

// Create inserts a new entity for the shop and returns it with its id + timestamps.
func (r *EntityRepository) Create(ctx context.Context, shopID int64, e model.Entity) (*model.Entity, error) {
	res, err := r.db.ExecContext(ctx,
		"INSERT INTO entity (shop_id, name, address, role, is_eu) VALUES (?,?,?,?,?)",
		shopID, e.Name, e.Address, e.Role, e.IsEU)
	if err != nil {
		return nil, fmt.Errorf("create entity: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.Get(ctx, shopID, id)
}

// Get loads one entity within the shop, or nil if unknown / another shop's.
func (r *EntityRepository) Get(ctx context.Context, shopID, id int64) (*model.Entity, error) {
	var e model.Entity
	var created, updated sql.NullString
	err := r.db.QueryRowContext(ctx,
		"SELECT id, name, address, role, is_eu, created_at, updated_at FROM entity WHERE shop_id = ? AND id = ?",
		shopID, id,
	).Scan(&e.ID, &e.Name, &e.Address, &e.Role, &e.IsEU, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get entity %d: %w", id, err)
	}
	e.CreatedAt, e.UpdatedAt = created.String, updated.String
	return &e, nil
}

// List returns all entities for the shop ordered by id.
func (r *EntityRepository) List(ctx context.Context, shopID int64) ([]model.Entity, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, name, address, role, is_eu, created_at, updated_at FROM entity WHERE shop_id = ? ORDER BY id ASC",
		shopID)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	defer rows.Close()
	var out []model.Entity
	for rows.Next() {
		var e model.Entity
		var created, updated sql.NullString
		if err := rows.Scan(&e.ID, &e.Name, &e.Address, &e.Role, &e.IsEU, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}
		e.CreatedAt, e.UpdatedAt = created.String, updated.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// Update changes an entity within the shop. Returns nil if the id is unknown /
// belongs to another shop.
func (r *EntityRepository) Update(ctx context.Context, shopID, id int64, e model.Entity) (*model.Entity, error) {
	res, err := r.db.ExecContext(ctx,
		"UPDATE entity SET name = ?, address = ?, role = ?, is_eu = ? WHERE shop_id = ? AND id = ?",
		e.Name, e.Address, e.Role, e.IsEU, shopID, id)
	if err != nil {
		return nil, fmt.Errorf("update entity %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Either unknown id or a no-op update; distinguish via a read.
		return r.Get(ctx, shopID, id)
	}
	return r.Get(ctx, shopID, id)
}

// Delete removes an entity within the shop. Returns ErrReferenced (C5 -> 409) if
// a classification_rule still references it (RESTRICT FK / MySQL 1451).
func (r *EntityRepository) Delete(ctx context.Context, shopID, id int64) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM entity WHERE shop_id = ? AND id = ?", shopID, id)
	if err != nil {
		if mysqlRowReferenced(err) {
			return ErrReferenced
		}
		return fmt.Errorf("delete entity %d: %w", id, err)
	}
	return nil
}
