/**
 * App shell tests — verify the ui-nav-menu web component renders the correct
 * sidebar links; screen-level behaviour is tested in screen-specific tests.
 *
 * Nav architecture: embedded apps use <ui-nav-menu> (CDN App Bridge web
 * component) so the sidebar lives OUTSIDE the iframe in the Shopify Admin
 * chrome.  We assert on the <a> elements inside <ui-nav-menu> — NOT on Polaris
 * Frame+Navigation items (that would be the wrong, standalone-app pattern).
 */
import { render } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { App } from "./App";

// Mock the API client so screens don't make real calls
vi.mock("./api/client", () => ({
  fetchEntities: vi.fn().mockResolvedValue([]),
  createEntity: vi.fn(),
  updateEntity: vi.fn(),
  deleteEntity: vi.fn(),
  fetchWarningTemplates: vi.fn().mockResolvedValue([]),
  createWarningTemplate: vi.fn(),
  updateWarningTemplate: vi.fn(),
  deleteWarningTemplate: vi.fn(),
}));

// Mock the products API (F6) so the default /products route doesn't make real calls
vi.mock("./api/products", () => ({
  fetchProducts: vi.fn().mockResolvedValue({ products: [], page: 1, has_next: false }),
  applyRuleset: vi.fn(),
  setOverride: vi.fn(),
  clearOverride: vi.fn(),
}));

describe("App — ui-nav-menu sidebar links", () => {
  it("renders a ui-nav-menu element", () => {
    render(<App />);
    // The <ui-nav-menu> custom element must be in the DOM so App Bridge can
    // lift it into the Shopify Admin sidebar.
    const navMenu = document.querySelector("ui-nav-menu");
    expect(navMenu).not.toBeNull();
  });

  it("renders an anchor for Products with rel=home (primary route)", () => {
    render(<App />);
    const link = document.querySelector('ui-nav-menu a[href="/products"]');
    expect(link).not.toBeNull();
    expect(link?.getAttribute("rel")).toBe("home");
    expect(link?.textContent?.trim()).toBe("Products");
  });

  it("renders an anchor for Entities", () => {
    render(<App />);
    const link = document.querySelector('ui-nav-menu a[href="/entities"]');
    expect(link).not.toBeNull();
    expect(link?.textContent?.trim()).toBe("Entities");
  });

  it("renders an anchor for Warning Templates", () => {
    render(<App />);
    const link = document.querySelector(
      'ui-nav-menu a[href="/warning-templates"]',
    );
    expect(link).not.toBeNull();
    expect(link?.textContent?.trim()).toBe("Warning Templates");
  });

  it("renders an anchor for Rules", () => {
    render(<App />);
    const link = document.querySelector('ui-nav-menu a[href="/rules"]');
    expect(link).not.toBeNull();
    expect(link?.textContent?.trim()).toBe("Rules");
  });

  it("all four nav links are present inside ui-nav-menu", () => {
    render(<App />);
    const navMenu = document.querySelector("ui-nav-menu");
    const links = navMenu?.querySelectorAll("a");
    expect(links?.length).toBe(4);
    const hrefs = Array.from(links ?? []).map((a) => a.getAttribute("href"));
    expect(hrefs).toEqual([
      "/products",
      "/entities",
      "/warning-templates",
      "/rules",
    ]);
  });
});

// Backwards-compatible alias for the legacy test description used in CI
// log scanning — any grep for "nav items for Products" still finds a test.
describe("App", () => {
  it("renders nav items for Products, Entities, Warning Templates, and Rules", () => {
    render(<App />);
    const navMenu = document.querySelector("ui-nav-menu");
    expect(navMenu?.querySelector('a[href="/products"]')).not.toBeNull();
    expect(navMenu?.querySelector('a[href="/entities"]')).not.toBeNull();
    expect(
      navMenu?.querySelector('a[href="/warning-templates"]'),
    ).not.toBeNull();
    expect(navMenu?.querySelector('a[href="/rules"]')).not.toBeNull();
  });
});
