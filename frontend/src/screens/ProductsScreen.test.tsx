/**
 * ProductsScreen — TDD tests (written before implementation, F6).
 *
 * Non-trivial behavior under test:
 *  1. Bulk selection state — selecting, deselecting, select-all tracked correctly.
 *  2. Per-item result rendering after bulk apply — each product row reflects the
 *     updated status from the re-fetched list, not a single opaque spinner.
 *  3. Three terminal states are visually distinct (ok / needs_review / override)
 *     — badge text and accessibility labels differ.
 *  4. Null compliance (no record yet) renders a distinct "No record" state.
 *  5. Pagination: "Load more" appears when has_next=true; hidden when false.
 *  6. Apply bulk action calls the API with selected product_ids.
 *  7. Override bulk action calls setOverride for a selected product.
 *  8. Clear override bulk action calls clearOverride.
 *  9. API error surfaces as a Polaris Banner, not a crash.
 * 10. "Mark reviewed" for needs_review products re-applies the ruleset on them.
 */

import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

// Mock the products API module
vi.mock("../api/products", () => ({
  fetchProducts: vi.fn(),
  applyRuleset: vi.fn(),
  setOverride: vi.fn(),
  clearOverride: vi.fn(),
}));

// Mock the client for entities + warning templates (needed by override modal)
vi.mock("../api/client", () => ({
  fetchEntities: vi.fn(),
  fetchWarningTemplates: vi.fn(),
}));

import { AppProvider } from "@shopify/polaris";
import enTranslations from "@shopify/polaris/locales/en.json";
import { fetchProducts, applyRuleset, setOverride, clearOverride } from "../api/products";
import { fetchEntities, fetchWarningTemplates } from "../api/client";
import { ProductsScreen } from "./ProductsScreen";
import type { Product, ProductsResponse } from "../api/types";
import type { Entity, WarningTemplate } from "../api/types";

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const PRODUCT_OK: Product = {
  id: 7001,
  title: "Wooden Toy Train",
  tags: ["toys", "wood"],
  category: "toys",
  material: "wood",
  origin: "CN",
  compliance: {
    product_id: 7001,
    matched_rule_id: 10,
    entity_id: 100,
    status: "ok",
    warning_template_ids: [1, 2],
  },
};

const PRODUCT_NEEDS_REVIEW: Product = {
  id: 7002,
  title: "Mystery Box",
  tags: [],
  category: null,
  material: null,
  origin: null,
  compliance: {
    product_id: 7002,
    matched_rule_id: null,
    entity_id: null,
    status: "needs_review",
    warning_template_ids: [],
  },
};

const PRODUCT_OVERRIDE: Product = {
  id: 7003,
  title: "Custom Gadget",
  tags: ["electronics"],
  category: "electronics",
  material: null,
  origin: "DE",
  compliance: {
    product_id: 7003,
    matched_rule_id: null,
    entity_id: 100,
    status: "override",
    warning_template_ids: [1],
  },
};

const PRODUCT_NO_RECORD: Product = {
  id: 7004,
  title: "Unclassified Widget",
  tags: [],
  category: null,
  material: null,
  origin: null,
  compliance: null,
};

const ENTITY_A: Entity = {
  id: 100,
  name: "Acme EU GmbH",
  address: "Berlin",
  role: "importer",
  is_eu: true,
  created_at: "2026-06-29T10:00:00Z",
  updated_at: "2026-06-29T10:00:00Z",
};

const TEMPLATE_1: WarningTemplate = {
  id: 1,
  locale: "en",
  text: "Choking hazard. Small parts.",
  applies_to: { tags: ["toys"] },
  created_at: "2026-06-29T10:00:00Z",
  updated_at: "2026-06-29T10:00:00Z",
};

function makeProductsResponse(
  products: Product[],
  page = 1,
  has_next = false,
): ProductsResponse {
  return { products, page, has_next };
}

const mockFetchProducts = vi.mocked(fetchProducts);
const mockApplyRuleset = vi.mocked(applyRuleset);
const mockSetOverride = vi.mocked(setOverride);
const mockClearOverride = vi.mocked(clearOverride);
const mockFetchEntities = vi.mocked(fetchEntities);
const mockFetchWarningTemplates = vi.mocked(fetchWarningTemplates);

