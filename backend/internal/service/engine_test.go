package service

import (
	"reflect"
	"testing"

	"github.com/gpsr/backend/internal/model"
)

// strp is a small helper to take the address of a string literal (products use
// *string to distinguish an absent field from an empty one — C4).
func strp(s string) *string { return &s }

// --- builders for readable test ruleset construction ---------------------------

func ruleTag(id int64, prio int, entity int64, tags ...string) model.Rule {
	return model.Rule{ID: id, Priority: prio, EntityID: entity,
		MatchConditions: model.MatchConditions{Tags: tags}}
}

func ruleCategory(id int64, prio int, entity int64, cat string) model.Rule {
	return model.Rule{ID: id, Priority: prio, EntityID: entity,
		MatchConditions: model.MatchConditions{Category: strp(cat)}}
}

// TestClassify_SingleCondition is table-driven: for each match dimension
// (tag/category/material/origin) it proves the rule matches the right product
// and does NOT match when the field differs or is absent (C4).
func TestClassify_SingleCondition(t *testing.T) {
	tests := []struct {
		name      string
		rule      model.Rule
		product   model.Product
		wantMatch bool
	}{
		// --- tags ---
		{"tag matches", ruleTag(1, 10, 100, "toys"),
			model.Product{ID: 1, Tags: []string{"toys", "eu"}}, true},
		{"tag absent on product -> no match (C4)", ruleTag(1, 10, 100, "toys"),
			model.Product{ID: 1}, false},
		{"tag differs -> no match", ruleTag(1, 10, 100, "toys"),
			model.Product{ID: 1, Tags: []string{"clothing"}}, false},
		{"all tags required: missing one -> no match", ruleTag(1, 10, 100, "toys", "electronic"),
			model.Product{ID: 1, Tags: []string{"toys"}}, false},
		{"all tags required: both present -> match", ruleTag(1, 10, 100, "toys", "electronic"),
			model.Product{ID: 1, Tags: []string{"electronic", "toys", "eu"}}, true},

		// --- category ---
		{"category matches", ruleCategory(1, 10, 100, "toys"),
			model.Product{ID: 1, Category: strp("toys")}, true},
		{"category absent on product -> no match (C4)", ruleCategory(1, 10, 100, "toys"),
			model.Product{ID: 1, Category: nil}, false},
		{"category differs -> no match", ruleCategory(1, 10, 100, "toys"),
			model.Product{ID: 1, Category: strp("clothing")}, false},
		{"category empty string is not 'matches empty' (C4)",
			model.Rule{ID: 1, Priority: 10, EntityID: 100,
				MatchConditions: model.MatchConditions{Category: strp("toys")}},
			model.Product{ID: 1, Category: strp("")}, false},

		// --- material ---
		{"material matches",
			model.Rule{ID: 1, Priority: 10, EntityID: 100,
				MatchConditions: model.MatchConditions{Material: strp("plastic")}},
			model.Product{ID: 1, Material: strp("plastic")}, true},
		{"material absent -> no match (C4)",
			model.Rule{ID: 1, Priority: 10, EntityID: 100,
				MatchConditions: model.MatchConditions{Material: strp("plastic")}},
			model.Product{ID: 1, Material: nil}, false},

		// --- origin ---
		{"origin matches",
			model.Rule{ID: 1, Priority: 10, EntityID: 100,
				MatchConditions: model.MatchConditions{Origin: strp("CN")}},
			model.Product{ID: 1, Origin: strp("CN")}, true},
		{"origin absent -> no match (C4)",
			model.Rule{ID: 1, Priority: 10, EntityID: 100,
				MatchConditions: model.MatchConditions{Origin: strp("CN")}},
			model.Product{ID: 1, Origin: nil}, false},

		// --- multi-condition AND ---
		{"all conditions must hold: one fails -> no match",
			model.Rule{ID: 1, Priority: 10, EntityID: 100,
				MatchConditions: model.MatchConditions{
					Tags: []string{"toys"}, Category: strp("toys"), Origin: strp("CN")}},
			model.Product{ID: 1, Tags: []string{"toys"}, Category: strp("toys"), Origin: strp("DE")}, false},
		{"all conditions must hold: all pass -> match",
			model.Rule{ID: 1, Priority: 10, EntityID: 100,
				MatchConditions: model.MatchConditions{
					Tags: []string{"toys"}, Category: strp("toys"), Origin: strp("CN")}},
			model.Product{ID: 1, Tags: []string{"toys"}, Category: strp("toys"), Origin: strp("CN")}, true},

		// --- empty rule (no conditions) must NOT match everything: a rule that
		// constrains nothing would silently classify every product. Treat an
		// all-empty condition set as "matches nothing" to avoid false positives.
		{"rule with no conditions -> matches nothing",
			model.Rule{ID: 1, Priority: 10, EntityID: 100},
			model.Product{ID: 1, Tags: []string{"toys"}}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.product, []model.Rule{tc.rule})
			matched := got.Status == model.StatusOK
			if matched != tc.wantMatch {
				t.Fatalf("match = %v (status %q), want %v", matched, got.Status, tc.wantMatch)
			}
		})
	}
}

