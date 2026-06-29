/**
 * EntityLibraryScreen — component tests (TDD: written before implementation).
 *
 * Tests verify:
 *  - Renders entity list from API
 *  - Create flow: form submits and list updates
 *  - Edit flow: form pre-fills and saves
 *  - Delete flow: 204 removes item from list
 *  - 409 on delete shows a clear error, does not crash/remove the entity
 *  - Loading state renders
 *  - Uses surrogate `id` as React key / identifier
 */

import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// Mock the API client
vi.mock("../api/client", () => ({
  fetchEntities: vi.fn(),
  createEntity: vi.fn(),
  updateEntity: vi.fn(),
  deleteEntity: vi.fn(),
}));

// Wrap in Polaris AppProvider for tests
import { AppProvider } from "@shopify/polaris";
import enTranslations from "@shopify/polaris/locales/en.json";

import {
  fetchEntities,
  createEntity,
  updateEntity,
  deleteEntity,
} from "../api/client";
import { EntityLibraryScreen } from "./EntityLibraryScreen";
import type { Entity } from "../api/types";

const mockFetchEntities = vi.mocked(fetchEntities);
const mockCreateEntity = vi.mocked(createEntity);
const mockUpdateEntity = vi.mocked(updateEntity);
const mockDeleteEntity = vi.mocked(deleteEntity);

const ENTITY_A: Entity = {
  id: 100,
  name: "Acme EU GmbH",
  address: "Berlin",
  role: "importer",
  is_eu: true,
  created_at: "2026-06-29T10:00:00Z",
  updated_at: "2026-06-29T10:00:00Z",
};

const ENTITY_B: Entity = {
  id: 101,
  name: "Global Distributor Ltd",
  address: "Dublin",
  role: "distributor",
  is_eu: true,
  created_at: "2026-06-29T11:00:00Z",
  updated_at: "2026-06-29T11:00:00Z",
};

