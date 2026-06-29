/**
 * WarningTemplateLibraryScreen — Warning template library (F4).
 *
 * Merchants create, edit, and delete warning templates here.
 * Rules reference templates by surrogate id. Deleting a referenced template is
 * blocked (409) — shown as a Polaris Banner error.
 *
 * Security: template.text is merchant input. It is rendered as React text
 * content — never via dangerouslySetInnerHTML. React escapes it by default.
 *
 * Styled with Shopify Polaris.
 */

import { useState, useEffect, useCallback } from "react";
import {
  Page,
  Card,
  IndexTable,
  Button,
  Modal,
  FormLayout,
  TextField,
  Banner,
  Badge,
  Text,
  InlineStack,
  Box,
  EmptyState,
  Spinner,
} from "@shopify/polaris";
import {
  fetchWarningTemplates,
  createWarningTemplate,
  updateWarningTemplate,
  deleteWarningTemplate,
} from "../api/client";
import type { WarningTemplate, WarningTemplateCreateBody } from "../api/types";

// ---------------------------------------------------------------------------
// Create / Edit modal form
// ---------------------------------------------------------------------------

interface TemplateFormModalProps {
  open: boolean;
  mode: "create" | "edit";
  initial?: WarningTemplate;
  onSave: (body: WarningTemplateCreateBody, id?: number) => Promise<void>;
  onClose: () => void;
}