// TestClassify_NoMatch_NeedsReview: a product matching no rule lands in
// needs_review with no invented entity/warnings (C2).
func TestClassify_NoMatch_NeedsReview(t *testing.T) {
	p := model.Product{ID: 1, Tags: []string{"furniture"}}
	rules := []model.Rule{ruleTag(1, 10, 100, "toys")}

	got := Classify(p, rules)
	if got.Status != model.StatusNeedsReview {
		t.Fatalf("status = %q, want needs_review", got.Status)
	}
	if got.MatchedRuleID != nil {
		t.Errorf("matched_rule_id = %v, want nil for no-match", *got.MatchedRuleID)
	}
	if got.EntityID != nil {
		t.Errorf("entity_id = %v, want nil (nothing invented, C2)", *got.EntityID)
	}
	if len(got.WarningTemplateIDs) != 0 {
		t.Errorf("warnings = %v, want none invented (C2)", got.WarningTemplateIDs)
	}
}

// TestClassify_Match_RecordsAudit: a match sets status ok, the entity, the
// warning ids and ALWAYS records matched_rule_id (audit — skill requirement).
func TestClassify_Match_RecordsAudit(t *testing.T) {
	rule := model.Rule{ID: 7, Priority: 10, EntityID: 100,
		MatchConditions:    model.MatchConditions{Tags: []string{"toys"}},
		WarningTemplateIDs: []int64{1, 2}}
	p := model.Product{ID: 1, Tags: []string{"toys"}}

	got := Classify(p, []model.Rule{rule})
	if got.Status != model.StatusOK {
		t.Fatalf("status = %q, want ok", got.Status)
	}
	if got.MatchedRuleID == nil || *got.MatchedRuleID != 7 {
		t.Fatalf("matched_rule_id = %v, want 7 (always recorded on match)", got.MatchedRuleID)
	}
	if got.EntityID == nil || *got.EntityID != 100 {
		t.Fatalf("entity_id = %v, want 100", got.EntityID)
	}
	if !reflect.DeepEqual(got.WarningTemplateIDs, []int64{1, 2}) {
		t.Fatalf("warnings = %v, want [1 2]", got.WarningTemplateIDs)
	}
	if got.ProductID != 1 {
		t.Errorf("product_id = %d, want 1", got.ProductID)
	}
}

