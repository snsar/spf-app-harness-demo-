/**
 * WarningTemplateLibraryScreen — component tests (TDD: written before implementation).
 *
 * Key safety rule: template.text is merchant input. Must be rendered as text,
 * never via dangerouslySetInnerHTML. React escapes by default — tests verify
 * the raw string appears as text content and no script element is injected.
 */

import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  fetchWarningTemplates: vi.fn(),
  createWarningTemplate: vi.fn(),
  updateWarningTemplate: vi.fn(),
  deleteWarningTemplate: vi.fn(),
}));

import { AppProvider } from "@shopify/polaris";
import enTranslations from "@shopify/polaris/locales/en.json";

import {
  fetchWarningTemplates,
  createWarningTemplate,
  updateWarningTemplate,
  deleteWarningTemplate,
} from "../api/client";
import { WarningTemplateLibraryScreen } from "./WarningTemplateLibraryScreen";
import type { WarningTemplate } from "../api/types";

const mockFetch = vi.mocked(fetchWarningTemplates);
const mockCreate = vi.mocked(createWarningTemplate);
const mockUpdate = vi.mocked(updateWarningTemplate);
const mockDelete = vi.mocked(deleteWarningTemplate);

const TEMPLATE_A: WarningTemplate = {
  id: 1,
  locale: "en",
  text: "Choking hazard. Small parts.",
  applies_to: { tags: ["toys"] },
  created_at: "2026-06-29T10:00:00Z",
  updated_at: "2026-06-29T10:00:00Z",
};

const TEMPLATE_B: WarningTemplate = {
  id: 2,
  locale: "de",
  text: "Erstickungsgefahr. Kleinteile.",
  applies_to: null,
  created_at: "2026-06-29T11:00:00Z",
  updated_at: "2026-06-29T11:00:00Z",
};

