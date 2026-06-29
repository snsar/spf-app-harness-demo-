/**
 * RulesScreen — Classification rules config (F5).
 *
 * Renders the ordered list of classification rules with VISIBLE PRECEDENCE
 * (C1: ordered by priority asc, id asc — first rule in the list is the first
 * the engine will attempt to match). Each row shows its rank number (1, 2, 3...)
 * so the merchant immediately understands which rule wins.
 *
 * Reordering precedence = editing a rule's `priority` field (Q5: no bulk-reorder
 * endpoint exists). The merchant edits a rule's priority and the list re-sorts
 * immediately after the save to preview the new order.
 *
 * match_conditions: null/absent field = "don't constrain on this attribute" (C4).
 * The form makes present-vs-absent explicit — empty string is treated as absent.
 *
 * All UI uses Shopify Polaris. Requires <AppProvider> in the tree (F4 owns the shell).
 */

import { useState, useEffect, useCallback } from "react";
import {
  Page,
  Card,
  IndexTable,
  Text,
  Button,
  Badge,
  Banner,
  Modal,
  Spinner,
  BlockStack,
  InlineStack,
  EmptyState,
  useIndexResourceState,
} from "@shopify/polaris";
import {
  getRules,
  createRule,
  updateRule,
  deleteRule,
  getEntities,
  getWarningTemplates,
  type Rule,
  type Entity,
  type WarningTemplate,
} from "../api/rules";
import { sortRulesForDisplay } from "./rulesSort";
import { RuleFormModal } from "./RuleFormModal";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatConditions(rule: Rule): string {
  const { match_conditions: mc } = rule;
  const parts: string[] = [];

  if (mc.tags && mc.tags.length > 0) {
    parts.push(`tags: ${mc.tags.join(", ")}`);
  }
  if (mc.category != null && mc.category !== "") {
    parts.push(`category: ${mc.category}`);
  }
  if (mc.material != null && mc.material !== "") {
    parts.push(`material: ${mc.material}`);
  }
  if (mc.origin != null && mc.origin !== "") {
    parts.push(`origin: ${mc.origin}`);
  }

  return parts.length > 0 ? parts.join(" · ") : "(any product)";
}

// ---------------------------------------------------------------------------
// Main screen component
// ---------------------------------------------------------------------------

type ModalMode =
  | { kind: "closed" }
  | { kind: "create" }
  | { kind: "edit"; rule: Rule };

