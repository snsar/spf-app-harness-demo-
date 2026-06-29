// Package service holds business logic for the GPSR Compliance Engine.
//
// engine.go is the PURE classification core: no DB, no I/O. Given a product and
// an ordered ruleset it produces a deterministic, auditable compliance result.
// Determinism + precedence (C1), safe defaults (C2), and partial-data handling
// (C4) all live here so they can be unit-tested without a database. Override
// resolution and persistence are layered on top in classifier.go.
package service

import (
	"sort"

	"github.com/gpsr/backend/internal/model"
)

// Classify applies an ordered ruleset to a single product and returns its
// compliance result. It NEVER consults overrides or the DB — it is the inference
// step only. Behaviour:
//
//   - Rules are evaluated in deterministic precedence order: lower priority
//     integer first, ties broken by lower rule id (C1). The first matching rule
//     wins; remaining rules are ignored.
//   - A matching rule yields StatusOK with the rule's entity, its warning
//     template ids, and the matched rule id recorded for audit.
//   - No matching rule yields StatusNeedsReview with no entity and no warnings —
//     nothing is invented (C2).
//   - A condition over a product field that is absent (nil tags/category/…) does
//     NOT match (C4); an empty rule (no conditions at all) matches nothing.
//
// The input slice is not mutated; ordering is applied to a copy.
func Classify(p model.Product, rules []model.Rule) model.ComplianceRecord {
	ordered := make([]model.Rule, len(rules))
	copy(ordered, rules)
	sortRules(ordered)

	for i := range ordered {
		r := ordered[i]
		if ruleMatches(r.MatchConditions, p) {
			ruleID := r.ID
			entityID := r.EntityID
			return model.ComplianceRecord{
				ProductID:          p.ID,
				MatchedRuleID:      &ruleID,
				EntityID:           &entityID,
				Status:             model.StatusOK,
				WarningTemplateIDs: append([]int64(nil), r.WarningTemplateIDs...),
			}
		}
	}

	// No rule matched: safe default. Invent nothing (C2).
	return model.ComplianceRecord{
		ProductID:          p.ID,
		MatchedRuleID:      nil,
		EntityID:           nil,
		Status:             model.StatusNeedsReview,
		WarningTemplateIDs: nil,
	}
}

// sortRules orders rules by precedence: priority ascending, then id ascending.
// A stable, total order makes Classify deterministic regardless of input order.
func sortRules(rules []model.Rule) {
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		return rules[i].ID < rules[j].ID
	})
}

// ruleMatches reports whether every present condition holds for the product. An
// absent condition (nil pointer / empty tag list) is not a constraint. A rule
// with NO conditions at all matches nothing — a rule that constrains nothing
// would silently classify every product, which is a false positive we refuse.
func ruleMatches(mc model.MatchConditions, p model.Product) bool {
	if isEmptyConditions(mc) {
		return false
	}
	if len(mc.Tags) > 0 && !productHasAllTags(p.Tags, mc.Tags) {
		return false
	}
	if mc.Category != nil && !scalarEquals(p.Category, *mc.Category) {
		return false
	}
	if mc.Material != nil && !scalarEquals(p.Material, *mc.Material) {
		return false
	}
	if mc.Origin != nil && !scalarEquals(p.Origin, *mc.Origin) {
		return false
	}
	return true
}

// isEmptyConditions reports whether a rule constrains nothing.
func isEmptyConditions(mc model.MatchConditions) bool {
	return len(mc.Tags) == 0 && mc.Category == nil && mc.Material == nil && mc.Origin == nil
}

// scalarEquals reports whether an optional product field equals want. A nil
// (absent) field never matches (C4) — not even against an empty want string.
func scalarEquals(field *string, want string) bool {
	if field == nil {
		return false
	}
	return *field == want
}

// productHasAllTags reports whether the product carries every required tag.
func productHasAllTags(have, required []string) bool {
	if len(have) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(have))
	for _, t := range have {
		set[t] = struct{}{}
	}
	for _, t := range required {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}