function renderWithPolaris(ui: React.ReactElement) {
  return render(<AppProvider i18n={enTranslations}>{ui}</AppProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  mockFetch.mockResolvedValue([TEMPLATE_A, TEMPLATE_B]);
  mockCreate.mockResolvedValue({ ...TEMPLATE_A, id: 99, text: "New warning" });
  mockUpdate.mockResolvedValue({ ...TEMPLATE_A, text: "Updated warning" });
  mockDelete.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("WarningTemplateLibraryScreen", () => {
  it("shows loading state initially", () => {
    mockFetch.mockReturnValue(new Promise(() => {}));
    renderWithPolaris(<WarningTemplateLibraryScreen />);
    expect(screen.getByText(/loading warning templates/i)).toBeInTheDocument();
  });

  it("renders template list after loading", async () => {
    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));
    expect(screen.getByText("Erstickungsgefahr. Kleinteile.")).toBeInTheDocument();
  });

  it("shows locale badge for each template", async () => {
    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));
    expect(screen.getByText("en")).toBeInTheDocument();
    expect(screen.getByText("de")).toBeInTheDocument();
  });

  it("renders template.text as text content (not HTML — XSS safety)", async () => {
    const xssTemplate: WarningTemplate = {
      ...TEMPLATE_A,
      id: 50,
      text: '<script>alert("xss")</script>',
    };
    mockFetch.mockResolvedValueOnce([xssTemplate]);

    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText('<script>alert("xss")</script>'));

    // Rendered as text — React escapes it. No executable script injected.
    expect(document.querySelector("script[src]")).toBeNull();
    expect(
      screen.getByText('<script>alert("xss")</script>'),
    ).toBeInTheDocument();
  });

  it("shows Add Warning Template button", async () => {
    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));
    expect(
      screen.getByRole("button", { name: /add warning template/i }),
    ).toBeInTheDocument();
  });

  it("opens create modal when Add Warning Template is clicked", async () => {
    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));

    fireEvent.click(screen.getByRole("button", { name: /add warning template/i }));

    await waitFor(() => screen.getByRole("dialog"));
    expect(screen.getByLabelText("Warning text")).toBeInTheDocument();
    expect(screen.getByLabelText("Locale")).toBeInTheDocument();
  });

  it("submits create form and adds template to list", async () => {
    const newTpl: WarningTemplate = {
      id: 99,
      locale: "fr",
      text: "Danger: pièces détachées.",
      applies_to: null,
      created_at: "2026-06-29T12:00:00Z",
      updated_at: "2026-06-29T12:00:00Z",
    };
    mockCreate.mockResolvedValueOnce(newTpl);

    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));

    fireEvent.click(screen.getByRole("button", { name: /add warning template/i }));
    await waitFor(() => screen.getByLabelText("Warning text"));

    fireEvent.change(screen.getByLabelText("Warning text"), {
      target: { value: "Danger: pièces détachées." },
    });
    fireEvent.change(screen.getByLabelText("Locale"), {
      target: { value: "fr" },
    });

    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => {
      expect(mockCreate).toHaveBeenCalledWith(
        expect.objectContaining({
          text: "Danger: pièces détachées.",
          locale: "fr",
        }),
      );
    });
    await waitFor(() => {
      expect(screen.getByText("Danger: pièces détachées.")).toBeInTheDocument();
    });
  });

  it("opens edit modal pre-filled with template data", async () => {
    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));

    const editBtn = screen.getByRole("button", {
      name: /edit warning template choking hazard/i,
    });
    fireEvent.click(editBtn);

    await waitFor(() => screen.getByLabelText("Warning text"));
    expect(
      (screen.getByLabelText("Warning text") as HTMLTextAreaElement).value,
    ).toBe("Choking hazard. Small parts.");
  });

  it("submits edit form and updates template in list", async () => {
    const updated: WarningTemplate = {
      ...TEMPLATE_A,
      text: "Updated choking hazard text.",
    };
    mockUpdate.mockResolvedValueOnce(updated);

    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));

    const editBtn = screen.getByRole("button", {
      name: /edit warning template choking hazard/i,
    });
    fireEvent.click(editBtn);

    await waitFor(() => screen.getByLabelText("Warning text"));
    fireEvent.change(screen.getByLabelText("Warning text"), {
      target: { value: "Updated choking hazard text." },
    });

    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => {
      expect(mockUpdate).toHaveBeenCalledWith(
        1,
        expect.objectContaining({ text: "Updated choking hazard text." }),
      );
    });
    await waitFor(() => {
      expect(screen.getByText("Updated choking hazard text.")).toBeInTheDocument();
    });
  });

  it("opens delete confirmation when Delete is clicked", async () => {
    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));

    const deleteBtn = screen.getByRole("button", {
      name: /delete warning template choking hazard/i,
    });
    fireEvent.click(deleteBtn);

    await waitFor(() => screen.getByText("Delete warning template?"));
    expect(
      screen.getByRole("button", { name: /confirm delete/i }),
    ).toBeInTheDocument();
  });

  it("deletes template and removes from list", async () => {
    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));

    fireEvent.click(
      screen.getByRole("button", { name: /delete warning template choking hazard/i }),
    );
    await waitFor(() => screen.getByRole("button", { name: /confirm delete/i }));
    fireEvent.click(screen.getByRole("button", { name: /confirm delete/i }));

    await waitFor(() => {
      expect(mockDelete).toHaveBeenCalledWith(1);
    });
    await waitFor(() => {
      expect(
        screen.queryByText("Choking hazard. Small parts."),
      ).not.toBeInTheDocument();
    });
  });

  it("shows 409 error on delete of referenced template without removing it", async () => {
    mockDelete.mockRejectedValueOnce(
      new Error("template is referenced by a rule"),
    );

    renderWithPolaris(<WarningTemplateLibraryScreen />);
    await waitFor(() => screen.getByText("Choking hazard. Small parts."));

    fireEvent.click(
      screen.getByRole("button", { name: /delete warning template choking hazard/i }),
    );
    await waitFor(() => screen.getByRole("button", { name: /confirm delete/i }));
    fireEvent.click(screen.getByRole("button", { name: /confirm delete/i }));

    await waitFor(() => {
      expect(screen.getByText(/referenced by a rule/i)).toBeInTheDocument();
    });
    // Template still in list
    expect(screen.getByText("Choking hazard. Small parts.")).toBeInTheDocument();
  });

  it("shows error state when API fetch fails", async () => {
    mockFetch.mockRejectedValueOnce(new Error("unauthorized"));
    renderWithPolaris(<WarningTemplateLibraryScreen />);

    await waitFor(() => {
      expect(screen.getByText(/unauthorized/i)).toBeInTheDocument();
    });
  });
});
