/**
 * ProductsScreen — Bulk product editor + compliance status (F6).
 *
 * Features:
 *  - Paginated product table (GET /api/products?page=&limit=50)
 *  - Three terminal states rendered as visually distinct Polaris Badges:
 *      ok           → Badge tone="success"  label="Compliant"
 *      needs_review → Badge tone="attention" label="Needs Review"  (NEVER same as ok)
 *      override     → Badge tone="info"      label="Override"
 *      null         → Text label="No Record"
 *  - Polaris IndexTable with multi-select bulk actions:
 *      Apply Ruleset  → POST /api/compliance/apply with selected ids
 *      Mark Reviewed  → POST /api/compliance/apply on selected ids (re-infer;
 *                       there is NO dedicated backend endpoint — see comment below)
 *      Set Override   → opens modal → POST /api/compliance/override
 *      Clear Override → DELETE /api/compliance/override/:id for each selected
 *  - Per-item result: after apply, fetchProducts is called to refresh the table
 *    rows individually — each row's badge updates to its actual new state.
 *    Not a single opaque spinner.
 *  - Pagination: Load More button appends page N+1 to the existing list.
 *
 * MARK-REVIEWED note (user story gap, documented):
 *   The user story mentions "mark reviewed" but the F3b backend has NO dedicated
 *   endpoint that flips needs_review→ok without re-inference. The only paths to
 *   resolve needs_review are:
 *     (a) apply ruleset — if a rule now matches, status becomes ok
 *     (b) set override  — status becomes override
 *   "Mark Reviewed" is therefore implemented as option (a): apply ruleset on the
 *   selected products. This is honest to the backend contract and communicates to
 *   the merchant that "review = re-run inference on these items". A phantom
 *   endpoint is NOT called. Gap documented here.
 *
 * All UI uses Shopify Polaris. Requires <AppProvider> in the tree (App.tsx owns it).
 * No dangerouslySetInnerHTML; merchant/product data rendered as text only.
 */

import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import {
  Page,
  Card,
  IndexTable,
  Text,
  Badge,
  Banner,
  Button,
  Spinner,
  BlockStack,
  InlineStack,
  EmptyState,
  Modal,
  FormLayout,
  Select,
  Box,
  Checkbox,
} from "@shopify/polaris";
import { useIndexResourceState } from "@shopify/polaris";
import {
  fetchProducts,
  applyRuleset,
  setOverride,
  clearOverride,
} from "../api/products";
import { fetchEntities, fetchWarningTemplates } from "../api/client";
import type { Product, ComplianceStatus } from "../api/types";
import type { Entity, WarningTemplate } from "../api/types";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type ActionState =
  | { kind: "idle" }
  | { kind: "busy"; label: string }
  | { kind: "success"; message: string }
  | { kind: "error"; message: string };

// ---------------------------------------------------------------------------
// Compliance status badge
// ---------------------------------------------------------------------------

/**
 * Renders the compliance status as a visually unmistakable Polaris Badge.
 *
 * Tone mapping (enforces the terminal-state visibility rule):
 *   ok           → success  (green)
 *   needs_review → attention (yellow/orange) — NEVER same as ok
 *   override     → info     (blue)
 *   null         → plain text "No Record"
 */
function ComplianceBadge({
  status,
}: {
  status: ComplianceStatus | null;
}) {
  if (status === null) {
    return (
      <Text as="span" tone="subdued">
        No Record
      </Text>
    );
  }

  switch (status) {
    case "ok":
      return <Badge tone="success">Compliant</Badge>;
    case "needs_review":
      return <Badge tone="attention">Needs Review</Badge>;
    case "override":
      return <Badge tone="info">Override</Badge>;
  }
}

// ---------------------------------------------------------------------------
// Override modal
// ---------------------------------------------------------------------------

interface OverrideModalProps {
  open: boolean;
  productId: number | null;
  productTitle: string;
  entities: Entity[];
  templates: WarningTemplate[];
  onConfirm: (entityId: number, templateIds: number[]) => Promise<void>;
  onClose: () => void;
}