function TemplateFormModal({ open, mode, initial, onSave, onClose }: TemplateFormModalProps) {
  const [text, setText] = useState(initial?.text ?? "");
  const [locale, setLocale] = useState(initial?.locale ?? "en");
  const [appliesToTags, setAppliesToTags] = useState(
    initial?.applies_to?.tags?.join(", ") ?? "",
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setText(initial?.text ?? "");
      setLocale(initial?.locale ?? "en");
      setAppliesToTags(initial?.applies_to?.tags?.join(", ") ?? "");
      setSaving(false);
      setError(null);
    }
  }, [open, initial]);

  const handleSave = async () => {
    if (!text.trim()) {
      setError("Warning text is required.");
      return;
    }
    setSaving(true);
    setError(null);

    const tags = appliesToTags
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean);

    const body: WarningTemplateCreateBody = {
      text: text.trim(),
      locale: locale.trim() || "en",
      applies_to: tags.length > 0 ? { tags } : undefined,
    };

    try {
      await onSave(body, initial?.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Save failed");
      setSaving(false);
    }
  };

  const title = mode === "create" ? "Add Warning Template" : "Edit Warning Template";

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={title}
      primaryAction={{
        content: saving ? "Saving…" : "Save",
        onAction: () => void handleSave(),
        disabled: saving,
        loading: saving,
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
        <FormLayout>
          <TextField
            label="Warning text"
            value={text}
            onChange={setText}
            multiline={3}
            autoComplete="off"
            disabled={saving}
            helpText="Merchant-authored safety warning text. Rendered as plain text on the storefront."
          />
          <TextField
            label="Locale"
            value={locale}
            onChange={setLocale}
            autoComplete="off"
            disabled={saving}
            helpText="BCP 47 locale code, e.g. en, de, fr. One locale per template."
            placeholder="en"
          />
          <TextField
            label="Applies to tags (optional)"
            value={appliesToTags}
            onChange={setAppliesToTags}
            autoComplete="off"
            disabled={saving}
            helpText="Comma-separated product tags this template targets. Leave blank for all products."
            placeholder="toys, electronics"
          />
        </FormLayout>
      </Modal.Section>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Delete confirmation modal
// ---------------------------------------------------------------------------

interface DeleteModalProps {
  template: WarningTemplate | null;
  deleting: boolean;
  error: string | null;
  onConfirm: () => void;
  onClose: () => void;
}

function DeleteModal({ template, deleting, error, onConfirm, onClose }: DeleteModalProps) {
  return (
    <Modal
      open={template !== null}
      onClose={onClose}
      title="Delete warning template?"
      primaryAction={{
        content: deleting ? "Deleting…" : "Confirm Delete",
        onAction: onConfirm,
        disabled: deleting,
        loading: deleting,
        destructive: true,
      }}
      secondaryActions={[
        { content: "Cancel", onAction: onClose, disabled: deleting },
      ]}
    >
      <Modal.Section>
        {error && (
          <Box paddingBlockEnd="400">
            <Banner tone="critical">
              {error}
            </Banner>
          </Box>
        )}
        {template && (
          <>
            <Text as="p">
              The template{" "}
              <Text as="span" fontWeight="semibold">
                [{template.locale}] {template.text.slice(0, 60)}
                {template.text.length > 60 ? "…" : ""}
              </Text>{" "}
              will be permanently removed.
            </Text>
            <Box paddingBlockStart="200">
              <Text as="p" tone="subdued">
                If this template is referenced by a classification rule, the deletion
                will be blocked — you will see an error message.
              </Text>
            </Box>
          </>
        )}
      </Modal.Section>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Main screen
// ---------------------------------------------------------------------------

type ModalMode =
  | { kind: "closed" }
  | { kind: "create" }
  | { kind: "edit"; template: WarningTemplate };

export function WarningTemplateLibraryScreen() {
  const [templates, setTemplates] = useState<WarningTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [modal, setModal] = useState<ModalMode>({ kind: "closed" });
  const [deleteTarget, setDeleteTarget] = useState<WarningTemplate | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setFetchError(null);
    try {
      const data = await fetchWarningTemplates();
      setTemplates(data);
    } catch (err) {
      setFetchError(err instanceof Error ? err.message : "Failed to load templates");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleSave = useCallback(
    async (body: WarningTemplateCreateBody, id?: number) => {
      if (id !== undefined) {
        const updated = await updateWarningTemplate(id, body);
        setTemplates((prev) => prev.map((t) => (t.id === id ? updated : t)));
      } else {
        const created = await createWarningTemplate(body);
        setTemplates((prev) => [...prev, created]);
      }
      setModal({ kind: "closed" });
    },
    [],
  );

  const handleDeleteRequest = useCallback((template: WarningTemplate) => {
    setDeleteTarget(template);
    setDeleteError(null);
  }, []);

  const handleDeleteConfirm = useCallback(async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await deleteWarningTemplate(deleteTarget.id);
      setTemplates((prev) => prev.filter((t) => t.id !== deleteTarget.id));
      setDeleteTarget(null);
      setDeleteError(null);
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : "Delete failed");
    } finally {
      setDeleting(false);
    }
  }, [deleteTarget]);

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  if (loading) {
    return (
      <Page title="Warning Template Library">
        <Card>
          <Box padding="800">
            <InlineStack align="center">
              <Spinner size="large" />
              <Text as="span">Loading warning templates…</Text>
            </InlineStack>
          </Box>
        </Card>
      </Page>
    );
  }

  if (fetchError) {
    return (
      <Page title="Warning Template Library">
        <Banner tone="critical" title="Failed to load templates">
          <p>{fetchError}</p>
        </Banner>
        <Box paddingBlockStart="400">
          <Button onClick={() => void load()}>Retry</Button>
        </Box>
      </Page>
    );
  }

  const rowMarkup = templates.map((template, index) => (
    <IndexTable.Row
      id={String(template.id)}
      key={template.id}
      position={index}
    >
      <IndexTable.Cell>
        <Badge>{template.locale}</Badge>
      </IndexTable.Cell>
      <IndexTable.Cell>
        {/* Rendered as text — React escapes by default; no dangerouslySetInnerHTML */}
        <Text as="span">
          {template.text}
        </Text>
      </IndexTable.Cell>
      <IndexTable.Cell>
        {template.applies_to?.tags && template.applies_to.tags.length > 0
          ? template.applies_to.tags.join(", ")
          : <Text as="span" tone="subdued">All products</Text>
        }
      </IndexTable.Cell>
      <IndexTable.Cell>
        <InlineStack gap="200">
          <Button
            size="slim"
            onClick={() => setModal({ kind: "edit", template })}
            accessibilityLabel={`Edit warning template ${template.text.slice(0, 30)}`}
          >
            Edit
          </Button>
          <Button
            size="slim"
            tone="critical"
            onClick={() => handleDeleteRequest(template)}
            accessibilityLabel={`Delete warning template ${template.text.slice(0, 30)}`}
          >
            Delete
          </Button>
        </InlineStack>
      </IndexTable.Cell>
    </IndexTable.Row>
  ));

  return (
    <Page
      title="Warning Template Library"
      subtitle="Manage reusable safety warning texts. Classification rules reference templates by id."
      primaryAction={
        <Button variant="primary" onClick={() => setModal({ kind: "create" })}>
          Add Warning Template
        </Button>
      }
    >
      <Card padding="0">
        {templates.length === 0 ? (
          <EmptyState
            heading="No warning templates yet"
            image=""
          >
            <p>Add a warning template to use it in classification rules.</p>
          </EmptyState>
        ) : (
          <IndexTable
            resourceName={{ singular: "template", plural: "templates" }}
            itemCount={templates.length}
            headings={[
              { title: "Locale" },
              { title: "Warning text" },
              { title: "Applies to" },
              { title: "Actions" },
            ]}
            selectable={false}
          >
            {rowMarkup}
          </IndexTable>
        )}
      </Card>

      {/* Create / Edit modal */}
      <TemplateFormModal
        open={modal.kind !== "closed"}
        mode={modal.kind === "edit" ? "edit" : "create"}
        initial={modal.kind === "edit" ? modal.template : undefined}
        onSave={handleSave}
        onClose={() => setModal({ kind: "closed" })}
      />

      {/* Delete confirmation modal */}
      <DeleteModal
        template={deleteTarget}
        deleting={deleting}
        error={deleteError}
        onConfirm={() => void handleDeleteConfirm()}
        onClose={() => {
          if (!deleting) {
            setDeleteTarget(null);
            setDeleteError(null);
          }
        }}
      />
    </Page>
  );
}