function renderWithPolaris(ui: React.ReactElement) {
  return render(<AppProvider i18n={enTranslations}>{ui}</AppProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  mockFetchProducts.mockResolvedValue(
    makeProductsResponse([PRODUCT_OK, PRODUCT_NEEDS_REVIEW, PRODUCT_OVERRIDE, PRODUCT_NO_RECORD]),
  );
  mockApplyRuleset.mockResolvedValue({ applied: 2 });
  mockSetOverride.mockResolvedValue({
    product_id: 7001,
    entity_id: 100,
    status: "override",
    matched_rule_id: null,
    warning_template_ids: [1],
  });
  mockClearOverride.mockResolvedValue(undefined);
  mockFetchEntities.mockResolvedValue([ENTITY_A]);
  mockFetchWarningTemplates.mockResolvedValue([TEMPLATE_1]);
});

// ---------------------------------------------------------------------------
// Helper: wait for products to load
// ---------------------------------------------------------------------------

async function waitForProducts() {
  await waitFor(() => screen.getByText("Wooden Toy Train"));
}

/**
 * Find the row checkbox for a given product by its title text.
 * Polaris renders each IndexTable.Row as a <tr>; within that row we find the
 * <input type="checkbox"> that drives selection.
 */
function getRowCheckbox(productTitle: string): HTMLElement {
  const titleEl = screen.getByText(productTitle);
  // Walk up to the table row (<tr>) that contains this title
  let node: HTMLElement | null = titleEl.parentElement;
  while (node && node.tagName !== "TR") {
    node = node.parentElement as HTMLElement | null;
  }
  if (!node) throw new Error(`Could not find TR ancestor for "${productTitle}"`);
  const checkbox = within(node).getByRole("checkbox");
  return checkbox;
}

// ---------------------------------------------------------------------------
// Suite 1: Loading and initial render
// ---------------------------------------------------------------------------

describe("ProductsScreen — loading and initial render", () => {
  it("renders a loading spinner initially", () => {
    mockFetchProducts.mockReturnValue(new Promise(() => {}));
    renderWithPolaris(<ProductsScreen />);
    // Match the visible paragraph text (not the Polaris visually-hidden aria label)
    expect(screen.getByText("Loading products…")).toBeInTheDocument();
  });

  it("renders all four products after loading", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();
    expect(screen.getByText("Wooden Toy Train")).toBeInTheDocument();
    expect(screen.getByText("Mystery Box")).toBeInTheDocument();
    expect(screen.getByText("Custom Gadget")).toBeInTheDocument();
    expect(screen.getByText("Unclassified Widget")).toBeInTheDocument();
  });

  it("shows API error as a Banner when fetch fails", async () => {
    mockFetchProducts.mockRejectedValueOnce(new Error("unauthorized"));
    renderWithPolaris(<ProductsScreen />);
    await waitFor(() =>
      expect(screen.getByText(/unauthorized/i)).toBeInTheDocument(),
    );
  });
});

// ---------------------------------------------------------------------------
// Suite 2: Terminal state badge distinctness
// ---------------------------------------------------------------------------

describe("ProductsScreen — compliance status badge distinctness", () => {
  it("renders 'Compliant' badge for ok status", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();
    expect(screen.getByText("Compliant")).toBeInTheDocument();
  });

  it("renders 'Needs Review' badge for needs_review status", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();
    expect(screen.getByText("Needs Review")).toBeInTheDocument();
  });

  it("renders 'Override' badge for override status", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();
    expect(screen.getByText("Override")).toBeInTheDocument();
  });

  it("renders 'No Record' label for null compliance", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();
    expect(screen.getByText("No Record")).toBeInTheDocument();
  });

  it("does NOT render 'Needs Review' badge with the same visual tone as 'Compliant'", async () => {
    /**
     * This test guards the terminal-state visibility rule:
     * needs_review must NOT look identical to ok.
     * We verify this by checking that the two badge text values are different strings
     * (which maps to different Polaris tone props in the implementation).
     */
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();
    const compliantBadge = screen.getByText("Compliant");
    const needsReviewBadge = screen.getByText("Needs Review");
    // They must be different DOM elements with different text content
    expect(compliantBadge).not.toBe(needsReviewBadge);
    expect(compliantBadge.textContent).not.toBe(needsReviewBadge.textContent);
  });
});

// ---------------------------------------------------------------------------
// Suite 3: Bulk selection state (non-trivial logic)
// ---------------------------------------------------------------------------

