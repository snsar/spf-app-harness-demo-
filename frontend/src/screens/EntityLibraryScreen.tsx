/**
 * EntityLibraryScreen — Responsible Economic Operator library (F4).
 *
 * Merchants create, edit, and delete entities (responsible operators) here.
 * Rules reference entities by surrogate id. Deleting a referenced entity is
 * blocked by the backend (409) — this screen shows a Polaris Banner error
 * instead of crashing or silently removing the entity.
 *
 * Styled with Shopify Polaris (AppProvider wraps the whole app in App.tsx).
 * No dangerouslySetInnerHTML; all merchant input is rendered as text.
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
  Checkbox,
  Select,
  Banner,
  Badge,
  Text,
  InlineStack,
  Box,
  EmptyState,
  Spinner,
} from "@shopify/polaris";
import {
  fetchEntities,
  createEntity,
  updateEntity,
  deleteEntity,
} from "../api/client";
import type { Entity, EntityCreateBody } from "../api/types";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const ROLE_OPTIONS = [
  { label: "Select a role", value: "" },
  { label: "Importer", value: "importer" },
  { label: "Manufacturer", value: "manufacturer" },
  { label: "Distributor", value: "distributor" },
  { label: "Authorised representative", value: "authorised_representative" },
  { label: "Fulfilment service provider", value: "fulfilment_service_provider" },
  { label: "Other", value: "other" },
];

// ---------------------------------------------------------------------------
// Create / Edit modal form
// ---------------------------------------------------------------------------

interface EntityFormModalProps {
  open: boolean;
  mode: "create" | "edit";
  initial?: Entity;
  onSave: (body: EntityCreateBody, id?: number) => Promise<void>;
  onClose: () => void;
}

function EntityFormModal({ open, mode, initial, onSave, onClose }: EntityFormModalProps) {
  const [name, setName] = useState(initial?.name ?? "");
  const [address, setAddress] = useState(initial?.address ?? "");
  const [role, setRole] = useState(initial?.role ?? "");
  const [isEu, setIsEu] = useState(initial?.is_eu ?? false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset form when modal opens with new initial data
  useEffect(() => {
    if (open) {
      setName(initial?.name ?? "");
      setAddress(initial?.address ?? "");
      setRole(initial?.role ?? "");
      setIsEu(initial?.is_eu ?? false);
      setSaving(false);
      setError(null);
    }
  }, [open, initial]);

  const handleSave = async () => {
    if (!name.trim() || !address.trim() || !role) {
      setError("Name, address, and role are required.");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await onSave(
        { name: name.trim(), address: address.trim(), role, is_eu: isEu },
        initial?.id,
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "Save failed");
      setSaving(false);
    }
  };

  const title = mode === "create" ? "Add Entity" : "Edit Entity";

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
            label="Name"
            value={name}
            onChange={setName}
            autoComplete="organization"
            disabled={saving}
          />
          <TextField
            label="Address"
            value={address}
            onChange={setAddress}
            autoComplete="street-address"
            disabled={saving}
            helpText="Full address of the responsible operator."
          />
          <Select
            label="Role"
            options={ROLE_OPTIONS}
            value={role}
            onChange={setRole}
            disabled={saving}
          />
          <Checkbox
            label="EU-based entity"
            checked={isEu}
            onChange={setIsEu}
            disabled={saving}
            helpText="Check if this operator is based in the European Union."
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
  entity: Entity | null;
  deleting: boolean;
  error: string | null;
  onConfirm: () => void;
  onClose: () => void;
}

function DeleteModal({ entity, deleting, error, onConfirm, onClose }: DeleteModalProps) {
  return (
    <Modal
      open={entity !== null}
      onClose={onClose}
      title="Delete entity?"
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
        <Text as="p">
          <Text as="span" fontWeight="semibold">{entity?.name}</Text> will be permanently
          removed. This cannot be undone.
        </Text>
        <Box paddingBlockStart="200">
          <Text as="p" tone="subdued">
            If this entity is referenced by a classification rule, the deletion will be
            blocked — you will see an error message.
          </Text>
        </Box>
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
  | { kind: "edit"; entity: Entity };

export function EntityLibraryScreen() {
  const [entities, setEntities] = useState<Entity[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [modal, setModal] = useState<ModalMode>({ kind: "closed" });
  const [deleteTarget, setDeleteTarget] = useState<Entity | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setFetchError(null);
    try {
      const data = await fetchEntities();
      setEntities(data);
    } catch (err) {
      setFetchError(err instanceof Error ? err.message : "Failed to load entities");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleSave = useCallback(
    async (body: EntityCreateBody, id?: number) => {
      if (id !== undefined) {
        const updated = await updateEntity(id, body);
        setEntities((prev) => prev.map((e) => (e.id === id ? updated : e)));
      } else {
        const created = await createEntity(body);
        setEntities((prev) => [...prev, created]);
      }
      setModal({ kind: "closed" });
    },
    [],
  );

  const handleDeleteRequest = useCallback((entity: Entity) => {
    setDeleteTarget(entity);
    setDeleteError(null);
  }, []);

  const handleDeleteConfirm = useCallback(async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await deleteEntity(deleteTarget.id);
      setEntities((prev) => prev.filter((e) => e.id !== deleteTarget.id));
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
      <Page title="Responsible Entity Library">
        <Card>
          <Box padding="800">
            <InlineStack align="center">
              <Spinner size="large" />
              <Text as="span">Loading entities…</Text>
            </InlineStack>
          </Box>
        </Card>
      </Page>
    );
  }

  if (fetchError) {
    return (
      <Page title="Responsible Entity Library">
        <Banner tone="critical" title="Failed to load entities">
          <p>{fetchError}</p>
        </Banner>
        <Box paddingBlockStart="400">
          <Button onClick={() => void load()}>Retry</Button>
        </Box>
      </Page>
    );
  }

  const rowMarkup = entities.map((entity, index) => (
    <IndexTable.Row
      id={String(entity.id)}
      key={entity.id}
      position={index}
    >
      <IndexTable.Cell>
        <Text as="span" fontWeight="semibold">
          {entity.name}
        </Text>
      </IndexTable.Cell>
      <IndexTable.Cell>{entity.address}</IndexTable.Cell>
      <IndexTable.Cell>
        <Badge>{entity.role}</Badge>
      </IndexTable.Cell>
      <IndexTable.Cell>
        {entity.is_eu ? (
          <Badge tone="success">EU</Badge>
        ) : (
          <Badge tone="attention">Non-EU</Badge>
        )}
      </IndexTable.Cell>
      <IndexTable.Cell>
        <InlineStack gap="200">
          <Button
            size="slim"
            onClick={() => setModal({ kind: "edit", entity })}
            accessibilityLabel={`Edit entity ${entity.name}`}
          >
            Edit
          </Button>
          <Button
            size="slim"
            tone="critical"
            onClick={() => handleDeleteRequest(entity)}
            accessibilityLabel={`Delete entity ${entity.name}`}
          >
            Delete
          </Button>
        </InlineStack>
      </IndexTable.Cell>
    </IndexTable.Row>
  ));

  return (
    <Page
      title="Responsible Entity Library"
      subtitle="Manage EU responsible economic operators. Classification rules reference entities by id."
      primaryAction={
        <Button variant="primary" onClick={() => setModal({ kind: "create" })}>
          Add Entity
        </Button>
      }
    >
      <Card padding="0">
        {entities.length === 0 ? (
          <EmptyState
            heading="No entities yet"
            image=""
          >
            <p>Add a responsible operator to get started.</p>
          </EmptyState>
        ) : (
          <IndexTable
            resourceName={{ singular: "entity", plural: "entities" }}
            itemCount={entities.length}
            headings={[
              { title: "Name" },
              { title: "Address" },
              { title: "Role" },
              { title: "EU" },
              { title: "Actions" },
            ]}
            selectable={false}
          >
            {rowMarkup}
          </IndexTable>
        )}
      </Card>

      {/* Create / Edit modal */}
      <EntityFormModal
        open={modal.kind !== "closed"}
        mode={modal.kind === "edit" ? "edit" : "create"}
        initial={modal.kind === "edit" ? modal.entity : undefined}
        onSave={handleSave}
        onClose={() => setModal({ kind: "closed" })}
      />

      {/* Delete confirmation modal */}
      <DeleteModal
        entity={deleteTarget}
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
