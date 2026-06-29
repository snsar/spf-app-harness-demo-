/// <reference types="vite/client" />
/**
 * src/api/products.ts — Products and compliance API calls for F6.
 *
 * Endpoints (all from F3b_backend_api.md):
 *   GET    /api/products?page=&limit=     → ProductsResponse
 *   POST   /api/compliance/apply          → ApplyResponse
 *   POST   /api/compliance/override       → OverrideRecord
 *   DELETE /api/compliance/override/:id   → void (204, idempotent)
 *
 * Session token: reads from sessionStorage("gpsr_session_token") — same
 * coordination strategy as src/api/rules.ts (F5). F4's client.ts writes the
 * token there after App Bridge resolves it.
 *
 * All field names match the F3b backend contract exactly (snake_case).
 */

import type {
  Product,
  ProductsResponse,
  ApplyResponse,
  OverrideBody,
  OverrideRecord,
} from "./types";

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? "";

// ---------------------------------------------------------------------------
// Session token (same pattern as rules.ts)
// ---------------------------------------------------------------------------

function getSessionToken(): string {
  return (
    (typeof sessionStorage !== "undefined" &&
      sessionStorage.getItem("gpsr_session_token")) ||
    ""
  );
}

// ---------------------------------------------------------------------------
// Error type
// ---------------------------------------------------------------------------

export class ProductsApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ProductsApiError";
  }
}

// ---------------------------------------------------------------------------
// Core fetch wrapper
// ---------------------------------------------------------------------------

async function apiFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
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
    let message = res.statusText || String(res.status);
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // ignore JSON parse error
    }
    throw new ProductsApiError(res.status, message);
  }

  // 204 No Content has no body
  if (res.status === 204) return undefined as unknown as T;

  return res.json() as Promise<T>;
}

// ---------------------------------------------------------------------------
// Products
// ---------------------------------------------------------------------------

export interface FetchProductsParams {
  page?: number;
  limit?: number;
}

/**
 * GET /api/products?page=&limit=
 * Default limit 50, max 250 (backend enforces the cap).
 */
export async function fetchProducts(
  params: FetchProductsParams = {},
): Promise<ProductsResponse> {
  const { page = 1, limit = 50 } = params;
  const qs = new URLSearchParams({
    page: String(page),
    limit: String(limit),
  });
  return apiFetch<ProductsResponse>(`/api/products?${qs.toString()}`);
}

// ---------------------------------------------------------------------------
// Compliance — apply ruleset
// ---------------------------------------------------------------------------

/**
 * POST /api/compliance/apply
 * Sends selected product_ids (or empty array = all shop products).
 * Returns { applied: n }. Per-product final state is read back via fetchProducts.
 */
export async function applyRuleset(productIds: number[]): Promise<ApplyResponse> {
  const body =
    productIds.length > 0 ? { product_ids: productIds } : {};
  return apiFetch<ApplyResponse>("/api/compliance/apply", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

// ---------------------------------------------------------------------------
// Compliance — override
// ---------------------------------------------------------------------------

/**
 * POST /api/compliance/override
 * Sets a manual override for a single product.
 * Cross-shop / unknown product → backend 404; propagated as ProductsApiError(404, ...).
 */
export async function setOverride(body: OverrideBody): Promise<OverrideRecord> {
  return apiFetch<OverrideRecord>("/api/compliance/override", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/**
 * DELETE /api/compliance/override/:product_id
 * Idempotent — 204 even if no override existed.
 */
export async function clearOverride(productId: number): Promise<void> {
  return apiFetch<void>(`/api/compliance/override/${productId}`, {
    method: "DELETE",
  });
}

// ---------------------------------------------------------------------------
// Helpers re-exported so tests can import only this module
// ---------------------------------------------------------------------------

export type { Product, ProductsResponse, ApplyResponse, OverrideBody, OverrideRecord };