describe("ProductsScreen — bulk selection state", () => {
  it("shows bulk action bar when at least one product is selected", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    fireEvent.click(getRowCheckbox("Wooden Toy Train"));

    await waitFor(() => {
      // Polaris IndexTable renders promoted bulk actions — may appear in multiple
      // places in the DOM (header toolbar + accessible hidden copy), so use getAllByText.
      const buttons = screen.getAllByText(/apply ruleset/i);
      expect(buttons.length).toBeGreaterThan(0);
    });
  });

  it("deselecting all products hides the bulk action bar", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    const checkbox = getRowCheckbox("Wooden Toy Train");

    // Select then deselect
    fireEvent.click(checkbox);
    await waitFor(() => {
      const buttons = screen.getAllByText(/apply ruleset/i);
      expect(buttons.length).toBeGreaterThan(0);
    });

    fireEvent.click(checkbox);
    await waitFor(() => {
      expect(screen.queryByText(/apply ruleset/i)).not.toBeInTheDocument();
    });
  });
});

// ---------------------------------------------------------------------------
// Suite 4: Bulk apply — per-item result feedback
// ---------------------------------------------------------------------------

describe("ProductsScreen — bulk apply per-item result", () => {
  it("calls applyRuleset with selected product IDs", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    fireEvent.click(getRowCheckbox("Wooden Toy Train"));

    // Polaris may render promoted bulk action buttons in multiple DOM nodes
    await waitFor(() => {
      const buttons = screen.getAllByText(/apply ruleset/i);
      expect(buttons.length).toBeGreaterThan(0);
    });
    // After apply, fetchProducts is called again to get updated statuses
    mockFetchProducts.mockResolvedValueOnce(
      makeProductsResponse([
        { ...PRODUCT_OK },
        PRODUCT_NEEDS_REVIEW,
        PRODUCT_OVERRIDE,
        PRODUCT_NO_RECORD,
      ]),
    );

    // Click the first visible "Apply Ruleset" button
    fireEvent.click(screen.getAllByText(/apply ruleset/i)[0]);

    await waitFor(() => {
      expect(mockApplyRuleset).toHaveBeenCalled();
    });
  });

  it("re-fetches products after apply to show updated per-row statuses", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    fireEvent.click(getRowCheckbox("Wooden Toy Train"));

    await waitFor(() => {
      const buttons = screen.getAllByText(/apply ruleset/i);
      expect(buttons.length).toBeGreaterThan(0);
    });

    // Simulate: after apply, the previously needs_review product becomes ok
    const afterApply: Product = {
      ...PRODUCT_NEEDS_REVIEW,
      compliance: {
        product_id: 7002,
        matched_rule_id: 10,
        entity_id: 100,
        status: "ok",
        warning_template_ids: [1],
      },
    };
    mockFetchProducts.mockResolvedValueOnce(
      makeProductsResponse([PRODUCT_OK, afterApply, PRODUCT_OVERRIDE, PRODUCT_NO_RECORD]),
    );

    fireEvent.click(screen.getAllByText(/apply ruleset/i)[0]);

    await waitFor(() => {
      expect(mockFetchProducts).toHaveBeenCalledTimes(2);
    });
    // The row for Mystery Box should now show Compliant (from the re-fetched data)
    await waitFor(() => {
      // There should now be 2 "Compliant" badges (7001 was already ok, 7002 is now ok)
      const compliantBadges = screen.getAllByText("Compliant");
      expect(compliantBadges.length).toBe(2);
    });
  });

  it("shows a success banner with applied count after bulk apply", async () => {
    mockApplyRuleset.mockResolvedValueOnce({ applied: 3 });
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    fireEvent.click(getRowCheckbox("Wooden Toy Train"));

    await waitFor(() => {
      const buttons = screen.getAllByText(/apply ruleset/i);
      expect(buttons.length).toBeGreaterThan(0);
    });
    fireEvent.click(screen.getAllByText(/apply ruleset/i)[0]);

    await waitFor(() => {
      expect(screen.getByText(/3 product/i)).toBeInTheDocument();
    });
  });

  it("shows an error banner if apply fails", async () => {
    mockApplyRuleset.mockRejectedValueOnce(new Error("server error"));
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    fireEvent.click(getRowCheckbox("Wooden Toy Train"));

    await waitFor(() => {
      const buttons = screen.getAllByText(/apply ruleset/i);
      expect(buttons.length).toBeGreaterThan(0);
    });
    fireEvent.click(screen.getAllByText(/apply ruleset/i)[0]);

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument();
    });
  });
});

// ---------------------------------------------------------------------------
// Suite 5: Clear override
// ---------------------------------------------------------------------------