function OverrideModal({
  open,
  productId,
  productTitle,
  entities,
  templates,
  onConfirm,
  onClose,
}: OverrideModalProps) {
  const [entityId, setEntityId] = useState<string>("");
  const [selectedTemplateIds, setSelectedTemplateIds] = useState<number[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset when modal opens
  useEffect(() => {
    if (open) {
      setEntityId("");
      setSelectedTemplateIds([]);
      setSaving(false);
      setError(null);
    }
  }, [open]);

  const entityOptions = [
    { label: "Select a responsible entity", value: "" },
    ...entities.map((e) => ({
      label: `${e.name}${e.is_eu ? " (EU)" : ""}`,
      value: String(e.id),
    })),
  ];

  const handleTemplateToggle = (id: number) => {
    setSelectedTemplateIds((prev) =>
      prev.includes(id) ? prev.filter((t) => t !== id) : [...prev, id],
    );
  };

  const handleConfirm = async () => {
    if (!entityId) {
      setError("Please select a responsible entity.");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await onConfirm(Number(entityId), selectedTemplateIds);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Override failed");
      setSaving(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`Set Override — ${productTitle}`}
      primaryAction={{
        content: saving ? "Saving…" : "Confirm Override",
        onAction: () => void handleConfirm(),
        disabled: saving || !entityId,
        loading: saving,
        accessibilityLabel: "Confirm Override",
      }}
      secondaryActions={[
        { content: "Cancel", onAction: onClose, disabled: saving },
      ]}
    >
      <Modal.Section>
        {error && (
          <Box paddingBlockEnd="400">
            <Banner tone="critical" onDismiss={() => setError(null)}>
              {error}
            </Banner>
          </Box>
        )}
        {productId === null ? null : (
          <FormLayout>
            <Select
              label="Responsible Entity"
              options={entityOptions}
              value={entityId}
              onChange={setEntityId}
              disabled={saving}
              helpText="The entity that takes responsibility for this product under GPSR."
            />
            {templates.length > 0 && (
              <BlockStack gap="200">
                <Text as="p" variant="bodyMd" fontWeight="semibold">
                  Warning Templates (optional)
                </Text>
                {templates.map((t) => (
                  <Checkbox
                    key={t.id}
                    label={`[${t.locale}] ${t.text}`}
                    checked={selectedTemplateIds.includes(t.id)}
                    onChange={() => handleTemplateToggle(t.id)}
                    disabled={saving}
                  />
                ))}
              </BlockStack>
            )}
          </FormLayout>
        )}
      </Modal.Section>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Main screen
// ---------------------------------------------------------------------------

export function ProductsScreen() {
  const [products, setProducts] = useState<Product[]>([]);
  const [page, setPage] = useState(1);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [actionState, setActionState] = useState<ActionState>({ kind: "idle" });
  const [entities, setEntities] = useState<Entity[]>([]);
  const [templates, setTemplates] = useState<WarningTemplate[]>([]);

  // Override modal state
  const [overrideModal, setOverrideModal] = useState<{
    open: boolean;
    productId: number | null;
    productTitle: string;
  }>({ open: false, productId: null, productTitle: "" });

  // ---------------------------------------------------------------------------
  // Fetch library data (entities + templates for override modal)
  // ---------------------------------------------------------------------------

  const libraryLoaded = useRef(false);

  const loadLibrary = useCallback(async () => {
    if (libraryLoaded.current) return;
    libraryLoaded.current = true;
    try {
      const [entitiesData, templatesData] = await Promise.all([
        fetchEntities(),
        fetchWarningTemplates(),
      ]);
      setEntities(entitiesData);
      setTemplates(templatesData);
    } catch {
      // Non-fatal: override modal will show an empty entity list
    }
  }, []);

  // ---------------------------------------------------------------------------
  // Fetch products
  // ---------------------------------------------------------------------------

  const loadProducts = useCallback(async (pageNum = 1) => {
    if (pageNum === 1) {
      setLoading(true);
      setFetchError(null);
    } else {
      setLoadingMore(true);
    }

    try {
      const data = await fetchProducts({ page: pageNum, limit: 50 });
      if (pageNum === 1) {
        setProducts(data.products);
      } else {
        setProducts((prev) => [...prev, ...data.products]);
      }
      setPage(data.page);
      setHasNext(data.has_next);
    } catch (err) {
      setFetchError(
        err instanceof Error ? err.message : "Failed to load products",
      );
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, []);

  useEffect(() => {
    void loadProducts(1);
    void loadLibrary();
  }, [loadProducts, loadLibrary]);

  // ---------------------------------------------------------------------------
  // IndexTable resource state (drives bulk selection)
  // Memoised so identity is stable across re-renders; prevents spurious resets
  // of the selection state inside useIndexResourceState.
  // ---------------------------------------------------------------------------

  const resourceItems = useMemo(
    () => products.map((p) => ({ id: String(p.id) })),
    [products],
  );
  const { selectedResources, allResourcesSelected, handleSelectionChange, clearSelection } =
    useIndexResourceState(resourceItems);

  const selectedProductIds = selectedResources.map(Number);

  // ---------------------------------------------------------------------------
  // Refresh products (called after bulk ops to show per-item results)
  // ---------------------------------------------------------------------------

  const refreshProducts = useCallback(async () => {
    try {
      const data = await fetchProducts({ page: 1, limit: 50 });
      setProducts(data.products);
      setPage(data.page);
      setHasNext(data.has_next);
    } catch {
      // Refresh failure is non-fatal — existing data stays; user sees stale rows
    }
  }, []);

  // ---------------------------------------------------------------------------
  // Bulk action: Apply Ruleset
  // ---------------------------------------------------------------------------

  const handleApplyRuleset = useCallback(
    async (ids: number[]) => {
      setActionState({ kind: "busy", label: "Applying ruleset…" });
      try {
        const result = await applyRuleset(ids);
        await refreshProducts();
        clearSelection();
        setActionState({
          kind: "success",
          message: `Ruleset applied to ${result.applied} product${result.applied === 1 ? "" : "s"}. Rows updated with per-product results.`,
        });
      } catch (err) {
        setActionState({
          kind: "error",
          message: err instanceof Error ? err.message : "Apply failed",
        });
      }
    },
    [refreshProducts, clearSelection],
  );

  // ---------------------------------------------------------------------------
  // Bulk action: Mark Reviewed
  // (Documented gap: no dedicated backend endpoint. Implemented as re-apply on
  //  the selected items. Only needs_review resolves to ok when a rule matches.)
  // ---------------------------------------------------------------------------

  const handleMarkReviewed = useCallback(
    async (ids: number[]) => {
      setActionState({ kind: "busy", label: "Re-applying ruleset on selected items…" });
      try {
        const result = await applyRuleset(ids);
        await refreshProducts();
        clearSelection();
        setActionState({
          kind: "success",
          message: `Ruleset re-applied to ${result.applied} product${result.applied === 1 ? "" : "s"}. Rows updated.`,
        });
      } catch (err) {
        setActionState({
          kind: "error",
          message: err instanceof Error ? err.message : "Mark reviewed failed",
        });
      }
    },
    [refreshProducts, clearSelection],
  );

  // ---------------------------------------------------------------------------
  // Bulk action: Set Override (opens modal for a single product)
  // ---------------------------------------------------------------------------

  const handleOpenOverrideModal = useCallback(() => {
    // For bulk-selected products we open the modal for the first selected product.
    // A more complete UX would iterate; MVP sets one at a time.
    const firstId = selectedProductIds[0];
    const product = products.find((p) => p.id === firstId);
    if (!product) return;
    setOverrideModal({ open: true, productId: firstId, productTitle: product.title });
  }, [selectedProductIds, products]);

  const handleOverrideConfirm = useCallback(
    async (entityId: number, templateIds: number[]) => {
      const productId = overrideModal.productId;
      if (productId === null) return;
      await setOverride({
        product_id: productId,
        entity_id: entityId,
        warning_template_ids: templateIds,
      });
      await refreshProducts();
      clearSelection();
      setOverrideModal({ open: false, productId: null, productTitle: "" });
      setActionState({
        kind: "success",
        message: `Override set for product #${productId}. Row updated.`,
      });
    },
    [overrideModal.productId, refreshProducts, clearSelection],
  );

  // ---------------------------------------------------------------------------
  // Bulk action: Clear Override
  // ---------------------------------------------------------------------------

  const handleClearOverride = useCallback(
    async (ids: number[]) => {
      setActionState({ kind: "busy", label: "Clearing overrides…" });
      try {
        await Promise.all(ids.map((id) => clearOverride(id)));
        await refreshProducts();
        clearSelection();
        setActionState({
          kind: "success",
          message: `Override cleared for ${ids.length} product${ids.length === 1 ? "" : "s"}. Rows updated.`,
        });
      } catch (err) {
        setActionState({
          kind: "error",
          message: err instanceof Error ? err.message : "Clear override failed",
        });
      }
    },
    [refreshProducts, clearSelection],
  );

  // ---------------------------------------------------------------------------
  // Load More
  // ---------------------------------------------------------------------------

  const handleLoadMore = useCallback(() => {
    void loadProducts(page + 1);
  }, [loadProducts, page]);

  // ---------------------------------------------------------------------------
  // Loading state
  // ---------------------------------------------------------------------------

  if (loading) {
    return (
      <Page title="Products">
        <Card>
          <BlockStack align="center" inlineAlign="center">
            <Spinner size="large" accessibilityLabel="Fetching products from server" />
            <Text as="p" variant="bodyMd">
              Loading products…
            </Text>
          </BlockStack>
        </Card>
      </Page>
    );
  }

  // ---------------------------------------------------------------------------
  // Error state
  // ---------------------------------------------------------------------------

  if (fetchError) {
    return (
      <Page title="Products">
        <Banner
          title="Failed to load products"
          tone="critical"
          action={{ content: "Retry", onAction: () => void loadProducts(1) }}
        >
          <p>{fetchError}</p>
        </Banner>
      </Page>
    );
  }

  // ---------------------------------------------------------------------------
  // Bulk actions definition
  // ---------------------------------------------------------------------------

  const isBusy = actionState.kind === "busy";

  // All bulk actions are promoted so Polaris renders them as visible buttons
  // in the IndexTable toolbar (not hidden inside an "Actions" dropdown).
  // This ensures per-item feedback is immediately visible — no opaque spinner.
  const promotedBulkActions = [
    {
      content: "Apply Ruleset",
      onAction: () => void handleApplyRuleset(selectedProductIds),
      disabled: isBusy,
    },
    {
      content: "Mark Reviewed",
      onAction: () => void handleMarkReviewed(selectedProductIds),
      disabled: isBusy,
    },
    {
      content: "Set Override",
      onAction: handleOpenOverrideModal,
      disabled: isBusy || selectedProductIds.length === 0,
    },
    {
      content: "Clear Override",
      onAction: () => void handleClearOverride(selectedProductIds),
      disabled: isBusy,
    },
  ];

  // ---------------------------------------------------------------------------
  // Table headings
  // ---------------------------------------------------------------------------

  const headings = [
    { title: "Product" },
    { title: "Tags / Category" },
    { title: "Status" },
    { title: "Matched Rule" },
    { title: "Entity" },
    { title: "Warnings" },
    { title: "Actions" },
  ] as [{ title: string }, ...{ title: string }[]];

  // ---------------------------------------------------------------------------
  // Rows
  // ---------------------------------------------------------------------------

  const rows = products.map((product, index) => {
    const cr = product.compliance;

    const tagsCategoryText = [
      // tags can be null/absent when a Shopify product has no tags.
      product.tags && product.tags.length > 0 ? product.tags.join(", ") : null,
      product.category ? `cat: ${product.category}` : null,
    ]
      .filter(Boolean)
      .join(" · ");

    return (
      <IndexTable.Row
        id={String(product.id)}
        key={product.id}
        selected={selectedResources.includes(String(product.id))}
        position={index}
      >
        {/* Product title */}
        <IndexTable.Cell>
          <Text as="span" fontWeight="semibold">
            {product.title}
          </Text>
        </IndexTable.Cell>

        {/* Tags + category */}
        <IndexTable.Cell>
          <Text as="span" variant="bodySm" tone="subdued">
            {tagsCategoryText || "—"}
          </Text>
        </IndexTable.Cell>

        {/* Compliance status — visually unmistakable */}
        <IndexTable.Cell>
          <ComplianceBadge status={cr ? cr.status : null} />
        </IndexTable.Cell>

        {/* Matched rule id */}
        <IndexTable.Cell>
          <Text as="span" variant="bodySm" tone="subdued">
            {cr?.matched_rule_id != null ? `Rule #${cr.matched_rule_id}` : "—"}
          </Text>
        </IndexTable.Cell>

        {/* Entity id */}
        <IndexTable.Cell>
          <Text as="span" variant="bodySm" tone="subdued">
            {cr?.entity_id != null ? `Entity #${cr.entity_id}` : "—"}
          </Text>
        </IndexTable.Cell>

        {/* Warning template ids */}
        <IndexTable.Cell>
          <Text as="span" variant="bodySm" tone="subdued">
            {cr && cr.warning_template_ids && cr.warning_template_ids.length > 0
              ? cr.warning_template_ids.map((id) => `#${id}`).join(", ")
              : "—"}
          </Text>
        </IndexTable.Cell>

        {/* Per-row actions — always accessible, not buried in the bulk actions disclosure menu */}
        <IndexTable.Cell>
          <InlineStack gap="200">
            <Button
              size="slim"
              variant="plain"
              onClick={() =>
                setOverrideModal({
                  open: true,
                  productId: product.id,
                  productTitle: product.title,
                })
              }
              disabled={isBusy}
              accessibilityLabel={`Set override for ${product.title}`}
            >
              Set Override
            </Button>
            {cr?.status === "override" && (
              <Button
                size="slim"
                variant="plain"
                tone="critical"
                onClick={() => void handleClearOverride([product.id])}
                disabled={isBusy}
                accessibilityLabel={`Clear override for ${product.title}`}
              >
                Clear Override
              </Button>
            )}
          </InlineStack>
        </IndexTable.Cell>
      </IndexTable.Row>
    );
  });

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <Page
      title="Products"
      subtitle="Bulk classify products and manage compliance status. Select products to apply the ruleset or set overrides."
    >
      <BlockStack gap="400">
        {/* Action feedback banners */}
        {actionState.kind === "success" && (
          <Banner
            tone="success"
            onDismiss={() => setActionState({ kind: "idle" })}
          >
            <p>{actionState.message}</p>
          </Banner>
        )}
        {actionState.kind === "error" && (
          <Banner
            tone="critical"
            onDismiss={() => setActionState({ kind: "idle" })}
          >
            <p>{actionState.message}</p>
          </Banner>
        )}
        {actionState.kind === "busy" && (
          <Banner tone="info">
            <InlineStack gap="200" blockAlign="center">
              <Spinner size="small" />
              <Text as="span">{actionState.label}</Text>
            </InlineStack>
          </Banner>
        )}

        {/* Products table */}
        <Card padding="0">
          {products.length === 0 ? (
            <EmptyState heading="No products found" image="">
              <p>
                Run a product sync from the backend to import your Shopify inventory,
                then return here to classify in bulk.
              </p>
            </EmptyState>
          ) : (
            <IndexTable
              resourceName={{ singular: "product", plural: "products" }}
              itemCount={products.length}
              selectedItemsCount={
                allResourcesSelected ? "All" : selectedResources.length
              }
              onSelectionChange={handleSelectionChange}
              headings={headings}
              promotedBulkActions={promotedBulkActions}
            >
              {rows}
            </IndexTable>
          )}
        </Card>

        {/* Pagination — Load More (offset pagination, not cursor) */}
        {hasNext && (
          <Box paddingBlock="400">
            <InlineStack align="center">
              <Button
                onClick={handleLoadMore}
                loading={loadingMore}
                disabled={loadingMore}
                accessibilityLabel="Load more products"
              >
                Load More
              </Button>
            </InlineStack>
          </Box>
        )}
      </BlockStack>

      {/* Override modal */}
      <OverrideModal
        open={overrideModal.open}
        productId={overrideModal.productId}
        productTitle={overrideModal.productTitle}
        entities={entities}
        templates={templates}
        onConfirm={handleOverrideConfirm}
        onClose={() =>
          setOverrideModal({ open: false, productId: null, productTitle: "" })
        }
      />
    </Page>
  );
}
