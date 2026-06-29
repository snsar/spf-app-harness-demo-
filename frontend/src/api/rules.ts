/// <reference types="vite/client" />
/**
 * src/api/rules.ts — Rules-scoped API module for F5.
 *
 * F4 owns the shared App Bridge session-token setup, generic fetch wrapper,
 * and the Entity/WarningTemplate types (src/api/types.ts + client.ts).
 * This module re-exports those shared types and adds Rules CRUD on top.
 *
 * Session token: F4's client.ts writes the acquired token to
 * sessionStorage("gpsr_session_token") after App Bridge resolves it.
 * We read from there so we stay rules-scoped without importing client.ts
 * (avoids circular dependency in a parallel build).
 *
 * All field names match the backend contract exactly (snake_case, F3b_backend_api.md).
 */

// ---------------------------------------------------------------------------
// Re-export shared types from F4 — do NOT redefine them here.
// Import for local use in this file's function signatures.
// ---------------------------------------------------------------------------
import type { Entity, WarningTemplate } from "./types";
export type { Entity, WarningTemplate } from "./types";

/**
 * Classification rule, as returned by GET /api/rules (ordered by priority asc, id asc).
 *
 * match_conditions: a null/absent field means "do not constrain on this attribute" (C4).
 * Empty string or missing key = same semantics: field absent → wildcard.
 */
export interface MatchConditions {
  tags?: string[] | null;
  category?: string | null;
  material?: string | null;
  origin?: string | null;
}

export interface Rule {
  id: number;
  priority: number;
  match_conditions: MatchConditions;
  entity_id: number;
  warning_template_ids: number[];
}

// POST /api/rules body
export interface CreateRuleBody {
  priority: number;
  match_conditions: MatchConditions;
  entity_id: number;
  warning_template_ids: number[];
}

// PUT /api/rules/:id body (full replacement)
export type UpdateRuleBody = CreateRuleBody;

// ---------------------------------------------------------------------------
// Minimal fetch helper (F4 owns the shared client — this is rules-scoped only)
// ---------------------------------------------------------------------------

const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? "";

/**
 * Retrieve the App Bridge session token from the URL (embedded admin path) or
 * fall back to a stored value. F4 owns the real implementation; this is a
 * stub that works for rules-only testing.
 */
function getSessionToken(): string {
  // In the embedded Shopify Admin the session token is retrieved asynchronously
  // via @shopify/app-bridge/utilities. F4 will provide the canonical helper;
  // for F5 we read from sessionStorage as a fallback (set by the App Bridge
  // init flow that F4 owns).
  return (
    (typeof sessionStorage !== "undefined" &&
      sessionStorage.getItem("gpsr_session_token")) ||
    ""
  );
}

async function apiFetch<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const token = getSessionToken();
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...options.headers,
    },
  });

  if (!res.ok) {
    let message = res.statusText;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // ignore JSON parse error
    }
    throw new ApiError(res.status, message);
  }

  // 204 No Content has no body
  if (res.status === 204) return undefined as unknown as T;

  return res.json() as Promise<T>;
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// ---------------------------------------------------------------------------
// Rules API
// ---------------------------------------------------------------------------

export async function getRules(): Promise<Rule[]> {
  const data = await apiFetch<{ rules: Rule[] }>("/api/rules");
  return data.rules;
}

export async function getRule(id: number): Promise<Rule> {
  return apiFetch<Rule>(`/api/rules/${id}`);
}

export async function createRule(body: CreateRuleBody): Promise<Rule> {
  return apiFetch<Rule>("/api/rules", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function updateRule(id: number, body: UpdateRuleBody): Promise<Rule> {
  return apiFetch<Rule>(`/api/rules/${id}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export async function deleteRule(id: number): Promise<void> {
  return apiFetch<void>(`/api/rules/${id}`, { method: "DELETE" });
}

// ---------------------------------------------------------------------------
// Entity + Warning-template references (used by the rule form to populate pickers)
// F4 may expose these from its own module; we fetch directly here to stay rules-scoped.
// ---------------------------------------------------------------------------

export async function getEntities(): Promise<Entity[]> {
  const data = await apiFetch<{ entities: Entity[] }>("/api/entities");
  return data.entities;
}

export async function getWarningTemplates(): Promise<WarningTemplate[]> {
  const data = await apiFetch<{ warning_templates: WarningTemplate[] }>(
    "/api/warning-templates",
  );
  return data.warning_templates;
}
