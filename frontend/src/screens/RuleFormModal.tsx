/**
 * RuleFormModal — Create / Edit a classification rule (F5).
 *
 * Key UX invariants:
 * - match_conditions: null/absent field = "don't constrain on this attribute" (C4).
 *   Each condition field has a Checkbox to opt in. Unchecked = absent (wildcard).
 *   Checked-but-blank is a validation error surfaced via InlineError.
 * - entity_id is a Select sourced from GET /api/entities (own shop only).
 * - warning_template_ids are Checkboxes sourced from GET /api/warning-templates.
 * - Priority: integer; lower = higher precedence. Changing priority re-sorts the list.
 * - Backend 400/404 (cross-shop ref violations) are surfaced via Banner.
 *
 * All UI uses Shopify Polaris. Requires <AppProvider> in the tree (F4 owns the shell).
 */

import { useState } from "react";
import {
  Modal,
  FormLayout,
  TextField,
  Select,
  Checkbox,
  Banner,
  Text,
  BlockStack,
  Divider,
  InlineError,
} from "@shopify/polaris";
import type { Rule, Entity, WarningTemplate, CreateRuleBody, MatchConditions } from "../api/rules";

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

interface RuleFormModalProps {
  mode: "create" | "edit";
  initialRule?: Rule;
  entities: Entity[];
  templates: WarningTemplate[];
  onSave: (body: CreateRuleBody, editId?: number) => Promise<void>;
  onClose: () => void;
}

// ---------------------------------------------------------------------------
// Internal state shape for the form
// ---------------------------------------------------------------------------

interface ConditionField {
  /** true = the condition is active and will be sent to the backend */
  active: boolean;
  value: string;
}

interface FormState {
  priority: string;
  entity_id: string;
  warning_template_ids: number[];
  cond_tags: ConditionField;     // comma-separated in the input
  cond_category: ConditionField;
  cond_material: ConditionField;
  cond_origin: ConditionField;
}

type CondKey = "cond_tags" | "cond_category" | "cond_material" | "cond_origin";

