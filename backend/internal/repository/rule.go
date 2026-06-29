package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/gpsr/backend/internal/model"
)

// RuleRepository is the shop-scoped CRUD store for classification rules. The
// rule's warning template ids live in the rule_warning_templates join table
// (delete+insert on update). List is ordered by (priority asc, id asc) so the
// admin UI shows precedence directly (C1).
type RuleRepository struct {
	db *sql.DB
}

// NewRuleRepository wires the repository to an open *sql.DB (MySQL:3308).
func NewRuleRepository(db *sql.DB) *RuleRepository {
	return &RuleRepository{db: db}
}

// Create inserts a rule for the shop and its warning join rows, in one tx.
func (r *RuleRepository) Create(ctx context.Context, shopID int64, rule model.Rule) (*model.Rule, error) {
	mcJSON, err := json.Marshal(rule.MatchConditions)
	if err != nil {
		return nil, fmt.Errorf("encode match_conditions: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		"INSERT INTO classification_rule (shop_id, priority, match_conditions, entity_id) VALUES (?,?,?,?)",
		shopID, rule.Priority, string(mcJSON), rule.EntityID)
	if err != nil {
		return nil, fmt.Errorf("create rule: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := replaceRuleWarnings(ctx, tx, id, rule.WarningTemplateIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit rule: %w", err)
	}
	return r.Get(ctx, shopID, id)
}

// Get loads one rule within the shop (with conditions + warning ids), or nil.
func (r *RuleRepository) Get(ctx context.Context, shopID, id int64) (*model.Rule, error) {
	var (
		rule  model.Rule
		mcRaw string
	)
	err := r.db.QueryRowContext(ctx,
		"SELECT id, priority, match_conditions, entity_id FROM classification_rule WHERE shop_id = ? AND id = ?",
		shopID, id,
	).Scan(&rule.ID, &rule.Priority, &mcRaw, &rule.EntityID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get rule %d: %w", id, err)
	}
	if err := json.Unmarshal([]byte(mcRaw), &rule.MatchConditions); err != nil {
		return nil, fmt.Errorf("decode match_conditions for rule %d: %w", id, err)
	}
	ids, err := r.warningIDs(ctx, rule.ID)
	if err != nil {
		return nil, err
	}
	rule.WarningTemplateIDs = ids
	return &rule, nil
}

// List returns the shop's rules ordered by (priority asc, id asc) — the single
// source of truth for precedence the UI renders (C1).
func (r *RuleRepository) List(ctx context.Context, shopID int64) ([]model.Rule, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, priority, match_conditions, entity_id FROM classification_rule WHERE shop_id = ? ORDER BY priority ASC, id ASC",
		shopID)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()
	var out []model.Rule
	for rows.Next() {
		var (
			rule  model.Rule
			mcRaw string
		)
		if err := rows.Scan(&rule.ID, &rule.Priority, &mcRaw, &rule.EntityID); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		if err := json.Unmarshal([]byte(mcRaw), &rule.MatchConditions); err != nil {
			return nil, fmt.Errorf("decode match_conditions: %w", err)
		}
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Attach warning ids per rule (after the rows cursor is closed).
	for i := range out {
		ids, err := r.warningIDs(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].WarningTemplateIDs = ids
	}
	return out, nil
}

// Update replaces a rule's fields + warning join within the shop, in one tx.
func (r *RuleRepository) Update(ctx context.Context, shopID, id int64, rule model.Rule) (*model.Rule, error) {
	mcJSON, err := json.Marshal(rule.MatchConditions)
	if err != nil {
		return nil, fmt.Errorf("encode match_conditions: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		"UPDATE classification_rule SET priority = ?, match_conditions = ?, entity_id = ? WHERE shop_id = ? AND id = ?",
		rule.Priority, string(mcJSON), rule.EntityID, shopID, id)
	if err != nil {
		return nil, fmt.Errorf("update rule %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Confirm the rule exists for this shop before rewriting the join.
		var exists int
		if err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM classification_rule WHERE shop_id = ? AND id = ?", shopID, id).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check rule %d: %w", id, err)
		}
		if exists == 0 {
			return nil, nil // unknown / cross-shop
		}
	}
	if err := replaceRuleWarnings(ctx, tx, id, rule.WarningTemplateIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit rule update: %w", err)
	}
	return r.Get(ctx, shopID, id)
}

// Delete removes a rule within the shop (its warning join cascades).
func (r *RuleRepository) Delete(ctx context.Context, shopID, id int64) error {
	if _, err := r.db.ExecContext(ctx,
		"DELETE FROM classification_rule WHERE shop_id = ? AND id = ?", shopID, id); err != nil {
		return fmt.Errorf("delete rule %d: %w", id, err)
	}
	return nil
}

// EntityBelongsToShop reports whether entityID exists AND is owned by shopID —
// used to reject cross-shop entity references on rule create/update.
func (r *RuleRepository) EntityBelongsToShop(ctx context.Context, shopID, entityID int64) (bool, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM entity WHERE shop_id = ? AND id = ?", shopID, entityID).Scan(&n); err != nil {
		return false, fmt.Errorf("check entity ownership: %w", err)
	}
	return n == 1, nil
}

// WarningTemplatesBelongToShop reports whether EVERY id exists AND is owned by
// shopID — used to reject cross-shop warning references on rule create/update.
func (r *RuleRepository) WarningTemplatesBelongToShop(ctx context.Context, shopID int64, ids []int64) (bool, error) {
	uniq := dedupeInt64(ids)
	if len(uniq) == 0 {
		return true, nil
	}
	placeholders, args := inClause(uniq)
	args = append([]any{shopID}, args...)
	var n int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM warning_template WHERE shop_id = ? AND id IN ("+placeholders+")",
		args...).Scan(&n); err != nil {
		return false, fmt.Errorf("check warning ownership: %w", err)
	}
	return n == len(uniq), nil
}

// warningIDs returns the warning template ids for a rule, ordered ascending.
func (r *RuleRepository) warningIDs(ctx context.Context, ruleID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT warning_template_id FROM rule_warning_templates WHERE rule_id = ? ORDER BY warning_template_id",
		ruleID)
	if err != nil {
		return nil, fmt.Errorf("query rule warnings: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan rule warning id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// replaceRuleWarnings deletes then inserts the rule's warning join rows.
func replaceRuleWarnings(ctx context.Context, tx *sql.Tx, ruleID int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM rule_warning_templates WHERE rule_id = ?", ruleID); err != nil {
		return fmt.Errorf("clear rule warnings: %w", err)
	}
	for _, wt := range dedupeInt64(ids) {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO rule_warning_templates (rule_id, warning_template_id) VALUES (?,?)", ruleID, wt); err != nil {
			return fmt.Errorf("link warning %d to rule %d: %w", wt, ruleID, err)
		}
	}
	return nil
}
