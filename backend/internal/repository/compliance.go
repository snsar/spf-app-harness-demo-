// Package repository is the data-access layer (MySQL on port 3308). It contains
// SQL/queries only and returns models or errors — no business rules. Handlers
// must never import it directly; access goes through the service layer.
//
// compliance.go backs the F2 rules engine: it loads products, and reads/writes
// compliance records plus their warning-template join rows. Every query is
// parameterized — product fields and warning text are merchant-supplied and
// UNTRUSTED, so no value is ever interpolated into SQL.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/gpsr/backend/internal/model"
)

// ComplianceRepository persists rules-engine state against the F1 schema. It
// satisfies the service.ComplianceRepository interface.
type ComplianceRepository struct {
	db *sql.DB
}

// NewComplianceRepository wires the repository to an open *sql.DB (MySQL:3308).
func NewComplianceRepository(db *sql.DB) *ComplianceRepository {
	return &ComplianceRepository{db: db}
}

// GetProducts returns the products for the given ids. JSON `tags` is decoded to a
// string slice; NULL scalar fields decode to nil pointers so the engine reads
// them as "absent" (C4). Order follows the SQL result, not the input slice.
func (r *ComplianceRepository) GetProducts(ctx context.Context, shopID int64, ids []int64) ([]model.Product, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders, args := inClause(ids)
	// shop_id is the FIRST arg; the IN-list args follow. Every read is tenant
	// scoped: a cross-shop id silently yields no row (never another tenant's data).
	query := "SELECT id, title, tags, category, material, origin FROM product WHERE shop_id = ? AND id IN (" + placeholders + ")"
	args = append([]any{shopID}, args...)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query products: %w", err)
	}
	defer rows.Close()

	var out []model.Product
	for rows.Next() {
		var (
			p        model.Product
			tagsRaw  sql.NullString
			category sql.NullString
			material sql.NullString
			origin   sql.NullString
		)
		if err := rows.Scan(&p.ID, &p.Title, &tagsRaw, &category, &material, &origin); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		if tagsRaw.Valid && tagsRaw.String != "" {
			if err := json.Unmarshal([]byte(tagsRaw.String), &p.Tags); err != nil {
				return nil, fmt.Errorf("decode tags for product %d: %w", p.ID, err)
			}
		}
		p.Category = nullToPtr(category)
		p.Material = nullToPtr(material)
		p.Origin = nullToPtr(origin)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate products: %w", err)
	}
	return out, nil
}

// GetRecord returns the compliance record for a product (with its warning ids),
// or nil if none exists.
func (r *ComplianceRepository) GetRecord(ctx context.Context, shopID, productID int64) (*model.ComplianceRecord, error) {
	var (
		rec       model.ComplianceRecord
		matched   sql.NullInt64
		entity    sql.NullInt64
		status    string
		generated sql.NullString
	)
	err := r.db.QueryRowContext(ctx,
		"SELECT id, product_id, matched_rule_id, entity_id, status, generated_at FROM compliance_record WHERE shop_id = ? AND product_id = ?",
		shopID, productID,
	).Scan(&rec.ID, &rec.ProductID, &matched, &entity, &status, &generated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query record for product %d: %w", productID, err)
	}
	rec.Status = model.Status(status)
	if matched.Valid {
		rec.MatchedRuleID = &matched.Int64
	}
	if entity.Valid {
		rec.EntityID = &entity.Int64
	}
	if generated.Valid {
		rec.GeneratedAt = generated.String
	}

	warnings, err := r.warningIDsForRecord(ctx, r.db, rec.ID)
	if err != nil {
		return nil, err
	}
	rec.WarningTemplateIDs = warnings
	return &rec, nil
}

