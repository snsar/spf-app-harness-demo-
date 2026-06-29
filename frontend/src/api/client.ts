/// <reference types="vite/client" />
/**
 * src/api/client.ts — Shared typed API client for the GPSR admin.
 *
 * Responsibilities:
 *  1. Acquire an App Bridge session token (JWT) and attach it as
 *     Authorization: Bearer <token> on every /api call.
 *  2. Provide typed fetch functions for Entity and Warning-Template CRUD
 *     (F4 scope). Rules CRUD lives in src/api/rules.ts (F5 scope).
 *  3. Degrade gracefully outside a Shopify iframe: if App Bridge cannot
 *     return a token (not embedded), throw an ApiClientError so the app can
 *     show a clear "open from Shopify Admin" state instead of crashing.
 *
 * All field names match the F3b backend contract exactly (snake_case).
 * Backend base URL: "" (same-origin via nginx proxy); override with
 * VITE_API_BASE env var for local dev pointing at Go on :8000.
 *
 * Shared coordination note (F5):
 *   After acquiring a token we also write it to sessionStorage under
 *   "gpsr_session_token" so that src/api/rules.ts (F5 stub) can pick it up
 *   without importing this module (avoids circular deps in a parallel build).
 */

import {
  getSessionToken as bridgeGetSessionToken,
  isShopifyEmbedded,
} from "@shopify/app-bridge/utilities";
import { getAppBridge } from "../lib/appBridge";
import type {
  Entity,
  EntityCreateBody,
  EntityUpdateBody,
  EntitiesResponse,
  WarningTemplate,
  WarningTemplateCreateBody,
  WarningTemplateUpdateBody,
  WarningTemplatesResponse,
} from "./types";

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? "";

// ---------------------------------------------------------------------------
// Session-token resolution
// ---------------------------------------------------------------------------

/**
 * Acquire the Shopify session token. When running embedded, delegates to
 * App Bridge. When not embedded (e.g. local file open), throws so the caller
 * can render an "open from Shopify Admin" prompt.
 *
 * Side effect: caches the token in sessionStorage for F5's rules.ts stub.
 */
async function resolveSessionToken(): Promise<string> {
  if (!isShopifyEmbedded()) {
    // Not inside a Shopify iframe — return empty string so the request still
    // goes out and the backend 401 bubbles up with a clear error message.
    return "";
  }

  // Retrieve the App Bridge app instance initialized at startup by App.tsx.
  // If the instance is null (non-embedded path), bridgeGetSessionToken will
  // fail and the catch block below returns an empty token → backend 401 →
  // "open from Shopify Admin" message shown to the merchant.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const app = (getAppBridge() ?? undefined) as any;
  const token = await bridgeGetSessionToken(app);

  // Share with F5's rules.ts getSessionToken stub
  try {
    if (typeof sessionStorage !== "undefined") {
      sessionStorage.setItem("gpsr_session_token", token);
    }
  } catch {
    // sessionStorage unavailable (e.g. private mode) — not fatal
  }

  return token;
}

// ---------------------------------------------------------------------------
// Error type
// ---------------------------------------------------------------------------

export class ApiClientError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiClientError";
  }
}

// ---------------------------------------------------------------------------
// Core fetch wrapper
// ---------------------------------------------------------------------------

async function apiFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  let token: string;
  try {
    token = await resolveSessionToken();
  } catch {
    // App Bridge threw (not embedded / bridge not initialised) — proceed with
    // empty token; the backend will return 401 with a clear message.
    token = "";
  }

  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(options.headers ?? {}),
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
    throw new ApiClientError(res.status, message);
  }

  // 204 No Content has no body
  if (res.status === 204) return undefined as unknown as T;

  return res.json() as Promise<T>;
}

// ---------------------------------------------------------------------------
// Entity CRUD
// ---------------------------------------------------------------------------

/** GET /api/entities → Entity[] */
export async function fetchEntities(): Promise<Entity[]> {
  const data = await apiFetch<EntitiesResponse>("/api/entities");
  return data.entities;
}

/** GET /api/entities/:id → Entity */
export async function fetchEntity(id: number): Promise<Entity> {
  return apiFetch<Entity>(`/api/entities/${id}`);
}

/** POST /api/entities → Entity (201) */
export async function createEntity(body: EntityCreateBody): Promise<Entity> {
  return apiFetch<Entity>("/api/entities", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/** PUT /api/entities/:id → Entity (200) */
export async function updateEntity(
  id: number,
  body: EntityUpdateBody,
): Promise<Entity> {
  return apiFetch<Entity>(`/api/entities/${id}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

/**
 * DELETE /api/entities/:id → void (204)
 * Throws ApiClientError(409, ...) if the entity is referenced by a rule.
 */
export async function deleteEntity(id: number): Promise<void> {
  return apiFetch<void>(`/api/entities/${id}`, { method: "DELETE" });
}

// ---------------------------------------------------------------------------
// Warning Template CRUD
// ---------------------------------------------------------------------------

/** GET /api/warning-templates → WarningTemplate[] */
export async function fetchWarningTemplates(): Promise<WarningTemplate[]> {
  const data = await apiFetch<WarningTemplatesResponse>("/api/warning-templates");
  return data.warning_templates;
}

/** GET /api/warning-templates/:id → WarningTemplate */
export async function fetchWarningTemplate(id: number): Promise<WarningTemplate> {
  return apiFetch<WarningTemplate>(`/api/warning-templates/${id}`);
}

/** POST /api/warning-templates → WarningTemplate (201) */
export async function createWarningTemplate(
  body: WarningTemplateCreateBody,
): Promise<WarningTemplate> {
  return apiFetch<WarningTemplate>("/api/warning-templates", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/** PUT /api/warning-templates/:id → WarningTemplate (200) */
export async function updateWarningTemplate(
  id: number,
  body: WarningTemplateUpdateBody,
): Promise<WarningTemplate> {
  return apiFetch<WarningTemplate>(`/api/warning-templates/${id}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

/**
 * DELETE /api/warning-templates/:id → void (204)
 * Throws ApiClientError(409, ...) if the template is referenced by a rule.
 */
export async function deleteWarningTemplate(id: number): Promise<void> {
  return apiFetch<void>(`/api/warning-templates/${id}`, { method: "DELETE" });
}
