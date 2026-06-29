/**
 * Tests for the typed API client (apiClient).
 * TDD: written before the implementation.
 *
 * Strategy: mock globalThis.fetch so tests run in jsdom without a real server.
 * The getSessionToken bridge call is mocked at the module level so we do not
 * need a real Shopify frame.
 *
 * App Bridge v3 fix: client.ts now calls getAppBridge() from lib/appBridge.ts
 * to obtain the initialized app instance. We mock that module here so tests
 * control whether an instance is "present" (embedded) or null (non-embedded).
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// Mock @shopify/app-bridge/utilities before importing the client
vi.mock("@shopify/app-bridge/utilities", () => ({
  getSessionToken: vi.fn(),
  isShopifyEmbedded: vi.fn(),
}));

// Mock the appBridge singleton so tests control the instance without needing
// a real Shopify frame or valid host URL param.
vi.mock("../lib/appBridge", () => ({
  getAppBridge: vi.fn(),
  initAppBridge: vi.fn(),
  _resetAppBridgeForTests: vi.fn(),
}));

import { getSessionToken, isShopifyEmbedded } from "@shopify/app-bridge/utilities";
import { getAppBridge } from "../lib/appBridge";
import {
  fetchEntities,
  createEntity,
  updateEntity,
  deleteEntity,
  fetchWarningTemplates,
  createWarningTemplate,
  updateWarningTemplate,
  deleteWarningTemplate,
} from "./client";
import type { Entity, WarningTemplate } from "./types";

const mockGetSessionToken = vi.mocked(getSessionToken);
const mockIsEmbedded = vi.mocked(isShopifyEmbedded);
const mockGetAppBridge = vi.mocked(getAppBridge);

// Fake App Bridge app instance — opaque object; getSessionToken receives it as
// the first arg. We verify it's passed through rather than being null.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const FAKE_APP_INSTANCE = { apiKey: "9dd02bd0f4a1c7f2286123e9800d896e" } as any;

const FAKE_TOKEN = "eyJhbGciOiJSUzI1NiJ9.fake";

const ENTITY: Entity = {
  id: 100,
  name: "Acme EU GmbH",
  address: "Berlin",
  role: "importer",
  is_eu: true,
  created_at: "2026-06-29T10:00:00Z",
  updated_at: "2026-06-29T10:00:00Z",
};

const TEMPLATE: WarningTemplate = {
  id: 1,
  locale: "en",
  text: "Choking hazard. Small parts.",
  applies_to: { tags: ["toys"] },
  created_at: "2026-06-29T10:00:00Z",
  updated_at: "2026-06-29T10:00:00Z",
};

function mockFetch(status: number, body: unknown): void {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      json: () => Promise.resolve(body),
      text: () => Promise.resolve(""),
    }),
  );
}

beforeEach(() => {
  // Default: running embedded — app instance present, token resolves to FAKE_TOKEN
  mockIsEmbedded.mockReturnValue(true);
  mockGetAppBridge.mockReturnValue(FAKE_APP_INSTANCE);
  mockGetSessionToken.mockResolvedValue(FAKE_TOKEN);
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

// ---------------------------------------------------------------------------
// Session token — Authorization header
// ---------------------------------------------------------------------------
describe("apiClient — session token", () => {
  it("attaches Authorization: Bearer <token> when embedded", async () => {
    mockFetch(200, { entities: [ENTITY] });

    await fetchEntities();

    const fetchMock = vi.mocked(globalThis.fetch);
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect((init.headers as Record<string, string>)["Authorization"]).toBe(
      `Bearer ${FAKE_TOKEN}`,
    );
  });

  it("passes the App Bridge app instance from getAppBridge() to getSessionToken()", async () => {
    // Verifies the fix: client.ts must call getAppBridge() and forward the
    // instance to bridgeGetSessionToken so App Bridge v3 can issue a real JWT.
    mockFetch(200, { entities: [ENTITY] });

    await fetchEntities();

    // getSessionToken should have been called with the FAKE_APP_INSTANCE (not undefined/null)
    expect(mockGetSessionToken).toHaveBeenCalledWith(FAKE_APP_INSTANCE);
  });

  it("falls back gracefully when getAppBridge() returns null (not embedded)", async () => {
    // Simulates non-embedded: no host param → initAppBridge returned null →
    // getAppBridge() returns null → getSessionToken(undefined) throws →
    // resolveSessionToken catches and returns "" → no Authorization header →
    // backend returns 401 → ApiClientError is thrown.
    mockIsEmbedded.mockReturnValue(false);
    mockGetAppBridge.mockReturnValue(null);
    mockGetSessionToken.mockRejectedValue(new Error("not embedded"));
    mockFetch(401, { error: "unauthorized" });

    await expect(fetchEntities()).rejects.toThrow("unauthorized");

    // Confirm: no Authorization header was sent (empty token path)
    const fetchMock = vi.mocked(globalThis.fetch);
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect((init.headers as Record<string, string>)["Authorization"]).toBeUndefined();
  });

  it("still makes the request when not embedded (token empty, expect 401 handling)", async () => {
    mockIsEmbedded.mockReturnValue(false);
    // Non-embedded: token will be empty string
    mockGetSessionToken.mockRejectedValue(new Error("not embedded"));
    // Backend returns 401
    mockFetch(401, { error: "unauthorized" });

    await expect(fetchEntities()).rejects.toThrow("unauthorized");
  });
});

// ---------------------------------------------------------------------------
// Entity CRUD
// ---------------------------------------------------------------------------
describe("fetchEntities", () => {
  it("returns entities array from GET /api/entities", async () => {
    mockFetch(200, { entities: [ENTITY] });

    const result = await fetchEntities();

    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({ id: 100, name: "Acme EU GmbH", is_eu: true });
  });

  it("throws with error message on 400", async () => {
    mockFetch(400, { error: "bad request" });
    await expect(fetchEntities()).rejects.toThrow("bad request");
  });
});

describe("createEntity", () => {
  it("POSTs to /api/entities and returns the created entity (201)", async () => {
    mockFetch(201, ENTITY);

    const result = await createEntity({
      name: "Acme EU GmbH",
      address: "Berlin",
      role: "importer",
      is_eu: true,
    });

    const fetchMock = vi.mocked(globalThis.fetch);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/entities");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toMatchObject({ name: "Acme EU GmbH" });
    expect(result.id).toBe(100);
  });
});

describe("updateEntity", () => {
  it("PUTs to /api/entities/:id and returns updated entity (200)", async () => {
    const updated = { ...ENTITY, name: "Acme EU GmbH Updated" };
    mockFetch(200, updated);

    const result = await updateEntity(100, {
      name: "Acme EU GmbH Updated",
      address: "Berlin",
      role: "importer",
      is_eu: true,
    });

    const fetchMock = vi.mocked(globalThis.fetch);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/entities/100");
    expect(init.method).toBe("PUT");
    expect(result.name).toBe("Acme EU GmbH Updated");
  });
});

describe("deleteEntity", () => {
  it("sends DELETE to /api/entities/:id (204 — no body)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, status: 204, json: () => Promise.resolve(null), text: () => Promise.resolve("") }),
    );

    await deleteEntity(100);

    const fetchMock = vi.mocked(globalThis.fetch);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/entities/100");
    expect(init.method).toBe("DELETE");
  });

  it("throws '409' error message when entity is referenced by a rule", async () => {
    mockFetch(409, { error: "entity is referenced by a rule" });
    await expect(deleteEntity(100)).rejects.toThrow("entity is referenced by a rule");
  });
});

// ---------------------------------------------------------------------------
// Warning Template CRUD
// ---------------------------------------------------------------------------
describe("fetchWarningTemplates", () => {
  it("returns warning_templates array from GET /api/warning-templates", async () => {
    mockFetch(200, { warning_templates: [TEMPLATE] });

    const result = await fetchWarningTemplates();

    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({ id: 1, locale: "en" });
  });
});

describe("createWarningTemplate", () => {
  it("POSTs to /api/warning-templates and returns created template (201)", async () => {
    mockFetch(201, TEMPLATE);

    const result = await createWarningTemplate({
      text: "Choking hazard. Small parts.",
      locale: "en",
      applies_to: { tags: ["toys"] },
    });

    const fetchMock = vi.mocked(globalThis.fetch);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/warning-templates");
    expect(init.method).toBe("POST");
    expect(result.text).toBe("Choking hazard. Small parts.");
  });
});

describe("updateWarningTemplate", () => {
  it("PUTs to /api/warning-templates/:id", async () => {
    mockFetch(200, { ...TEMPLATE, text: "Updated text" });

    const result = await updateWarningTemplate(1, {
      text: "Updated text",
      locale: "en",
    });

    const fetchMock = vi.mocked(globalThis.fetch);
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/warning-templates/1");
    expect(result.text).toBe("Updated text");
  });
});

describe("deleteWarningTemplate", () => {
  it("throws '409' error message when template is referenced", async () => {
    mockFetch(409, { error: "template is referenced by a rule" });
    await expect(deleteWarningTemplate(1)).rejects.toThrow(
      "template is referenced by a rule",
    );
  });
});
