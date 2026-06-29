package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/gpsr/backend/internal/model"
)

// ProductRepository persists the local mirror of Shopify products, scoped per
// shop (F3b). Every query filters by shop_id; the merchant-supplied product
// fields are UNTRUSTED so all writes are parameterized. The natural key is
// (shop_id, shopify_product_id); product.id is an opaque surrogate (Q1 Opt A).
type ProductRepository struct {
	db *sql.DB
}

// NewProductRepository wires the repository to an open *sql.DB (MySQL:3308).
func NewProductRepository(db *sql.DB) *ProductRepository {
	return &ProductRepository{db: db}
}

// maxLimit / defaultLimit bound page sizes (Q4). A caller may not exceed maxLimit.
const (
	defaultLimit = 50
	maxLimit     = 250
)

// Upsert inserts or updates a product for the shop, keyed by
// (shop_id, shopify_product_id) — idempotent (C7). Returns the surrogate id.
// Nil scalar fields (material/origin/category absent) are stored NULL so the
// engine reads them as absent (C4). tags is stored as a JSON array.
func (r *ProductRepository) Upsert(ctx context.Context, shopID, shopifyProductID int64, p model.Product) (int64, error) {
	var tagsJSON any
	if len(p.Tags) > 0 {
		b, err := json.Marshal(p.Tags)
		if err != nil {
			return 0, fmt.Errorf("encode tags: %w", err)
		}
		tagsJSON = string(b)
	}

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO product (shop_id, shopify_product_id, title, tags, category, material, origin)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			title    = VALUES(title),
			tags     = VALUES(tags),
			category = VALUES(category),
			material = VALUES(material),
			origin   = VALUES(origin)`,
		shopID, shopifyProductID, p.Title, tagsJSON,
		ptrToNull(p.Category), ptrToNull(p.Material), ptrToNull(p.Origin),
	)
	if err != nil {
		return 0, fmt.Errorf("upsert product (shop %d, shopify %d): %w", shopID, shopifyProductID, err)
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		// On a pure update the auto-increment id is not returned; resolve it.
		if scanErr := r.db.QueryRowContext(ctx,
			"SELECT id FROM product WHERE shop_id = ? AND shopify_product_id = ?",
			shopID, shopifyProductID).Scan(&id); scanErr != nil {
			return 0, fmt.Errorf("resolve product id: %w", scanErr)
		}
	}
	return id, nil
}

// ListWithCompliance returns one page of the shop's products (each with its
// compliance record or nil) ordered by id, and whether a further page exists.
// page is 1-based; limit is clamped to [1, maxLimit] with a default (Q4 offset).
func (r *ProductRepository) ListWithCompliance(ctx context.Context, shopID int64, page, limit int) ([]model.ProductWithCompliance, bool, error) {
	limit = clampLimit(limit)
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	// Fetch limit+1 to detect a next page without a second COUNT round-trip.
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, shopify_product_id, title, tags, category, material, origin
		FROM product WHERE shop_id = ?
		ORDER BY id ASC
		LIMIT ? OFFSET ?`,
		shopID, limit+1, offset)
	if err != nil {
		return nil, false, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	var products []model.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, false, err
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate products: %w", err)
	}

	hasNext := false
	if len(products) > limit {
		hasNext = true
		products = products[:limit]
	}

	out := make([]model.ProductWithCompliance, 0, len(products))
	for _, p := range products {
		rec, err := r.recordForProduct(ctx, shopID, p.ID)
		if err != nil {
			return nil, false, err
		}
		out = append(out, model.ProductWithCompliance{Product: p, Compliance: rec})
	}
	return out, hasNext, nil
}

// GetShopifyProductID returns the Shopify product id (the remote ID stored in
// shopify_product_id) for a local surrogate product id within the shop.
// Returns (0, nil) when the product does not exist or belongs to another shop.
// Used by the metafield sync path to form the Shopify product GID.
func (r *ProductRepository) GetShopifyProductID(ctx context.Context, shopID, productID int64) (int64, error) {
	var shopifyProdID int64
	err := r.db.QueryRowContext(ctx,
		"SELECT shopify_product_id FROM product WHERE shop_id = ? AND id = ?",
		shopID, productID,
	).Scan(&shopifyProdID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get shopify_product_id for product %d: %w", productID, err)
	}
	return shopifyProdID, nil
}

// GetWithCompliance returns a single product (by surrogate id) within the shop
// with its compliance record, or nil if the id is unknown / belongs to another
// shop (cross-shop reads never leak — they map to not-found).
func (r *ProductRepository) GetWithCompliance(ctx context.Context, shopID, productID int64) (*model.ProductWithCompliance, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, shopify_product_id, title, tags, category, material, origin
		FROM product WHERE shop_id = ? AND id = ?`,
		shopID, productID)
	p, err := scanProduct(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec, err := r.recordForProduct(ctx, shopID, p.ID)
	if err != nil {
		return nil, err
	}
	return &model.ProductWithCompliance{Product: p, Compliance: rec}, nil
}

// recordForProduct loads the compliance record (with warning ids) for a product
// in the shop, or nil if none exists.
func (r *ProductRepository) recordForProduct(ctx context.Context, shopID, productID int64) (*model.ComplianceRecord, error) {
	var (
		rec     model.ComplianceRecord
		matched sql.NullInt64
		entity  sql.NullInt64
		status  string
	)
	err := r.db.QueryRowContext(ctx,
		"SELECT id, product_id, matched_rule_id, entity_id, status FROM compliance_record WHERE shop_id = ? AND product_id = ?",
		shopID, productID,
	).Scan(&rec.ID, &rec.ProductID, &matched, &entity, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load record for product %d: %w", productID, err)
	}
	rec.Status = model.Status(status)
	if matched.Valid {
		rec.MatchedRuleID = &matched.Int64
	}
	if entity.Valid {
		rec.EntityID = &entity.Int64
	}
	warnings, err := warningIDsForComplianceRecord(ctx, r.db, rec.ID)
	if err != nil {
		return nil, err
	}
	rec.WarningTemplateIDs = warnings
	return &rec, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanProduct decodes a product row (surrogate id as Product.ID; JSON tags;
// NULL scalars -> nil pointers for C4).
func scanProduct(s rowScanner) (model.Product, error) {
	var (
		p         model.Product
		shopifyID int64
		tagsRaw   sql.NullString
		category  sql.NullString
		material  sql.NullString
		origin    sql.NullString
	)
	if err := s.Scan(&p.ID, &shopifyID, &p.Title, &tagsRaw, &category, &material, &origin); err != nil {
		return model.Product{}, err
	}
	if tagsRaw.Valid && tagsRaw.String != "" {
		if err := json.Unmarshal([]byte(tagsRaw.String), &p.Tags); err != nil {
			return model.Product{}, fmt.Errorf("decode tags for product %d: %w", p.ID, err)
		}
	}
	p.Category = nullToPtr(category)
	p.Material = nullToPtr(material)
	p.Origin = nullToPtr(origin)
	return p, nil
}

// clampLimit bounds a requested page size to [1, maxLimit], defaulting when <= 0.
func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// ptrToNull turns a *string into a value or nil for parameterized NULL inserts.
func ptrToNull(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// warningIDsForComplianceRecord returns the warning template ids linked to a
// compliance record, ordered ascending.
func warningIDsForComplianceRecord(ctx context.Context, q queryer, recordID int64) ([]int64, error) {
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
