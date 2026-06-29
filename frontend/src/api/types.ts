/**
 * API types — exact shapes from F3b backend contract (snake_case).
 * All /api/* endpoints return these shapes. Keep in sync with F3b_backend_api.md.
 */

// ---- Entity ----------------------------------------------------------------

export interface Entity {
  id: number;
  name: string;
  address: string;
  role: string;
  is_eu: boolean;
  created_at: string;
  updated_at: string;
}

export interface EntityCreateBody {
  name: string;
  address: string;
  role: string;
  is_eu: boolean;
}

export interface EntityUpdateBody extends EntityCreateBody {}

export interface EntitiesResponse {
  entities: Entity[];
}

// ---- Warning Template -------------------------------------------------------

/**
 * applies_to / match_conditions shape — mirrors Go MatchConditions (snake_case JSON tags).
 * Every field is optional; an absent field is NOT a constraint on the backend.
 * category/material/origin are pointer types in Go so they may also arrive as null.
 */
export interface AppliesTo {
  tags?: string[];
  category?: string | null;
  material?: string | null;
  origin?: string | null;
}

export interface WarningTemplate {
  id: number;
  locale: string;
  text: string;
  applies_to: AppliesTo | null;
  created_at: string;
  updated_at: string;
}

export interface WarningTemplateCreateBody {
  locale?: string;
  text: string;
  applies_to?: AppliesTo;
}

export interface WarningTemplateUpdateBody extends WarningTemplateCreateBody {}

export interface WarningTemplatesResponse {
  warning_templates: WarningTemplate[];
}

// ---- Compliance Record ------------------------------------------------------

/**
 * Terminal states for a compliance record.
 *  ok           — matched a rule; entity + warnings assigned.
 *  needs_review — no rule matched; entity is null; merchant must act.
 *  override     — merchant manually assigned entity + warnings; survives re-apply.
 */
export type ComplianceStatus = "ok" | "needs_review" | "override";

export interface ComplianceRecord {
  product_id: number;
  matched_rule_id: number | null;
  entity_id: number | null;
  status: ComplianceStatus;
  warning_template_ids: number[];
}

// ---- Product ----------------------------------------------------------------

/**
 * Product as returned by GET /api/products.
 * `id` is the surrogate auto-increment PK — use it as key + for all API calls.
 * `compliance` is null when no record exists yet (never synthesized by the backend).
 */
export interface Product {
  id: number;
  title: string;
  tags: string[];
  category: string | null;
  material: string | null;
  origin: string | null;
  compliance: ComplianceRecord | null;
}

export interface ProductsResponse {
  products: Product[];
  page: number;
  has_next: boolean;
}

// ---- Bulk compliance --------------------------------------------------------

/**
 * Response from POST /api/compliance/apply.
 * `applied` = count of records written.
 * Per-product detail is fetched via GET /api/products after apply completes.
 */
export interface ApplyResponse {
  applied: number;
}

/**
 * POST /api/compliance/override body.
 */
export interface OverrideBody {
  product_id: number;
  entity_id: number;
  warning_template_ids: number[];
}

/**
 * Response from POST /api/compliance/override (200).
 */
export interface OverrideRecord {
  product_id: number;
  entity_id: number;
  status: "override";
  matched_rule_id: null;
  warning_template_ids: number[];
}

// ---- Shared -----------------------------------------------------------------

/** Pagination wrapper (used by products; entities/templates do not paginate for MVP) */
export interface Paginated {
  page: number;
  has_next: boolean;
}

/** Standard error shape from backend */
export interface ApiError {
  error: string;
}