function renderWithPolaris(ui: React.ReactElement) {
  return render(
    <AppProvider i18n={enTranslations}>{ui}</AppProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockFetchEntities.mockResolvedValue([ENTITY_A, ENTITY_B]);
  mockCreateEntity.mockResolvedValue({ ...ENTITY_A, id: 200, name: "New Entity" });
  mockUpdateEntity.mockResolvedValue({ ...ENTITY_A, name: "Updated Name" });
  mockDeleteEntity.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("EntityLibraryScreen", () => {
  it("renders loading state initially", () => {
    mockFetchEntities.mockReturnValue(new Promise(() => {}));
    renderWithPolaris(<EntityLibraryScreen />);
    expect(screen.getByText(/loading entities/i)).toBeInTheDocument();
  });

  it("renders entity list after loading", async () => {
    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));
    expect(screen.getByText("Global Distributor Ltd")).toBeInTheDocument();
  });

  it("displays entity address", async () => {
    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));
    expect(screen.getByText("Berlin")).toBeInTheDocument();
  });

  it("displays entity role as a badge", async () => {
    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));
    expect(screen.getByText("importer")).toBeInTheDocument();
  });

  it("shows EU badge for EU entities", async () => {
    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));
    const euBadges = screen.getAllByText("EU");
    expect(euBadges.length).toBeGreaterThanOrEqual(1);
  });

  it("shows Add Entity button", async () => {
    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));
    expect(screen.getByRole("button", { name: /add entity/i })).toBeInTheDocument();
  });

  it("opens create modal when Add Entity is clicked", async () => {
    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));

    fireEvent.click(screen.getByRole("button", { name: /add entity/i }));

    // Wait for modal title (Polaris renders it in a dialog heading)
    await waitFor(() => screen.getByRole("dialog"));
    // Modal form fields present (Polaris TextField labels)
    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(screen.getByLabelText("Address")).toBeInTheDocument();
    expect(screen.getByLabelText("Role")).toBeInTheDocument();
  });

  it("submits create form and adds entity to list", async () => {
    const newEntity: Entity = {
      id: 200,
      name: "New Entity",
      address: "Paris",
      role: "manufacturer",
      is_eu: true,
      created_at: "2026-06-29T12:00:00Z",
      updated_at: "2026-06-29T12:00:00Z",
    };
    mockCreateEntity.mockResolvedValueOnce(newEntity);

    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));

    fireEvent.click(screen.getByRole("button", { name: /add entity/i }));
    await waitFor(() => screen.getByLabelText("Name"));

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "New Entity" },
    });
    fireEvent.change(screen.getByLabelText("Address"), {
      target: { value: "Paris" },
    });
    fireEvent.change(screen.getByLabelText("Role"), {
      target: { value: "manufacturer" },
    });

    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => {
      expect(mockCreateEntity).toHaveBeenCalledWith(
        expect.objectContaining({ name: "New Entity", address: "Paris" }),
      );
    });
    await waitFor(() => {
      expect(screen.getByText("New Entity")).toBeInTheDocument();
    });
  });

  it("opens edit modal pre-filled with entity data", async () => {
    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));

    const editButton = screen.getByRole("button", {
      name: /edit entity acme eu gmbh/i,
    });
    fireEvent.click(editButton);

    await waitFor(() => screen.getByLabelText("Name"));
    expect(
      (screen.getByLabelText("Name") as HTMLInputElement).value,
    ).toBe("Acme EU GmbH");
    expect(
      (screen.getByLabelText("Address") as HTMLInputElement).value,
    ).toBe("Berlin");
  });

  it("submits edit form and updates entity in list", async () => {
    const updated: Entity = { ...ENTITY_A, name: "Acme EU GmbH Renamed" };
    mockUpdateEntity.mockResolvedValueOnce(updated);

    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));

    const editButton = screen.getByRole("button", {
      name: /edit entity acme eu gmbh/i,
    });
    fireEvent.click(editButton);

    await waitFor(() => screen.getByLabelText("Name"));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Acme EU GmbH Renamed" },
    });

    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => {
      expect(mockUpdateEntity).toHaveBeenCalledWith(
        100,
        expect.objectContaining({ name: "Acme EU GmbH Renamed" }),
      );
    });
    await waitFor(() => {
      expect(screen.getByText("Acme EU GmbH Renamed")).toBeInTheDocument();
    });
  });

  it("opens delete confirmation when Delete is clicked", async () => {
    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));

    const deleteButton = screen.getByRole("button", {
      name: /delete entity acme eu gmbh/i,
    });
    fireEvent.click(deleteButton);

    await waitFor(() => screen.getByText("Delete entity?"));
    expect(screen.getByRole("button", { name: /confirm delete/i })).toBeInTheDocument();
  });

  it("deletes entity on confirm and removes from list", async () => {
    mockDeleteEntity.mockResolvedValueOnce(undefined);

    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));

    fireEvent.click(
      screen.getByRole("button", { name: /delete entity acme eu gmbh/i }),
    );
    await waitFor(() => screen.getByRole("button", { name: /confirm delete/i }));
    fireEvent.click(screen.getByRole("button", { name: /confirm delete/i }));

    await waitFor(() => {
      expect(mockDeleteEntity).toHaveBeenCalledWith(100);
    });
    await waitFor(() => {
      expect(screen.queryByText("Acme EU GmbH")).not.toBeInTheDocument();
    });
  });

  it("shows 409 error on delete without removing entity", async () => {
    mockDeleteEntity.mockRejectedValueOnce(
      new Error("entity is referenced by a rule"),
    );

    renderWithPolaris(<EntityLibraryScreen />);
    await waitFor(() => screen.getByText("Acme EU GmbH"));

    fireEvent.click(
      screen.getByRole("button", { name: /delete entity acme eu gmbh/i }),
    );
    await waitFor(() => screen.getByRole("button", { name: /confirm delete/i }));
    fireEvent.click(screen.getByRole("button", { name: /confirm delete/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/referenced by a rule/i),
      ).toBeInTheDocument();
    });
    // Entity still in list — at least one element contains the name
    const nameElements = screen.getAllByText("Acme EU GmbH");
    expect(nameElements.length).toBeGreaterThan(0);
  });

  it("shows error state when API fetch fails", async () => {
    mockFetchEntities.mockRejectedValueOnce(new Error("unauthorized"));
    renderWithPolaris(<EntityLibraryScreen />);

    await waitFor(() => {
      expect(screen.getByText(/unauthorized/i)).toBeInTheDocument();
    });
  });
});
