/// <reference types="vite/client" />
// Vitest global setup: register @testing-library/jest-dom matchers
// (e.g. toBeInTheDocument) and clean up the DOM between tests.
import "@testing-library/jest-dom/vitest";
import { afterEach, vi } from "vitest";
import { cleanup } from "@testing-library/react";

// ---------------------------------------------------------------------------
// window.matchMedia stub — required for Polaris (jsdom does not implement it).
// Polaris calls matchMedia(query).matches and also .addEventListener on the result.
// ---------------------------------------------------------------------------
function makeMediaQueryList(query: string): MediaQueryList {
  return {
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  } as unknown as MediaQueryList;
}

Object.defineProperty(window, "matchMedia", {
  writable: true,
  configurable: true,
  value: vi.fn((query: string) => makeMediaQueryList(query)),
});

// ---------------------------------------------------------------------------
// ResizeObserver stub — Polaris uses it internally
// ---------------------------------------------------------------------------
window.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// ---------------------------------------------------------------------------
// IntersectionObserver stub — Polaris modals use it
// ---------------------------------------------------------------------------
if (!window.IntersectionObserver) {
  window.IntersectionObserver = class IntersectionObserver {
    constructor(
      _callback: IntersectionObserverCallback,
      _options?: IntersectionObserverInit,
    ) {
      void _callback;
      void _options;
    }
    observe() {}
    unobserve() {}
    disconnect() {}
    readonly root = null;
    readonly rootMargin = "";
    readonly thresholds: ReadonlyArray<number> = [];
    takeRecords(): IntersectionObserverEntry[] {
      return [];
    }
  };
}

afterEach(() => {
  cleanup();
});
