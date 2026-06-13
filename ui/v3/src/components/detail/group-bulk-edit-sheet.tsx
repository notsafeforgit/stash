import { useEffect, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useEntityMutation } from "src/core/client";
import { useIntl } from "react-intl";
import { toast } from "sonner";
import * as GQL from "src/core/generated-graphql";
import { Field, FieldGroup, FieldLabel } from "src/components/ui/field";
import { BulkTextField } from "src/components/forms/bulk-text-field";
import { BulkDateField } from "src/components/forms/bulk-date-field";
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

export type GroupBulkItem = {
  id: string;
  tags?: Array<{ id: string; name: string }>;
};

interface GroupBulkFormValues {
  date: string | null | undefined;
  director: string | null | undefined;
  rating100: number | null | undefined;
  studio_id: EntityOption | null | undefined;
  tag_ids: GQL.BulkUpdateIds;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildInitialValues(_items: GroupBulkItem[]): GroupBulkFormValues {
  // Add mode starts empty — see scene-bulk-edit-sheet for rationale.
  return {
    date: undefined,
    director: undefined,
    rating100: undefined,
    studio_id: undefined,
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildEmptyValues(): GroupBulkFormValues {
  return {
    date: undefined,
    director: undefined,
    rating100: undefined,
    studio_id: undefined,
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildMutationInput(
  ids: string[],
  v: GroupBulkFormValues,
  applyToAll: boolean,
  applyToAllTarget?: BulkApplyTarget,
): GQL.BulkGroupUpdateInput {
  const base: GQL.BulkGroupUpdateInput = {
    ids: applyToAll ? [] : ids,
    date: v.date,
    director: v.director,
    rating100: v.rating100,
    studio_id:
      v.studio_id === undefined ? undefined : (v.studio_id?.id ?? null),
    tag_ids: v.tag_ids,
  };
  if (applyToAll && applyToAllTarget) {
    base.apply_to_items_matching_filters = true;
    base.find_filter = applyToAllTarget.findFilter;
    base.group_filter_ast = applyToAllTarget.filterAST;
  }
  return base;
}

// ── Component ─────────────────────────────────────────────────────────────────

interface GroupBulkEditSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: GroupBulkItem[];
  applyToAllTarget?: BulkApplyTarget;
  totalCount?: number;
  onSaved?: () => void;
}

export function GroupBulkEditSheet({
  open,
  onOpenChange,
  items,
  applyToAllTarget,
  totalCount,
  onSaved,
}: GroupBulkEditSheetProps) {
  const intl = useIntl();
  const [applyToAll, setApplyToAll] = useState(false);
  const initialValuesRef = useRef(buildInitialValues(items));

  const [bulkUpdateGroups, { loading: savingSync }] = useEntityMutation(
    GQL.BulkGroupUpdateDocument,
  );
  const [bulkUpdateGroupsJob, { loading: savingJob }] = useEntityMutation(
    GQL.BulkGroupUpdateJobDocument,
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
        await bulkUpdateGroupsJob({ variables: { input } });
        toast.success(
          intl.formatMessage({
            id: "toast.started_bulk_update",
            defaultMessage: "Bulk update started",
          }),
        );
      } else {
        await bulkUpdateGroups({ variables: { input } });
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
    for (const t of item.tags ?? []) existingTagNames[t.id] = t.name;
  }
  const tagIdLists = items.map((i) => (i.tags ?? []).map((t) => t.id));
  const tagIntersection = getIntersectionIds(tagIdLists);
  const tagUnion = getUnionIds(tagIdLists);

  return (
    <BulkEditSheet
      open={open}
      onOpenChange={onOpenChange}
      title={intl.formatMessage(
        {
          id: "dialogs.edit_groups_title",
          defaultMessage: "Edit {count} groups",
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
        {/* Date */}
        <form.Field name="date">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({ id: "date", defaultMessage: "Date" })}
              </FieldLabel>
              <BulkDateField
                value={field.state.value}
                onChange={field.handleChange}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Studio */}
        <form.Field name="studio_id">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({ id: "studio", defaultMessage: "Studio" })}
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

        {/* Director */}
        <form.Field name="director">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "director",
                  defaultMessage: "Director",
                })}
              </FieldLabel>
              <BulkTextField
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