export function RulesScreen() {
  const [rules, setRules] = useState<Rule[]>([]);
  const [entities, setEntities] = useState<Entity[]>([]);
  const [templates, setTemplates] = useState<WarningTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [modal, setModal] = useState<ModalMode>({ kind: "closed" });
  const [deleteConfirmId, setDeleteConfirmId] = useState<number | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const [rulesData, entitiesData, templatesData] = await Promise.all([
        getRules(),
        getEntities(),
        getWarningTemplates(),
      ]);
      setRules(rulesData);
      setEntities(entitiesData);
      setTemplates(templatesData);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : "Failed to load data");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleCreate = useCallback(() => {
    setModal({ kind: "create" });
  }, []);

  const handleEdit = useCallback((rule: Rule) => {
    setModal({ kind: "edit", rule });
  }, []);

  const handleModalSave = useCallback(
    async (body: Parameters<typeof createRule>[0], editId?: number) => {
      if (editId !== undefined) {
        const updated = await updateRule(editId, body);
        setRules((prev) => prev.map((r) => (r.id === editId ? updated : r)));
      } else {
        const created = await createRule(body);
        setRules((prev) => [...prev, created]);
      }
      setModal({ kind: "closed" });
    },
    [],
  );

  const handleDeleteRequest = useCallback((id: number) => {
    setDeleteConfirmId(id);
    setDeleteError(null);
  }, []);

  const handleDeleteConfirm = useCallback(async () => {
    if (deleteConfirmId === null) return;
    setDeleting(true);
    try {
      await deleteRule(deleteConfirmId);
      setRules((prev) => prev.filter((r) => r.id !== deleteConfirmId));
      setDeleteConfirmId(null);
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : "Delete failed");
    } finally {
      setDeleting(false);
    }
  }, [deleteConfirmId]);

  const sortedRules = sortRulesForDisplay(rules);

  const entityMap = new Map(entities.map((e) => [e.id, e]));
  const templateMap = new Map(templates.map((t) => [t.id, t]));

  // IndexTable requires resource state
  const resourceName = { singular: "rule", plural: "rules" };
  const { selectedResources, allResourcesSelected, handleSelectionChange } =
    useIndexResourceState(sortedRules.map((r) => ({ id: String(r.id) })));

  // ---------------------------------------------------------------------------
  // Loading state
  // ---------------------------------------------------------------------------

  if (loading) {
    return (
      <Page title="Classification Rules">
        <Card>
          <BlockStack align="center" inlineAlign="center">
            <Spinner size="large" accessibilityLabel="Loading rules" />
            <Text as="p" variant="bodyMd">
              Loading classification rules...
            </Text>
          </BlockStack>
        </Card>
      </Page>
    );
  }

  // ---------------------------------------------------------------------------
  // Error state
  // ---------------------------------------------------------------------------

  if (loadError) {
    return (
      <Page title="Classification Rules">
        <Banner
          title="Failed to load rules"
          tone="critical"
          action={{ content: "Retry", onAction: () => void load() }}
        >
          <p>{loadError}</p>
        </Banner>
      </Page>
    );
  }

  // ---------------------------------------------------------------------------
  // IndexTable columns
  // ---------------------------------------------------------------------------

  // Cast satisfies NonEmptyArray<IndexTableHeading> — we know this is non-empty
  // at the type level since it's a literal array with 6 fixed elements.
  const columnHeadings = [
    { title: "Rank" },
    { title: "Priority" },
    { title: "Match conditions" },
    { title: "Entity (responsible operator)" },
    { title: "Warning templates" },
    { title: "Actions" },
  ] as [{ title: string }, ...{ title: string }[]];

  // ---------------------------------------------------------------------------
  // Rows
  // ---------------------------------------------------------------------------

  const rows = sortedRules.map((rule, index) => {
    const entity = entityMap.get(rule.entity_id);
    const rank = index + 1;
    const rankLabel = `#${rank}` as string;
    const rankA11yLabel = `Precedence rank ${rank}: evaluated ${rank === 1 ? "first" : `at position ${rank}`}` as string;

    const templateSummary = rule.warning_template_ids
      .map((tid) => {
        const t = templateMap.get(tid);
        return t
          ? `[${t.locale}] ${t.text.slice(0, 40)}${t.text.length > 40 ? "..." : ""}`
          : `#${tid}`;
      })
      .join(", ");

    return (
      <IndexTable.Row
        id={String(rule.id)}
        key={rule.id}
        selected={selectedResources.includes(String(rule.id))}
        position={index}
      >
        {/* Rank — the most important column: makes precedence unmistakeable */}
        <IndexTable.Cell>
          <Badge
            tone={rank === 1 ? "success" : "info"}
            toneAndProgressLabelOverride={rankA11yLabel}
          >
            {rankLabel}
          </Badge>
        </IndexTable.Cell>

        {/* Priority numeric value */}
        <IndexTable.Cell>
          <Text as="span" fontWeight="semibold">
            {rule.priority}
          </Text>
        </IndexTable.Cell>

        {/* Match conditions */}
        <IndexTable.Cell>
          <Text as="span" variant="bodyMd" tone="subdued">
            {formatConditions(rule)}
          </Text>
        </IndexTable.Cell>

        {/* Entity */}
        <IndexTable.Cell>
          {entity ? (
            <InlineStack gap="100" blockAlign="center">
              <Text as="span">{entity.name}</Text>
              {entity.is_eu && (
                <Badge tone="success" toneAndProgressLabelOverride="EU-based entity">
                  EU
                </Badge>
              )}
            </InlineStack>
          ) : (
            <Text as="span" tone="critical">
              Entity #{rule.entity_id} (not found)
            </Text>
          )}
        </IndexTable.Cell>

        {/* Warning templates */}
        <IndexTable.Cell>
          {rule.warning_template_ids.length === 0 ? (
            <Text as="span" tone="subdued">
              None
            </Text>
          ) : (
            <Text as="span" variant="bodySm">
              {templateSummary || `${rule.warning_template_ids.length} template(s)`}
            </Text>
          )}
        </IndexTable.Cell>

        {/* Actions */}
        <IndexTable.Cell>
          <InlineStack gap="200">
            <Button
              variant="plain"
              onClick={() => handleEdit(rule)}
              accessibilityLabel={`Edit rule #${rule.id} (priority ${rule.priority})`}
            >
              Edit
            </Button>
            <Button
              variant="plain"
              tone="critical"
              onClick={() => handleDeleteRequest(rule.id)}
              accessibilityLabel={`Delete rule #${rule.id}`}
            >
              Delete
            </Button>
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
      title="Classification Rules"
      subtitle="Rules are evaluated in precedence order (rank #1 first). The first rule that matches a product wins. Lower priority number = higher precedence. Edit a rule's priority to change its position."
      primaryAction={{
        content: "Add rule",
        onAction: handleCreate,
      }}
    >
      <BlockStack gap="400">
        {/* Precedence explanation banner */}
        <Banner tone="info">
          <p>
            <strong>How precedence works:</strong> The engine evaluates rules
            top-to-bottom (rank #1 first). The <em>first</em> rule whose
            conditions all match the product wins. To change precedence, edit
            a rule and set a lower priority number to move it higher.
          </p>
        </Banner>

        {/* Rules table */}
        <Card padding="0">
          {sortedRules.length === 0 ? (
            <EmptyState
              heading="No classification rules yet"
              action={{ content: "Add first rule", onAction: handleCreate }}
              image=""
            >
              <p>
                Add a rule to start classifying products automatically. Each rule
                maps matching products to a responsible entity and required warnings.
              </p>
            </EmptyState>
          ) : (
            <IndexTable
              resourceName={resourceName}
              itemCount={sortedRules.length}
              selectedItemsCount={
                allResourcesSelected ? "All" : selectedResources.length
              }
              onSelectionChange={handleSelectionChange}
              headings={columnHeadings}
            >
              {rows}
            </IndexTable>
          )}
        </Card>
      </BlockStack>

      {/* Delete confirmation modal */}
      <Modal
        open={deleteConfirmId !== null}
        onClose={() => {
          if (!deleting) {
            setDeleteConfirmId(null);
            setDeleteError(null);
          }
        }}
        title="Delete rule?"
        primaryAction={{
          content: deleting ? "Deleting..." : "Delete",
          destructive: true,
          disabled: deleting,
          onAction: () => void handleDeleteConfirm(),
        }}
        secondaryActions={[
          {
            content: "Cancel",
            disabled: deleting,
            onAction: () => {
              setDeleteConfirmId(null);
              setDeleteError(null);
            },
          },
        ]}
      >
        <Modal.Section>
          <BlockStack gap="200">
            <Text as="p">
              Rule #{deleteConfirmId} will be permanently removed. This cannot be
              undone. Products that were classified by this rule will remain as-is
              until you re-run the ruleset.
            </Text>
            {deleteError && (
              <Banner tone="critical">
                <p>{deleteError}</p>
              </Banner>
            )}
          </BlockStack>
        </Modal.Section>
      </Modal>

      {/* Create / Edit modal */}
      {modal.kind !== "closed" && (
        <RuleFormModal
          mode={modal.kind}
          initialRule={modal.kind === "edit" ? modal.rule : undefined}
          entities={entities}
          templates={templates}
          onSave={handleModalSave}
          onClose={() => setModal({ kind: "closed" })}
        />
      )}
    </Page>
  );
}
