import { useEffect, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useEntityMutation } from "src/core/client";
import { useIntl } from "react-intl";
import { toast } from "sonner";
import * as GQL from "src/core/generated-graphql";
import { Field, FieldGroup, FieldLabel } from "src/components/ui/field";
import { BulkBooleanField } from "src/components/forms/bulk-boolean-field";
import { BulkRatingField } from "src/components/forms/bulk-rating-field";
import { BulkEntityField } from "src/components/forms/bulk-entity-field";
import { BulkEntitySingleField } from "src/components/forms/bulk-entity-single-field";
import type { EntityOption } from "src/components/forms/async-entity-select";
import {
  getIntersectionIds,
  getUnionIds,
  makeBulkUpdateIds,
} from "src/utils/bulkUpdate";
import { BulkEditSheet } from "./bulk-edit-sheet";
import type { BulkApplyTarget } from "src/components/list/list-provider";

// ── Types ─────────────────────────────────────────────────────────────────────

export type StudioBulkItem = {
  id: string;
  tags: Array<{ id: string; name: string }>;
};

interface StudioBulkFormValues {
  favorite: boolean | undefined;
  ignore_auto_tag: boolean | undefined;
  organized: boolean | undefined;
  rating100: number | null | undefined;
  tag_ids: GQL.BulkUpdateIds;
  parent_id: EntityOption | null | undefined;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildInitialValues(_items: StudioBulkItem[]): StudioBulkFormValues {
  // Add mode starts empty — see scene-bulk-edit-sheet for rationale.
  return {
    favorite: undefined,
    ignore_auto_tag: undefined,
    organized: undefined,
    rating100: undefined,
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    parent_id: undefined,
  };
}

function buildEmptyValues(): StudioBulkFormValues {
  return {
    favorite: undefined,
    ignore_auto_tag: undefined,
    organized: undefined,
    rating100: undefined,
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    parent_id: undefined,
  };
}

function buildMutationInput(
  ids: string[],
  v: StudioBulkFormValues,
  applyToAll: boolean,
  applyToAllTarget?: BulkApplyTarget,
): GQL.BulkStudioUpdateInput {
  // ids is required (non-optional) on BulkStudioUpdateInput
  const base: GQL.BulkStudioUpdateInput = {
    ids: applyToAll ? [] : ids,
    favorite: v.favorite,
    ignore_auto_tag: v.ignore_auto_tag,
    organized: v.organized,
    rating100: v.rating100,
    tag_ids: v.tag_ids,
    parent_id:
      v.parent_id === undefined ? undefined : (v.parent_id?.id ?? null),
  };
  if (applyToAll && applyToAllTarget) {
    base.apply_to_items_matching_filters = true;
    base.find_filter = applyToAllTarget.findFilter;
    base.studio_filter_ast = applyToAllTarget.filterAST;
  }
  return base;
}

// ── Component ─────────────────────────────────────────────────────────────────

interface StudioBulkEditSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: StudioBulkItem[];
  applyToAllTarget?: BulkApplyTarget;
  totalCount?: number;
  onSaved?: () => void;
}

export function StudioBulkEditSheet({
  open,
  onOpenChange,
  items,
  applyToAllTarget,
  totalCount,
  onSaved,
}: StudioBulkEditSheetProps) {
  const intl = useIntl();
  const [applyToAll, setApplyToAll] = useState(false);
  const initialValuesRef = useRef(buildInitialValues(items));

  const [bulkUpdateStudios, { loading: savingSync }] = useEntityMutation(
    GQL.BulkStudioUpdateDocument,
  );
  const [bulkUpdateStudiosJob, { loading: savingJob }] = useEntityMutation(
    GQL.BulkStudioUpdateJobDocument,
  );
  const saving = savingSync || savingJob;

  const [tagOptions, setTagOptions] = useState<EntityOption[]>([]);
  const [searchTags, { data: tagData, loading: tagLoading }] = useLazyQuery(
    GQL.FindTagsDocument,
  );

  const [studioOptions, setStudioOptions] = useState<EntityOption[]>([]);
  const [searchStudios, { data: studioData, loading: studioLoading }] =
    useLazyQuery(GQL.FindStudiosDocument);

  useEffect(() => {
    if (tagData)
      setTagOptions(
        tagData.findTags.tags.map((t) => ({ id: t.id, name: t.name })),
      );
  }, [tagData]);
  useEffect(() => {
    if (studioData)
      setStudioOptions(
        studioData.findStudios.studios.map((s) => ({ id: s.id, name: s.name })),
      );
  }, [studioData]);

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
        await bulkUpdateStudiosJob({ variables: { input } });
        toast.success(
          intl.formatMessage({
            id: "toast.started_bulk_update",
            defaultMessage: "Bulk update started",
          }),
        );
      } else {
        await bulkUpdateStudios({ variables: { input } });
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

  const existingTagNames: Record<string, string> = {};
  for (const item of items) {
    for (const t of item.tags) existingTagNames[t.id] = t.name;
  }
  const tagIdLists = items.map((i) => i.tags.map((t) => t.id));
  const tagIntersection = getIntersectionIds(tagIdLists);
  const tagUnion = getUnionIds(tagIdLists);

  return (
    <BulkEditSheet
      open={open}
      onOpenChange={onOpenChange}
      title={intl.formatMessage(
        {
          id: "dialogs.edit_studios_title",
          defaultMessage: "Edit {count} studios",
        },
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
        {/* Parent studio */}
        <form.Field name="parent_id">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "parent_studio",
                  defaultMessage: "Parent studio",
                })}
              </FieldLabel>
              <BulkEntitySingleField
                value={field.state.value}
                onChange={field.handleChange}
                options={studioOptions}
                onSearch={(q) =>
                  searchStudios({ variables: { filter: { q, per_page: 20 } } })
                }
                loading={studioLoading}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Tags */}
        <form.Field name="tag_ids">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
              </FieldLabel>
              <BulkEntityField
                value={field.state.value}
                onChange={field.handleChange}
                options={tagOptions}
                onSearch={(q) =>
                  searchTags({ variables: { filter: { q, per_page: 20 } } })
                }
                loading={tagLoading}
                intersectionIds={tagIntersection}
                unionIds={tagUnion}
                existingNames={existingTagNames}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Rating */}
        <form.Field name="rating100">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({ id: "rating", defaultMessage: "Rating" })}
              </FieldLabel>
              <BulkRatingField
                value={field.state.value}
                onChange={field.handleChange}
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

        {/* Organised */}
        <form.Field name="organized">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "organized",
                  defaultMessage: "Organised",
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
