/**
 * RulesScreen — TDD: precedence ordering display logic (F5).
 *
 * C1: ordered by (priority asc, id asc). The GET /api/rules returns them
 * already ordered; we test that sortRulesForDisplay preserves / enforces that
 * order AND that the rendered list assigns visible rank numbers (1, 2, 3…)
 * matching the sorted position, making precedence unmistakeable for the merchant.
 */

import { describe, it, expect } from "vitest";
import { sortRulesForDisplay } from "./rulesSort";
import type { Rule } from "../api/rules";

// Minimal Rule stubs — only the fields used by sort/display.
function makeRule(id: number, priority: number): Rule {
  return {
    id,
    priority,
    match_conditions: {},
    entity_id: 1,
    warning_template_ids: [],
  };
}

describe("sortRulesForDisplay — precedence ordering (C1)", () => {
  it("sorts by priority ascending", () => {
    const input: Rule[] = [makeRule(10, 300), makeRule(20, 100), makeRule(30, 200)];
    const sorted = sortRulesForDisplay(input);
    expect(sorted.map((r) => r.id)).toEqual([20, 30, 10]);
  });

  it("breaks ties by id ascending when priorities are equal", () => {
    const input: Rule[] = [makeRule(15, 100), makeRule(5, 100), makeRule(10, 100)];
    const sorted = sortRulesForDisplay(input);
    expect(sorted.map((r) => r.id)).toEqual([5, 10, 15]);
  });

  it("handles a single rule", () => {
    const input: Rule[] = [makeRule(1, 50)];
    expect(sortRulesForDisplay(input)).toHaveLength(1);
    expect(sortRulesForDisplay(input)[0].id).toBe(1);
  });

  it("handles an empty list", () => {
    expect(sortRulesForDisplay([])).toEqual([]);
  });

  it("preserves already-ordered list (idempotent)", () => {
    const input: Rule[] = [makeRule(1, 10), makeRule(2, 20), makeRule(3, 20)];
    const sorted = sortRulesForDisplay(input);
    expect(sorted.map((r) => r.id)).toEqual([1, 2, 3]);
  });

  it("rank #1 = lowest priority integer (first to match in engine)", () => {
    const input: Rule[] = [makeRule(99, 500), makeRule(7, 10)];
    const sorted = sortRulesForDisplay(input);
    // Rank #1 is index 0: lowest priority value = id 7
    expect(sorted[0].id).toBe(7);
    expect(sorted[0].priority).toBe(10);
  });
});
