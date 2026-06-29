/// <reference types="vite/client" />
/**
 * src/lib/appBridge.ts — App Bridge v3 initialization singleton.
 *
 * Initializes the App Bridge app instance ONCE at startup when running
 * embedded in Shopify Admin. Exports a `getAppBridge()` accessor so that
 * any module (client.ts, rules.ts, etc.) can retrieve the instance without
 * circular imports.
 *
 * createApp requires:
 *   - apiKey: the PUBLIC Shopify app client_id (VITE_SHOPIFY_API_KEY env var).
 *     This is a public identifier — safe to ship in client-side code.
 *     Do NOT use the API secret here.
 *   - host: the base64-encoded host provided by Shopify as a URL query param
 *     when it opens the app in the iframe. Without a valid host the App Bridge
 *     can't verify the session JWT or route embedded navigation correctly.
 *
 * HOST CAPTURE STRATEGY (fixes 401 from lost host after React Router redirect):
 *   Shopify opens the iframe at `/?host=<base64>&embedded=1&shop=...`.
 *   React Router's BrowserRouter + <Navigate to="/products" replace /> drops
 *   the query string, so window.location.search becomes "" after the first
 *   render — and a late call to getHostParam() returns null.
 *
 *   Fix: capture `host` AT MODULE LOAD TIME (i.e. when this file is first
 *   imported — before React Router mounts or redirects anything).  The value
 *   is stored in two places so it survives the redirect AND any future
 *   page-refresh without the query string:
 *     1. Module-level variable `_cachedHost` (lives for the page's lifetime).
 *     2. sessionStorage key "gpsr_host" (survives same-origin navigations).
 *
 *   initAppBridge() reads `_cachedHost` first, then falls back to
 *   sessionStorage, then to the live URL param (covers edge cases where the
 *   module is freshly loaded on a non-root URL that still carries the param).
 *
 * Non-embedded path: if `host` cannot be found anywhere, we skip createApp
 * entirely. The rest of the app degrades gracefully — API calls will get a
 * 401 and screens show the "open from Shopify Admin" banner.
 */

import createApp from "@shopify/app-bridge";
import type { ClientApplication } from "@shopify/app-bridge/client/types";

// ---------------------------------------------------------------------------
// Host capture — must run at module load time, before any React Router render
// ---------------------------------------------------------------------------

/**
 * The sessionStorage key used to persist the Shopify host param across
 * same-origin navigation (e.g. after the Router drops the query string).
 */
const HOST_STORAGE_KEY = "gpsr_host";

/**
 * Read `host` from the current URL's query string.
 * Returns null when not present (non-embedded, or redirect already happened).
 */
function readHostFromUrl(): string | null {
  try {
    return new URLSearchParams(window.location.search).get("host");
  } catch {
    return null;
  }
}

/**
 * Read the previously persisted host from sessionStorage.
 * Returns null if unavailable (e.g. private-browsing restrictions, or first load).
 */
function readHostFromStorage(): string | null {
  try {
    return (
      (typeof sessionStorage !== "undefined" &&
        sessionStorage.getItem(HOST_STORAGE_KEY)) ||
      null
    );
  } catch {
    return null;
  }
}

/**
 * Persist the host in sessionStorage so it survives the React Router redirect
 * that strips the query string from window.location.
 */
function persistHost(host: string): void {
  try {
    if (typeof sessionStorage !== "undefined") {
      sessionStorage.setItem(HOST_STORAGE_KEY, host);
    }
  } catch {
    // sessionStorage unavailable (private mode, etc.) — module-level cache
    // still covers the current page lifetime.
  }
}

// Capture host immediately at module load, before React Router can redirect.
// If the URL has the param right now (first iframe load from Shopify), lock it
// in. Otherwise fall back to sessionStorage for subsequent navigations.
let _cachedHost: string | null = readHostFromUrl();
if (_cachedHost) {
  // Persist so subsequent re-renders / navigations can still find it.
  persistHost(_cachedHost);
} else {
  // Module was loaded after a redirect — recover from sessionStorage.
  _cachedHost = readHostFromStorage();
}

// ---------------------------------------------------------------------------
// Singleton state
// ---------------------------------------------------------------------------

let _app: ClientApplication | null = null;

// ---------------------------------------------------------------------------
// Internal host resolution
// ---------------------------------------------------------------------------

/**
 * Return the best available host value, trying (in order):
 *   1. Module-level cache (captured at load time — most reliable).
 *   2. sessionStorage (survives a redirect within the same tab session).
 *   3. Live URL param (edge case: module freshly loaded on a URL that still
 *      carries the param; also keeps the old getHostParam() code path alive
 *      for any scenario we haven't anticipated).
 *
 * Returns null only when none of the three sources has the value — meaning
 * the app is running outside Shopify.
 */
function resolveHost(): string | null {
  return _cachedHost ?? readHostFromStorage() ?? readHostFromUrl();
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Initialize App Bridge v3.
 *
 * Call this ONCE, at app startup (before the router renders), in App.tsx or
 * main.tsx. Calling it multiple times is a no-op (returns the cached instance).
 *
 * Returns the app instance when embedded (host present + apiKey configured),
 * or null when running outside Shopify.
 */
export function initAppBridge(): ClientApplication | null {
  if (_app) return _app;

  const apiKey =
    (import.meta.env.VITE_SHOPIFY_API_KEY as string | undefined) ?? "";
  const host = resolveHost();

  if (!apiKey || !host) {
    // Not embedded (or env var missing): skip init, fall back gracefully.
    return null;
  }

  try {
    // App Bridge v3: pass a single object to createApp (silences the
    // "deprecated parameters" console warning from the bridge's own check).
    _app = createApp({ apiKey, host });
    // Make the instance globally accessible for legacy code that reads
    // window.__shopify_app (e.g. any third-party scripts or older patterns).
    // client.ts no longer uses this global — it calls getAppBridge() instead —
    // but we set it here so nothing external breaks.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (window as any).__shopify_app = _app;
  } catch (err) {
    // createApp can throw if the host is malformed — treat as non-embedded.
    console.warn("[appBridge] createApp failed:", err);
    _app = null;
  }

  return _app;
}

/**
 * Return the App Bridge app instance if it was successfully initialized,
 * or null if running outside Shopify (not embedded).
 *
 * Always call initAppBridge() first (once, at startup). This accessor is
 * safe to call from anywhere without worrying about initialization order
 * — it simply returns whatever state initAppBridge() left behind.
 */
export function getAppBridge(): ClientApplication | null {
  return _app;
}

/**
 * Reset the singleton. Exported for testing only — do NOT call in production code.
 */
export function _resetAppBridgeForTests(): void {
  _app = null;
  _cachedHost = null;
  try {
    if (typeof sessionStorage !== "undefined") {
      sessionStorage.removeItem(HOST_STORAGE_KEY);
    }
  } catch {
    // ignore
  }
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    delete (window as any).__shopify_app;
  } catch {
    // ignore
  }
}

/**
 * Force-set the cached host. Exported for testing only — allows tests to
 * simulate the module-load-time capture without manipulating window.location.
 */
export function _setCachedHostForTests(host: string | null): void {
  _cachedHost = host;
  if (host) {
    persistHost(host);
  } else {
    try {
      if (typeof sessionStorage !== "undefined") {
        sessionStorage.removeItem(HOST_STORAGE_KEY);
      }
    } catch {
      // ignore
    }
  }
}