function initialFormState(rule?: Rule): FormState {
  if (!rule) {
    return {
      priority: "100",
      entity_id: "",
      warning_template_ids: [],
      cond_tags: { active: false, value: "" },
      cond_category: { active: false, value: "" },
      cond_material: { active: false, value: "" },
      cond_origin: { active: false, value: "" },
    };
  }

  const mc = rule.match_conditions;
  return {
    priority: String(rule.priority),
    entity_id: String(rule.entity_id),
    warning_template_ids: rule.warning_template_ids,
    cond_tags: {
      active: Array.isArray(mc.tags) && mc.tags.length > 0,
      value: Array.isArray(mc.tags) ? mc.tags.join(", ") : "",
    },
    cond_category: {
      active: mc.category != null && mc.category !== "",
      value: mc.category ?? "",
    },
    cond_material: {
      active: mc.material != null && mc.material !== "",
      value: mc.material ?? "",
    },
    cond_origin: {
      active: mc.origin != null && mc.origin !== "",
      value: mc.origin ?? "",
    },
  };
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function RuleFormModal({
  mode,
  initialRule,
  entities,
  templates,
  onSave,
  onClose,
}: RuleFormModalProps) {
  const [form, setForm] = useState<FormState>(() => initialFormState(initialRule));
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Partial<Record<CondKey | "priority" | "entity_id", string>>>({});

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function setCondActive(key: CondKey, active: boolean) {
    setForm((prev) => ({ ...prev, [key]: { ...prev[key], active } }));
    // Clear field error when user unchecks
    if (!active) {
      setFieldErrors((prev) => ({ ...prev, [key]: undefined }));
    }
  }

  function setCondValue(key: CondKey, value: string) {
    setForm((prev) => ({ ...prev, [key]: { ...prev[key], value } }));
    if (value.trim()) {
      setFieldErrors((prev) => ({ ...prev, [key]: undefined }));
    }
  }

  function setPriority(value: string) {
    setForm((prev) => ({ ...prev, priority: value }));
    setFieldErrors((prev) => ({ ...prev, priority: undefined }));
  }

  function setEntityId(value: string) {
    setForm((prev) => ({ ...prev, entity_id: value }));
    setFieldErrors((prev) => ({ ...prev, entity_id: undefined }));
  }

  function toggleTemplate(id: number) {
    setForm((prev) => {
      const ids = prev.warning_template_ids;
      const next = ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id];
      return { ...prev, warning_template_ids: next };
    });
  }

  // ---------------------------------------------------------------------------
  // Validation and submission
  // ---------------------------------------------------------------------------

  function buildMatchConditions(): MatchConditions {
    const mc: MatchConditions = {};
    if (form.cond_tags.active) {
      mc.tags = form.cond_tags.value
        .split(",")
        .map((t) => t.trim())
        .filter(Boolean);
    }
    if (form.cond_category.active && form.cond_category.value.trim()) {
      mc.category = form.cond_category.value.trim();
    }
    if (form.cond_material.active && form.cond_material.value.trim()) {
      mc.material = form.cond_material.value.trim();
    }
    if (form.cond_origin.active && form.cond_origin.value.trim()) {
      mc.origin = form.cond_origin.value.trim();
    }
    return mc;
  }

  function validate(): boolean {
    const errors: typeof fieldErrors = {};
    const priority = parseInt(form.priority, 10);
    if (isNaN(priority) || priority < 0) {
      errors.priority = "Priority must be a non-negative integer.";
    }
    if (!form.entity_id) {
      errors.entity_id = "Select a responsible entity.";
    }
    if (form.cond_tags.active) {
      const tags = form.cond_tags.value
        .split(",")
        .map((t) => t.trim())
        .filter(Boolean);
      if (tags.length === 0) {
        errors.cond_tags =
          "Tags condition is checked but no tags were entered. Add tags or uncheck this condition.";
      }
    }
    if (form.cond_category.active && !form.cond_category.value.trim()) {
      errors.cond_category =
        "Category is checked but empty. Enter a value or uncheck this condition.";
    }
    if (form.cond_material.active && !form.cond_material.value.trim()) {
      errors.cond_material =
        "Material is checked but empty. Enter a value or uncheck this condition.";
    }
    if (form.cond_origin.active && !form.cond_origin.value.trim()) {
      errors.cond_origin =
        "Origin is checked but empty. Enter a value or uncheck this condition.";
    }
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  }

  async function handleSave() {
    setSubmitError(null);
    if (!validate()) return;

    setSubmitting(true);
    try {
      const body: CreateRuleBody = {
        priority: parseInt(form.priority, 10),
        match_conditions: buildMatchConditions(),
        entity_id: parseInt(form.entity_id, 10),
        warning_template_ids: form.warning_template_ids,
      };
      await onSave(body, initialRule?.id);
    } catch (err) {
      setSubmitError(
        err instanceof Error ? err.message : "Failed to save rule",
      );
    } finally {
      setSubmitting(false);
    }
  }

  // ---------------------------------------------------------------------------
  // Entity options for Polaris Select
  // ---------------------------------------------------------------------------

  const entityOptions = [
    { label: "-- Select entity --", value: "" },
    ...entities.map((e) => ({
      label: `${e.name} (${e.role})${e.is_eu ? " [EU]" : ""}`,
      value: String(e.id),
    })),
  ];

  const title =
    mode === "create"
      ? "Add classification rule"
      : `Edit rule #${initialRule?.id ?? ""}`;

  return (
    <Modal
      open
      onClose={onClose}
      title={title}
      primaryAction={{
        content: submitting
          ? "Saving..."
          : mode === "create"
            ? "Add rule"
            : "Save changes",
        disabled: submitting,
        onAction: () => void handleSave(),
      }}
      secondaryActions={[
        { content: "Cancel", disabled: submitting, onAction: onClose },
      ]}
      size="large"
    >
      <Modal.Section>
        <BlockStack gap="400">
          {/* Submit / API error banner */}
          {submitError && (
            <Banner
              title="Could not save rule"
              tone="critical"
              onDismiss={() => setSubmitError(null)}
            >
              <p>{submitError}</p>
            </Banner>
          )}

          <FormLayout>
            {/* Priority */}
            <TextField
              label="Priority (lower number = higher precedence)"
              type="number"
              value={form.priority}
              onChange={setPriority}
              helpText="Integer >= 0. Use 100, 200, 300... to leave room for new rules between existing ones. Two rules with the same priority are sorted by id (lower id wins)."
              error={fieldErrors.priority}
              autoComplete="off"
            />
          </FormLayout>

          <Divider />

          {/* Match conditions */}
          <BlockStack gap="300">
            <Text as="h3" variant="headingSm">
              Match conditions
            </Text>
            <Text as="p" variant="bodySm" tone="subdued">
              Unchecked = wildcard (this field is not constrained). Checked-but-blank
              is a validation error — it would never match and likely indicates a
              mistake (C4: missing field does not match "matches-empty").
            </Text>

            {/* Tags */}
            <BlockStack gap="100">
              <Checkbox
                label="Constrain on tags"
                checked={form.cond_tags.active}
                onChange={(checked) => setCondActive("cond_tags", checked)}
              />
              {form.cond_tags.active && (
                <>
                  <TextField
                    label="Tags (comma-separated)"
                    labelHidden
                    placeholder="toys, electronics, ..."
                    value={form.cond_tags.value}
                    onChange={(v) => setCondValue("cond_tags", v)}
                    autoComplete="off"
                    error={!!fieldErrors.cond_tags}
                  />
                  {fieldErrors.cond_tags && (
                    <InlineError
                      message={fieldErrors.cond_tags}
                      fieldID="cond_tags"
                    />
                  )}
                </>
              )}
            </BlockStack>

            {/* Category */}
            <BlockStack gap="100">
              <Checkbox
                label="Constrain on category"
                checked={form.cond_category.active}
                onChange={(checked) => setCondActive("cond_category", checked)}
              />
              {form.cond_category.active && (
                <>
                  <TextField
                    label="Category"
                    labelHidden
                    placeholder="e.g. toys"
                    value={form.cond_category.value}
                    onChange={(v) => setCondValue("cond_category", v)}
                    autoComplete="off"
                    error={!!fieldErrors.cond_category}
                  />
                  {fieldErrors.cond_category && (
                    <InlineError
                      message={fieldErrors.cond_category}
                      fieldID="cond_category"
                    />
                  )}
                </>
              )}
            </BlockStack>

            {/* Material */}
            <BlockStack gap="100">
              <Checkbox
                label="Constrain on material"
                checked={form.cond_material.active}
                onChange={(checked) => setCondActive("cond_material", checked)}
              />
              {form.cond_material.active && (
                <>
                  <TextField
                    label="Material"
                    labelHidden
                    placeholder="e.g. wood"
                    value={form.cond_material.value}
                    onChange={(v) => setCondValue("cond_material", v)}
                    autoComplete="off"
                    error={!!fieldErrors.cond_material}
                  />
                  {fieldErrors.cond_material && (
                    <InlineError
                      message={fieldErrors.cond_material}
                      fieldID="cond_material"
                    />
                  )}
                </>
              )}
            </BlockStack>

            {/* Origin */}
            <BlockStack gap="100">
              <Checkbox
                label="Constrain on origin (country code)"
                checked={form.cond_origin.active}
                onChange={(checked) => setCondActive("cond_origin", checked)}
              />
              {form.cond_origin.active && (
                <>
                  <TextField
                    label="Origin country code"
                    labelHidden
                    placeholder="e.g. CN"
                    value={form.cond_origin.value}
                    onChange={(v) => setCondValue("cond_origin", v)}
                    maxLength={2}
                    autoComplete="off"
                    error={!!fieldErrors.cond_origin}
                  />
                  {fieldErrors.cond_origin && (
                    <InlineError
                      message={fieldErrors.cond_origin}
                      fieldID="cond_origin"
                    />
                  )}
                </>
              )}
            </BlockStack>
          </BlockStack>

          <Divider />

          {/* Entity picker */}
          <BlockStack gap="200">
            {entities.length === 0 ? (
              <Banner tone="warning">
                <p>
                  No entities available. Create a responsible entity in the Entity
                  library before adding a rule.
                </p>
              </Banner>
            ) : (
              <>
                <Select
                  label="Responsible entity"
                  options={entityOptions}
                  value={form.entity_id}
                  onChange={setEntityId}
                  error={fieldErrors.entity_id}
                />
              </>
            )}
          </BlockStack>

          <Divider />

          {/* Warning templates */}
          <BlockStack gap="200">
            <Text as="h3" variant="headingSm">
              Warning templates
            </Text>
            {templates.length === 0 ? (
              <Banner tone="warning">
                <p>
                  No warning templates available. Create one in the Warning template
                  library before adding a rule.
                </p>
              </Banner>
            ) : (
              <BlockStack gap="100">
                {templates.map((t) => (
                  <Checkbox
                    key={t.id}
                    label={`[${t.locale}] ${
                      t.text.length > 80 ? t.text.slice(0, 80) + "..." : t.text
                    }`}
                    checked={form.warning_template_ids.includes(t.id)}
                    onChange={() => toggleTemplate(t.id)}
                  />
                ))}
              </BlockStack>
            )}
          </BlockStack>
        </BlockStack>
      </Modal.Section>
    </Modal>
  );
}
