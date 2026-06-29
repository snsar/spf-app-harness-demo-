// Package model holds domain structs with explicit JSON tags (snake_case).
// These define the request/response contracts shared with the React admin and
// the Shopify theme extension, and the inputs/outputs of the rules engine.
package model

// Status is the terminal compliance state of a product's record. Exactly one of
// these values is always set (C2 — never silently empty).
type Status string

const (
	// StatusOK — a classification rule matched; entity + warnings are inferred.
	StatusOK Status = "ok"
	// StatusNeedsReview — no rule matched; nothing is invented (C2). The
	// merchant must resolve it manually.
	StatusNeedsReview Status = "needs_review"
	// StatusOverride — a manual override wins over inference (C3).
	StatusOverride Status = "override"
)

// Valid reports whether s is one of the three terminal states.
func (s Status) Valid() bool {
	switch s {
	case StatusOK, StatusNeedsReview, StatusOverride:
		return true
	default:
		return false
	}
}

// Entity is a responsible economic operator (manufacturer, importer, …).
// Mirrors the `entity` table.
type Entity struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Role      string `json:"role"`
	IsEU      bool   `json:"is_eu"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// WarningTemplate is editable safety-warning text (data, not code).
// `Text` is merchant-supplied and UNTRUSTED — escape before rendering (C10).
type WarningTemplate struct {
	ID        int64            `json:"id"`
	Locale    string           `json:"locale"`
	Text      string           `json:"text"`
	AppliesTo *MatchConditions `json:"applies_to,omitempty"`
	CreatedAt string           `json:"created_at,omitempty"`
	UpdatedAt string           `json:"updated_at,omitempty"`
}

// Product is the local mirror of a Shopify product — only the fields rules match
// on. A nil/empty field means the field is ABSENT, which must read as "does not
// match" (C4), never "matches empty". Pointers distinguish absent from empty
// string for the scalar fields.
type Product struct {
	ID       int64    `json:"id"` // Shopify product id
	Title    string   `json:"title"`
	Tags     []string `json:"tags,omitempty"`
	Category *string  `json:"category,omitempty"`
	Material *string  `json:"material,omitempty"`
	Origin   *string  `json:"origin,omitempty"`
}

// ProductWithCompliance pairs a product with its compliance record (or nil when
// no record exists yet — C2, never synthesize). It is the shape the
// GET /api/products list and detail endpoints return.
type ProductWithCompliance struct {
	Product    Product           `json:"-"`
	Compliance *ComplianceRecord `json:"compliance"`
}

// MatchConditions is the predicate set on a classification rule. Every field is
// optional; an absent (nil/empty) field is NOT a constraint. Tags requires ALL
// listed tags to be present on the product.
type MatchConditions struct {
	Tags     []string `json:"tags,omitempty"`
	Category *string  `json:"category,omitempty"`
	Material *string  `json:"material,omitempty"`
	Origin   *string  `json:"origin,omitempty"`
}

// Rule is one ordered classification rule. Lower Priority = higher precedence;
// first match wins, ties broken by Priority then ID (C1). It carries the entity
// it assigns and the warning-template ids it emits.
type Rule struct {
	ID                 int64           `json:"id"`
	Priority           int             `json:"priority"`
	MatchConditions    MatchConditions `json:"match_conditions"`
	EntityID           int64           `json:"entity_id"`
	WarningTemplateIDs []int64         `json:"warning_template_ids"`
	CreatedAt          string          `json:"created_at,omitempty"`
	UpdatedAt          string          `json:"updated_at,omitempty"`
}

// ComplianceRecord is the inference output, one per product. MatchedRuleID is
// nil for an override or an unmatched product (audit trail — C2). Status is
// always one of the three terminal states.
type ComplianceRecord struct {
	ID                 int64   `json:"id,omitempty"`
	ProductID          int64   `json:"product_id"`
	MatchedRuleID      *int64  `json:"matched_rule_id"`
	EntityID           *int64  `json:"entity_id"`
	Status             Status  `json:"status"`
	WarningTemplateIDs []int64 `json:"warning_template_ids"`
	GeneratedAt        string  `json:"generated_at,omitempty"`
}
