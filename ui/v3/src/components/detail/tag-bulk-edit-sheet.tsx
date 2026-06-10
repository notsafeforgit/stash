import { useEffect, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useEntityMutation } from "src/core/client";
import { useIntl } from "react-intl";
import { toast } from "sonner";
import * as GQL from "src/core/generated-graphql";
import { Field, FieldGroup, FieldLabel } from "src/components/ui/field";
import { BulkBooleanField } from "src/components/forms/bulk-boolean-field";
import { BulkEntityField } from "src/components/forms/bulk-entity-field";
import type { EntityOption } from "src/components/forms/async-entity-select";
import {
  getIntersectionIds,
  getUnionIds,
  makeBulkUpdateIds,
} from "src/utils/bulkUpdate";
import { BulkEditSheet } from "./bulk-edit-sheet";
import type { BulkApplyTarget } from "src/components/list/list-provider";

// ── Types ─────────────────────────────────────────────────────────────────────

export type TagBulkItem = {
  id: string;
  parents: Array<{ id: string; name: string }>;
  children: Array<{ id: string; name: string }>;
};

interface TagBulkFormValues {
  favorite: boolean | undefined;
  ignore_auto_tag: boolean | undefined;
  parent_ids: GQL.BulkUpdateIds;
  child_ids: GQL.BulkUpdateIds;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildInitialValues(_items: TagBulkItem[]): TagBulkFormValues {
  // Add mode starts empty — see scene-bulk-edit-sheet for rationale.
  return {
    favorite: undefined,
    ignore_auto_tag: undefined,
    parent_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    child_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildEmptyValues(): TagBulkFormValues {
  return {
    favorite: undefined,
    ignore_auto_tag: undefined,
    parent_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    child_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildMutationInput(
  ids: string[],
  v: TagBulkFormValues,
  applyToAll: boolean,
  applyToAllTarget?: BulkApplyTarget,
): GQL.BulkTagUpdateInput {
  const base: GQL.BulkTagUpdateInput = {
    ids: applyToAll ? [] : ids,
    favorite: v.favorite,
    ignore_auto_tag: v.ignore_auto_tag,
    parent_ids: v.parent_ids,
    child_ids: v.child_ids,
  };
  if (applyToAll && applyToAllTarget) {
    base.apply_to_items_matching_filters = true;
    base.find_filter = applyToAllTarget.findFilter;
    base.tag_filter = applyToAllTarget.objectFilter as GQL.TagFilterType;
  }
  return base;
}

// ── Component ─────────────────────────────────────────────────────────────────

interface TagBulkEditSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: TagBulkItem[];
  applyToAllTarget?: BulkApplyTarget;
  totalCount?: number;
  onSaved?: () => void;
}

export function TagBulkEditSheet({
  open,
  onOpenChange,
  items,
  applyToAllTarget,
  totalCount,
  onSaved,
}: TagBulkEditSheetProps) {
  const intl = useIntl();
  const [applyToAll, setApplyToAll] = useState(false);
  const initialValuesRef = useRef(buildInitialValues(items));

  const [bulkUpdateTags, { loading: savingSync }] = useEntityMutation(
    GQL.BulkTagUpdateDocument,
  );
  const [bulkUpdateTagsJob, { loading: savingJob }] = useEntityMutation(
    GQL.BulkTagUpdateJobDocument,
  );
  const saving = savingSync || savingJob;

  const [tagOptions, setTagOptions] = useState<EntityOption[]>([]);
  const [searchTags, { data: tagData, loading: tagLoading }] = useLazyQuery(
    GQL.FindTagsDocument,
  );

  useEffect(() => {
    if (tagData)
      setTagOptions(
        tagData.findTags.tags.map((t) => ({ id: t.id, name: t.name })),
      );
  }, [tagData]);

  const form = useForm({
    defaultValues: buildInitialValues(items),
    onSubmit: async ({ value }) => {
      const ids = items.map((i) => i.id);
      const input = buildMutationInput(
        ids,
        value,
        applyToAll,
        applyToAllTarget,
      );
      if (applyToAll) {
        await bulkUpdateTagsJob({ variables: { input } });
        toast.success(
          intl.formatMessage({
            id: "toast.started_bulk_update",
            defaultMessage: "Bulk update started",
          }),
        );
      } else {
        await bulkUpdateTags({ variables: { input } });
        onSaved?.();
      }
      onOpenChange(false);
    },
  });

  useEffect(() => {
    if (open) {
      const initial = buildInitialValues(items);
      initialValuesRef.current = initial;
      setApplyToAll(false);
      form.reset(initial);
    }
  }, [open, form, items]);

  function handleApplyToAllChange(v: boolean) {
    setApplyToAll(v);
    form.reset(v ? buildEmptyValues() : initialValuesRef.current);
  }

  const existingParentNames: Record<string, string> = {};
  const existingChildNames: Record<string, string> = {};
  for (const item of items) {
    for (const p of item.parents) existingParentNames[p.id] = p.name;
    for (const c of item.children) existingChildNames[c.id] = c.name;
  }
  const parentIdLists = items.map((i) => i.parents.map((p) => p.id));
  const childIdLists = items.map((i) => i.children.map((c) => c.id));
  const parentIntersection = getIntersectionIds(parentIdLists);
  const parentUnion = getUnionIds(parentIdLists);
  const childIntersection = getIntersectionIds(childIdLists);
  const childUnion = getUnionIds(childIdLists);

  return (
    <BulkEditSheet
      open={open}
      onOpenChange={onOpenChange}
      title={intl.formatMessage(
        { id: "dialogs.edit_tags_title", defaultMessage: "Edit {count} tags" },
        { count: items.length },
      )}
      saving={saving}
      onSubmit={form.handleSubmit}
      applyToAllTarget={applyToAllTarget}
      totalCount={totalCount}
      itemCount={items.length}
      applyToAll={applyToAll}
      onApplyToAllChange={handleApplyToAllChange}
    >
      <FieldGroup className="gap-4">
        {/* Parent tags */}
        <form.Field name="parent_ids">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "parent_tags",
                  defaultMessage: "Parent tags",
                })}
              </FieldLabel>
              <BulkEntityField
                value={field.state.value}
                onChange={field.handleChange}
                options={tagOptions}
                onSearch={(q) =>
                  searchTags({ variables: { filter: { q, per_page: 20 } } })
                }
                loading={tagLoading}
                intersectionIds={parentIntersection}
                unionIds={parentUnion}
                existingNames={existingParentNames}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Child tags */}
        <form.Field name="child_ids">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "sub_tags",
                  defaultMessage: "Sub-tags",
                })}
              </FieldLabel>
              <BulkEntityField
                value={field.state.value}
                onChange={field.handleChange}
                options={tagOptions}
                onSearch={(q) =>
                  searchTags({ variables: { filter: { q, per_page: 20 } } })
                }
                loading={tagLoading}
                intersectionIds={childIntersection}
                unionIds={childUnion}
                existingNames={existingChildNames}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Favourite */}
        <form.Field name="favorite">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "favourite",
                  defaultMessage: "Favourite",
                })}
              </FieldLabel>
              <BulkBooleanField
                value={field.state.value}
                onChange={field.handleChange}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Ignore auto-tag */}
        <form.Field name="ignore_auto_tag">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "ignore_auto_tag",
                  defaultMessage: "Ignore auto-tag",
                })}
              </FieldLabel>
              <BulkBooleanField
                value={field.state.value}
                onChange={field.handleChange}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>
      </FieldGroup>
    </BulkEditSheet>
  );
}
