package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/gpsr/backend/internal/model"
)

// WarningTemplateRepository is the shop-scoped CRUD store for safety-warning
// templates. `text` is merchant-supplied + UNTRUSTED; stored verbatim via
// parameterized SQL (escaping happens at the storefront — C10).
type WarningTemplateRepository struct {
	db *sql.DB
}

// NewWarningTemplateRepository wires the repository to an open *sql.DB.
func NewWarningTemplateRepository(db *sql.DB) *WarningTemplateRepository {
	return &WarningTemplateRepository{db: db}
}

// Create inserts a new warning template for the shop.
func (r *WarningTemplateRepository) Create(ctx context.Context, shopID int64, w model.WarningTemplate) (*model.WarningTemplate, error) {
	locale := w.Locale
	if locale == "" {
		locale = "en"
	}
	appliesJSON, err := marshalApplies(w.AppliesTo)
	if err != nil {
		return nil, err
	}
	res, err := r.db.ExecContext(ctx,
		"INSERT INTO warning_template (shop_id, locale, text, applies_to) VALUES (?,?,?,?)",
		shopID, locale, w.Text, appliesJSON)
	if err != nil {
		return nil, fmt.Errorf("create warning template: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.Get(ctx, shopID, id)
}

// Get loads one template within the shop, or nil if unknown / another shop's.
func (r *WarningTemplateRepository) Get(ctx context.Context, shopID, id int64) (*model.WarningTemplate, error) {
	var (
		w       model.WarningTemplate
		applies sql.NullString
		created sql.NullString
		updated sql.NullString
	)
	err := r.db.QueryRowContext(ctx,
		"SELECT id, locale, text, applies_to, created_at, updated_at FROM warning_template WHERE shop_id = ? AND id = ?",
		shopID, id,
	).Scan(&w.ID, &w.Locale, &w.Text, &applies, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get warning template %d: %w", id, err)
	}
	if applies.Valid && applies.String != "" {
		var mc model.MatchConditions
		if err := json.Unmarshal([]byte(applies.String), &mc); err != nil {
			return nil, fmt.Errorf("decode applies_to for template %d: %w", id, err)
		}
		w.AppliesTo = &mc
	}
	w.CreatedAt, w.UpdatedAt = created.String, updated.String
	return &w, nil
}

// List returns all templates for the shop ordered by id.
func (r *WarningTemplateRepository) List(ctx context.Context, shopID int64) ([]model.WarningTemplate, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, locale, text, applies_to, created_at, updated_at FROM warning_template WHERE shop_id = ? ORDER BY id ASC",
		shopID)
	if err != nil {
		return nil, fmt.Errorf("list warning templates: %w", err)
	}
	defer rows.Close()
	var out []model.WarningTemplate
	for rows.Next() {
		var (
			w       model.WarningTemplate
			applies sql.NullString
			created sql.NullString
			updated sql.NullString
		)
		if err := rows.Scan(&w.ID, &w.Locale, &w.Text, &applies, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan warning template: %w", err)
		}
		if applies.Valid && applies.String != "" {
			var mc model.MatchConditions
			if err := json.Unmarshal([]byte(applies.String), &mc); err != nil {
				return nil, fmt.Errorf("decode applies_to: %w", err)
			}
			w.AppliesTo = &mc
		}
		w.CreatedAt, w.UpdatedAt = created.String, updated.String
		out = append(out, w)
	}
	return out, rows.Err()
}

// Update changes a template within the shop.
func (r *WarningTemplateRepository) Update(ctx context.Context, shopID, id int64, w model.WarningTemplate) (*model.WarningTemplate, error) {
	locale := w.Locale
	if locale == "" {
		locale = "en"
	}
	appliesJSON, err := marshalApplies(w.AppliesTo)
	if err != nil {
		return nil, err
	}
	if _, err := r.db.ExecContext(ctx,
		"UPDATE warning_template SET locale = ?, text = ?, applies_to = ? WHERE shop_id = ? AND id = ?",
		locale, w.Text, appliesJSON, shopID, id); err != nil {
		return nil, fmt.Errorf("update warning template %d: %w", id, err)
	}
	return r.Get(ctx, shopID, id)
}

// Delete removes a template within the shop. Returns ErrReferenced (C5 -> 409) if
// a classification_rule still references it (RESTRICT FK / MySQL 1451).
func (r *WarningTemplateRepository) Delete(ctx context.Context, shopID, id int64) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM warning_template WHERE shop_id = ? AND id = ?", shopID, id)
	if err != nil {
		if mysqlRowReferenced(err) {
			return ErrReferenced
		}
		return fmt.Errorf("delete warning template %d: %w", id, err)
	}
	return nil
}

// marshalApplies encodes optional match conditions to a JSON column value or nil.
func marshalApplies(mc *model.MatchConditions) (any, error) {
	if mc == nil {
		return nil, nil
	}
	b, err := json.Marshal(mc)
	if err != nil {
		return nil, fmt.Errorf("encode applies_to: %w", err)
	}
	return string(b), nil
}