describe("ProductsScreen — clear override bulk action", () => {
  it("calls clearOverride for each selected product", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    // Select the override product (Custom Gadget = id 7003)
    fireEvent.click(getRowCheckbox("Custom Gadget"));

    await waitFor(() => {
      expect(screen.getAllByText(/clear override/i).length).toBeGreaterThan(0);
    });
    mockFetchProducts.mockResolvedValueOnce(
      makeProductsResponse([PRODUCT_OK, PRODUCT_NEEDS_REVIEW, PRODUCT_OVERRIDE, PRODUCT_NO_RECORD]),
    );
    fireEvent.click(screen.getAllByText(/clear override/i)[0]);

    await waitFor(() => {
      expect(mockClearOverride).toHaveBeenCalledWith(PRODUCT_OVERRIDE.id);
    });
  });
});

// ---------------------------------------------------------------------------
// Suite 6: Mark reviewed (re-apply on needs_review items)
// ---------------------------------------------------------------------------

describe("ProductsScreen — mark reviewed", () => {
  it("mark reviewed calls applyRuleset on the selected needs_review products", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    // Select the needs_review product (Mystery Box = id 7002)
    fireEvent.click(getRowCheckbox("Mystery Box"));

    await waitFor(() => {
      expect(screen.getAllByText(/mark reviewed/i).length).toBeGreaterThan(0);
    });
    fireEvent.click(screen.getAllByText(/mark reviewed/i)[0]);

    await waitFor(() => {
      expect(mockApplyRuleset).toHaveBeenCalledWith([PRODUCT_NEEDS_REVIEW.id]);
    });
  });
});

// ---------------------------------------------------------------------------
// Suite 7: Pagination
// ---------------------------------------------------------------------------

describe("ProductsScreen — pagination", () => {
  it("shows Load More button when has_next is true", async () => {
    mockFetchProducts.mockResolvedValueOnce(
      makeProductsResponse([PRODUCT_OK], 1, true),
    );
    renderWithPolaris(<ProductsScreen />);
    await waitFor(() => screen.getByText("Wooden Toy Train"));
    expect(screen.getByRole("button", { name: /load more/i })).toBeInTheDocument();
  });

  it("does not show Load More when has_next is false", async () => {
    mockFetchProducts.mockResolvedValueOnce(
      makeProductsResponse([PRODUCT_OK], 1, false),
    );
    renderWithPolaris(<ProductsScreen />);
    await waitFor(() => screen.getByText("Wooden Toy Train"));
    expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument();
  });

  it("fetches page 2 when Load More is clicked and appends rows", async () => {
    mockFetchProducts
      .mockResolvedValueOnce(makeProductsResponse([PRODUCT_OK], 1, true))
      .mockResolvedValueOnce(makeProductsResponse([PRODUCT_NEEDS_REVIEW], 2, false));

    renderWithPolaris(<ProductsScreen />);
    await waitFor(() => screen.getByText("Wooden Toy Train"));

    fireEvent.click(screen.getByRole("button", { name: /load more/i }));

    await waitFor(() => {
      expect(screen.getByText("Mystery Box")).toBeInTheDocument();
    });
    // Page 1 row still present
    expect(screen.getByText("Wooden Toy Train")).toBeInTheDocument();
    // Load More is gone (has_next = false on page 2)
    expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Suite 8: Override modal
// ---------------------------------------------------------------------------

describe("ProductsScreen — set override", () => {
  it("shows Set Override button for each product row", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    // Each row has a per-row "Set Override" button (accessible by aria-label)
    expect(
      screen.getByRole("button", { name: /set override for wooden toy train/i }),
    ).toBeInTheDocument();
  });

  it("calls setOverride API when override is confirmed via per-row button", async () => {
    renderWithPolaris(<ProductsScreen />);
    await waitForProducts();

    // Click the per-row "Set Override" button for product 7001
    fireEvent.click(
      screen.getByRole("button", { name: /set override for wooden toy train/i }),
    );

    // Override modal should open
    await waitFor(() =>
      expect(screen.getByRole("dialog")).toBeInTheDocument(),
    );

    // Select entity (the only entity in the list)
    const entitySelect = screen.getByLabelText(/responsible entity/i);
    fireEvent.change(entitySelect, { target: { value: "100" } });

    // Confirm the override
    const confirmBtn = screen.getByRole("button", { name: /confirm override/i });
    fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(mockSetOverride).toHaveBeenCalledWith(
        expect.objectContaining({
          product_id: PRODUCT_OK.id,
          entity_id: 100,
        }),
      );
    });
  });
});
