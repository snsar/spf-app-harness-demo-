/**
 * Tests for src/lib/appBridge.ts
 *
 * Core behavior under test:
 *   1. Host is captured at module load time and cached in sessionStorage.
 *   2. initAppBridge() uses the cached host even after window.location.search
 *      has been emptied by React Router's redirect — this is the root-cause fix
 *      for the 401 on every /api call.
 *   3. getAppBridge() returns the initialized instance.
 *   4. initAppBridge() is a no-op (returns cached instance) if called twice.
 *   5. Without a host anywhere, initAppBridge() returns null (non-embedded path).
 *
 * Strategy: we cannot easily re-run module-level side effects in Vitest
 * (modules are cached), so we test the public API by controlling the
 * module-level `_cachedHost` via `_setCachedHostForTests` and verifying
 * that initAppBridge() uses it rather than re-reading window.location.search.
 * This is the exact scenario a merchant encounters: module loaded with host in
 * URL → redirect strips search → initAppBridge() called → must still work.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// Mock @shopify/app-bridge so tests don't need a real Shopify frame.
// We capture calls to createApp to verify the host is forwarded correctly.
const mockCreateApp = vi.fn().mockImplementation(({ apiKey, host }) => ({
  apiKey,
  host,
  __type: "MockClientApplication",
}));
vi.mock("@shopify/app-bridge", () => ({
  default: (opts: { apiKey: string; host: string }) => mockCreateApp(opts),
}));

import {
  initAppBridge,
  getAppBridge,
  _resetAppBridgeForTests,
  _setCachedHostForTests,
} from "./appBridge";

const FAKE_HOST = "dGVzdC5teXNob3BpZnkuY29tL2FkbWlu"; // base64 placeholder
const FAKE_API_KEY = "test-api-key-public";

beforeEach(() => {
  // Reset singleton state so each test starts clean.
  _resetAppBridgeForTests();
  // Reset call history AND re-assert the implementation (mockClear only clears
  // history; this guarantees createApp always returns a truthy instance so the
  // singleton's `if (_app) return _app` short-circuit works as intended).
  mockCreateApp.mockReset();
  mockCreateApp.mockImplementation(({ apiKey, host }) => ({
    apiKey,
    host,
    __type: "MockClientApplication",
  }));

  // Provide a known API key via the env-var mock.
  vi.stubEnv("VITE_SHOPIFY_API_KEY", FAKE_API_KEY);
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
  _resetAppBridgeForTests();
});

// ---------------------------------------------------------------------------
// Core: cached host survives after window.location.search is emptied
// ---------------------------------------------------------------------------

describe("initAppBridge — host caching (root-cause fix)", () => {
  it("uses the cached host even when window.location.search is empty", () => {
    // Simulate: host was captured at module load from /?host=... , then
    // React Router redirected to /products stripping the query string.
    // _setCachedHostForTests represents the module-load-time capture.
    _setCachedHostForTests(FAKE_HOST);

    // Confirm window.location.search is empty (jsdom default in tests).
    expect(window.location.search).toBe("");

    const app = initAppBridge();

    expect(app).not.toBeNull();
    expect(mockCreateApp).toHaveBeenCalledOnce();
    expect(mockCreateApp).toHaveBeenCalledWith({ apiKey: FAKE_API_KEY, host: FAKE_HOST });
  });

  it("persists the host to sessionStorage during module load so it survives redirect", () => {
    // Simulate the module-load-time capture writing to sessionStorage.
    _setCachedHostForTests(FAKE_HOST);

    expect(sessionStorage.getItem("gpsr_host")).toBe(FAKE_HOST);
  });

  it("recovers host from sessionStorage when module-level cache is null", () => {
    // Simulate scenario: user does a soft-navigation within the same tab session
    // where a new module instance starts with _cachedHost === null but
    // sessionStorage still has the host from the original load.
    sessionStorage.setItem("gpsr_host", FAKE_HOST);
    // _cachedHost is null (already reset), sessionStorage is the fallback.
    _setCachedHostForTests(null); // ensure _cachedHost is null
    // But _setCachedHostForTests(null) also clears sessionStorage — re-set it.
    sessionStorage.setItem("gpsr_host", FAKE_HOST);

    const app = initAppBridge();

    expect(app).not.toBeNull();
    expect(mockCreateApp).toHaveBeenCalledWith({ apiKey: FAKE_API_KEY, host: FAKE_HOST });
  });
});

// ---------------------------------------------------------------------------
// Singleton behaviour
// ---------------------------------------------------------------------------

describe("initAppBridge — singleton", () => {
  it("returns the same instance on repeated calls (no-op after first init)", () => {
    _setCachedHostForTests(FAKE_HOST);

    const first = initAppBridge();
    const second = initAppBridge();

    expect(first).toBe(second);
    // createApp must only be called once even though initAppBridge was called twice.
    expect(mockCreateApp).toHaveBeenCalledOnce();
  });
});

// ---------------------------------------------------------------------------
// getAppBridge accessor
// ---------------------------------------------------------------------------

describe("getAppBridge", () => {
  it("returns null before initAppBridge is called", () => {
    expect(getAppBridge()).toBeNull();
  });

  it("returns the initialized instance after initAppBridge is called", () => {
    _setCachedHostForTests(FAKE_HOST);

    const appFromInit = initAppBridge();
    const appFromGet = getAppBridge();

    expect(appFromGet).toBe(appFromInit);
    expect(appFromGet).not.toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Non-embedded path (no host anywhere)
// ---------------------------------------------------------------------------

describe("initAppBridge — non-embedded", () => {
  it("returns null when host is absent from cache, sessionStorage, and URL", () => {
    // _cachedHost is null (reset), sessionStorage is empty (reset clears it),
    // and window.location.search is "" (jsdom default).
    expect(getAppBridge()).toBeNull();

    const app = initAppBridge();

    expect(app).toBeNull();
    expect(mockCreateApp).not.toHaveBeenCalled();
  });

  it("returns null when apiKey env var is missing", () => {
    // Force the env var empty. (vi.unstubAllEnvs() alone doesn't help because
    // frontend/.env defines VITE_SHOPIFY_API_KEY, which Vite injects regardless.)
    vi.stubEnv("VITE_SHOPIFY_API_KEY", "");
    _setCachedHostForTests(FAKE_HOST);

    const app = initAppBridge();

    expect(app).toBeNull();
    expect(mockCreateApp).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// createApp call shape (App Bridge v3 single-object form)
// ---------------------------------------------------------------------------

describe("initAppBridge — createApp call shape", () => {
  it("calls createApp with a single { apiKey, host } object (v3 form, no deprecation warning)", () => {
    _setCachedHostForTests(FAKE_HOST);

    initAppBridge();

    // App Bridge v3 requires createApp({ apiKey, host }) — NOT positional args.
    // This verifies we are NOT triggering the "deprecated parameters" warning.
    const [callArg] = mockCreateApp.mock.calls[0] as [{ apiKey: string; host: string }];
    expect(callArg).toEqual({ apiKey: FAKE_API_KEY, host: FAKE_HOST });
    // Confirm there is exactly one argument (not two positional ones).
    expect(mockCreateApp.mock.calls[0]).toHaveLength(1);
  });
});
