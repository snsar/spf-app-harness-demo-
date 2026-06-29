/**
 * App shell tests — verify nav renders; screen-level behavior is in screen tests.
 */
import { render, screen, waitFor } from "@testing-library/react";
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

describe("App", () => {
  it("renders the GPSR Compliance navigation section title", async () => {
    render(<App />);
    await waitFor(() => {
      expect(screen.getByText("GPSR Compliance")).toBeInTheDocument();
    });
  });

  it("renders nav items for Products, Entities, Warning Templates, and Rules", async () => {
    render(<App />);
    // "Products" appears in both the nav and the page title (use getAllByText)
    await waitFor(() => {
      expect(screen.getAllByText("Products").length).toBeGreaterThanOrEqual(1);
    });
    // Nav-specific: the nav link has "Entities" which doesn't appear elsewhere on /products
    expect(screen.getByText("Entities")).toBeInTheDocument();
    expect(screen.getByText("Warning Templates")).toBeInTheDocument();
    expect(screen.getByText("Rules")).toBeInTheDocument();
  });
});