// TestClassify_Precedence_FirstMatchWins covers C1: when several rules match,
// the lowest priority integer wins; ties break by rule id; both regardless of
// the order the rules are passed in (deterministic).
func TestClassify_Precedence_FirstMatchWins(t *testing.T) {
	p := model.Product{ID: 1, Tags: []string{"toys"}}

	t.Run("lower priority wins", func(t *testing.T) {
		// Passed high-priority-number first to prove ordering is by value, not slice order.
		rules := []model.Rule{
			ruleTag(2, 20, 200, "toys"), // priority 20
			ruleTag(1, 10, 100, "toys"), // priority 10 -> should win
		}
		got := Classify(p, rules)
		if got.MatchedRuleID == nil || *got.MatchedRuleID != 1 {
			t.Fatalf("matched_rule_id = %v, want 1 (priority 10 wins)", got.MatchedRuleID)
		}
	})

	t.Run("tie on priority -> lower id wins", func(t *testing.T) {
		rules := []model.Rule{
			ruleTag(9, 10, 900, "toys"),
			ruleTag(4, 10, 400, "toys"), // same priority, lower id -> wins
		}
		got := Classify(p, rules)
		if got.MatchedRuleID == nil || *got.MatchedRuleID != 4 {
			t.Fatalf("matched_rule_id = %v, want 4 (tie -> lower id)", got.MatchedRuleID)
		}
	})
}

// TestClassify_Determinism: same product + ruleset always yields an identical
// record, including under shuffled input order (C1 / skill determinism).
func TestClassify_Determinism(t *testing.T) {
	p := model.Product{ID: 1, Tags: []string{"toys"}, Category: strp("toys")}
	base := []model.Rule{
		ruleTag(3, 30, 300, "toys"),
		ruleCategory(2, 20, 200, "toys"),
		ruleTag(1, 10, 100, "toys"),
	}
	shuffled := []model.Rule{base[2], base[0], base[1]}

	a := Classify(p, base)
	b := Classify(p, shuffled)
	c := Classify(p, base)

	if !reflect.DeepEqual(a, b) {
		t.Fatalf("not order-independent:\n base=%+v\n shuffled=%+v", a, b)
	}
	if !reflect.DeepEqual(a, c) {
		t.Fatalf("not repeatable:\n run1=%+v\n run2=%+v", a, c)
	}
}

// TestClassify_Invariant_TerminalState is the C2 property test: across a broad
// matrix of products and rulesets, EVERY result is exactly one of the three
// terminal states — never empty/unset. On match, audit + entity are present;
// on no-match nothing is invented.
func TestClassify_Invariant_TerminalState(t *testing.T) {
	rules := []model.Rule{
		ruleTag(1, 10, 100, "toys"),
		ruleCategory(2, 20, 200, "electronics"),
		{ID: 3, Priority: 30, EntityID: 300,
			MatchConditions: model.MatchConditions{Material: strp("plastic")}},
	}

	products := []model.Product{
		{ID: 1, Tags: []string{"toys"}},
		{ID: 2, Category: strp("electronics")},
		{ID: 3, Material: strp("plastic")},
		{ID: 4},                                   // nothing -> needs_review
		{ID: 5, Tags: []string{"toys"}, Category: strp("electronics")}, // multi
		{ID: 6, Tags: []string{"unknown"}},        // no-match
		{ID: 7, Category: strp("")},               // empty field -> no-match (C4)
	}

	for _, p := range products {
		r := Classify(p, rules)
		if !r.Status.Valid() {
			t.Fatalf("product %d: invalid/empty status %q", p.ID, r.Status)
		}
		switch r.Status {
		case model.StatusOK:
			if r.MatchedRuleID == nil {
				t.Errorf("product %d: ok but matched_rule_id nil (audit broken)", p.ID)
			}
			if r.EntityID == nil {
				t.Errorf("product %d: ok but entity_id nil", p.ID)
			}
		case model.StatusNeedsReview:
			if r.MatchedRuleID != nil || r.EntityID != nil || len(r.WarningTemplateIDs) != 0 {
				t.Errorf("product %d: needs_review must invent nothing (C2), got %+v", p.ID, r)
			}
		default:
			t.Errorf("product %d: Classify must not produce status %q (override is set elsewhere)", p.ID, r.Status)
		}
	}
}