// SaveRecord upserts the compliance record for a product and replaces its
// warning-template join rows, atomically in one transaction. One record per
// product (the unique key on product_id makes this an upsert).
func (r *ComplianceRepository) SaveRecord(ctx context.Context, shopID int64, rec model.ComplianceRecord) error {
	if !rec.Status.Valid() {
		return fmt.Errorf("invalid status %q for product %d", rec.Status, rec.ProductID)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	// Upsert the record row by its per-shop unique (shop_id, product_id). entity_id
	// is nullable (a needs_review record has no responsible entity — C2).
	res, err := tx.ExecContext(ctx, `
		INSERT INTO compliance_record (shop_id, product_id, matched_rule_id, entity_id, status)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			matched_rule_id = VALUES(matched_rule_id),
			entity_id       = VALUES(entity_id),
			status          = VALUES(status)`,
		shopID, rec.ProductID, nullableInt(rec.MatchedRuleID), nullableInt(rec.EntityID), string(rec.Status),
	)
	if err != nil {
		return fmt.Errorf("upsert record for product %d: %w", rec.ProductID, err)
	}

	// Resolve the record id (LastInsertId is 0 on a pure update path), shop scoped.
	recordID, err := res.LastInsertId()
	if err != nil || recordID == 0 {
		if scanErr := tx.QueryRowContext(ctx,
			"SELECT id FROM compliance_record WHERE shop_id = ? AND product_id = ?", shopID, rec.ProductID,
		).Scan(&recordID); scanErr != nil {
			return fmt.Errorf("resolve record id for product %d: %w", rec.ProductID, scanErr)
		}
	}

	// Replace the warning join rows: delete then insert the current set.
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM compliance_record_warnings WHERE compliance_record_id = ?", recordID); err != nil {
		return fmt.Errorf("clear warnings for record %d: %w", recordID, err)
	}
	for _, wt := range dedupeInt64(rec.WarningTemplateIDs) {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO compliance_record_warnings (compliance_record_id, warning_template_id) VALUES (?, ?)",
			recordID, wt); err != nil {
			return fmt.Errorf("link warning %d to record %d: %w", wt, recordID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record for product %d: %w", rec.ProductID, err)
	}
	return nil
}

// DeleteRecord removes a product's compliance record. The warning join rows are
// removed by ON DELETE CASCADE.
func (r *ComplianceRepository) DeleteRecord(ctx context.Context, shopID, productID int64) error {
	if _, err := r.db.ExecContext(ctx,
		"DELETE FROM compliance_record WHERE shop_id = ? AND product_id = ?", shopID, productID); err != nil {
		return fmt.Errorf("delete record for product %d: %w", productID, err)
	}
	return nil
}

// MarkNeedsReviewIfNotOverride sets a product's compliance record to
// needs_review UNLESS it is an override (C3 — a manual override survives a
// product change; the merchant clears it). A no-op when no record exists or it is
// already needs_review (idempotent — safe for webhook replays, C7/C8).
func (r *ComplianceRepository) MarkNeedsReviewIfNotOverride(ctx context.Context, shopID, productID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE compliance_record SET status = 'needs_review'
		 WHERE shop_id = ? AND product_id = ? AND status <> 'override'`,
		shopID, productID)
	if err != nil {
		return fmt.Errorf("mark needs_review for product %d: %w", productID, err)
	}
	return nil
}

// warningIDsForRecord returns the warning template ids linked to a record,
// ordered ascending for deterministic output.
func (r *ComplianceRepository) warningIDsForRecord(ctx context.Context, q queryer, recordID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx,
		"SELECT warning_template_id FROM compliance_record_warnings WHERE compliance_record_id = ? ORDER BY warning_template_id",
		recordID)
	if err != nil {
		return nil, fmt.Errorf("query warnings for record %d: %w", recordID, err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan warning id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// queryer is the read surface shared by *sql.DB and *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// --- small helpers ------------------------------------------------------------

func nullToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func nullableInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// inClause builds "?,?,?" plus the matching args for an IN list.
func inClause(ids []int64) (string, []any) {
	placeholders := make([]byte, 0, len(ids)*2)
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	return string(placeholders), args
}

// dedupeInt64 removes duplicate ids while preserving first-seen order; a record
// has at most one row per template (PK), so duplicates in the input must be
// collapsed before insert.
func dedupeInt64(in []int64) []int64 {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(in))
	out := make([]int64, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
