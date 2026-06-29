/**
 * rulesSort.ts — Pure sorting utility for classification rules (F5).
 *
 * Extracted from RulesScreen.tsx so that:
 *  - The screen file only exports a component (required for react-refresh fast HMR).
 *  - Unit tests can import the pure function without rendering anything.
 *
 * C1: Rule precedence order = (priority asc, id asc).
 */

import type { Rule } from "../api/rules";

/**
 * Sort rules in display/precedence order: (priority asc, id asc).
 * This mirrors the engine's evaluation order exactly (C1).
 * Rank #1 = index 0 = first rule the engine will attempt to match.
 */
export function sortRulesForDisplay(rules: Rule[]): Rule[] {
  return [...rules].sort((a, b) => {
    if (a.priority !== b.priority) return a.priority - b.priority;
    return a.id - b.id;
  });
}
