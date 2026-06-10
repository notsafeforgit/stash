import { useEffect, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useEntityMutation } from "src/core/client";
import { useIntl } from "react-intl";
import { toast } from "sonner";
import * as GQL from "src/core/generated-graphql";
import { galleryLabel } from "src/lib/gallery-utils";
import { Field, FieldGroup, FieldLabel } from "src/components/ui/field";
import { Button } from "src/components/ui/button";
import { FileClock } from "lucide-react";
import { BulkBooleanField } from "src/components/forms/bulk-boolean-field";
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

export type SceneBulkItem = {
  id: string;
  title?: string | null;
  date?: string | null;
  paths?: { screenshot?: string | null } | null;
  studio?: { id: string; name: string } | null;
  tags?: Array<{ id: string; name: string }>;
  performers?: Array<{ id: string; name: string }>;
  galleries?: Array<{
    id: string;
    title?: string | null;
    files?: Array<{ path: string }>;
    folder?: { path: string; basename?: string | null } | null;
  }>;
  groups?: Array<{ group: { id: string; name: string } }>;
};

interface SceneBulkFormValues {
  code: string | null | undefined;
  date: string | null | undefined;
  director: string | null | undefined;
  rating100: number | null | undefined;
  organized: boolean | undefined;
  studio_id: EntityOption | null | undefined;
  performer_ids: GQL.BulkUpdateIds;
  tag_ids: GQL.BulkUpdateIds;
  gallery_ids: GQL.BulkUpdateIds;
  group_ids: GQL.BulkUpdateIds;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildInitialValues(_items: SceneBulkItem[]): SceneBulkFormValues {
  // Add mode starts empty — the field shows what to add, not what's
  // already on the items. Switching to Set mode in BulkEntityField seeds
  // it with the aggregated existing IDs (passed via `existingIds` below).
  return {
    code: undefined,
    date: undefined,
    director: undefined,
    rating100: undefined,
    organized: undefined,
    studio_id: undefined,
    performer_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    gallery_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    group_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildEmptyValues(): SceneBulkFormValues {
  return {
    code: undefined,
    date: undefined,
    director: undefined,
    rating100: undefined,
    organized: undefined,
    studio_id: undefined,
    performer_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    gallery_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    group_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildMutationInput(
  ids: string[],
  v: SceneBulkFormValues,
  applyToAll: boolean,
  applyToAllTarget?: BulkApplyTarget,
): GQL.BulkSceneUpdateInput {
  const base: GQL.BulkSceneUpdateInput = {
    ids: applyToAll ? [] : ids,
    code: v.code,
    date: v.date,
    director: v.director,
    rating100: v.rating100,
    organized: v.organized,
    studio_id:
      v.studio_id === undefined ? undefined : (v.studio_id?.id ?? null),
    performer_ids: v.performer_ids,
    tag_ids: v.tag_ids,
    gallery_ids: v.gallery_ids,
    group_ids: v.group_ids,
  };
  if (applyToAll && applyToAllTarget) {
    base.apply_to_items_matching_filters = true;
    base.find_filter = applyToAllTarget.findFilter;
    base.scene_filter = applyToAllTarget.objectFilter as GQL.SceneFilterType;
  }
  return base;
}

// ── Component ─────────────────────────────────────────────────────────────────

interface SceneBulkEditSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: SceneBulkItem[];
  applyToAllTarget?: BulkApplyTarget;
  totalCount?: number;
  onSaved?: () => void;
}

export function SceneBulkEditSheet({
  open,
  onOpenChange,
  items,
  applyToAllTarget,
  totalCount,
  onSaved,
}: SceneBulkEditSheetProps) {
  const intl = useIntl();
  const [applyToAll, setApplyToAll] = useState(false);
  const initialValuesRef = useRef(buildInitialValues(items));

  const [bulkUpdateScenes, { loading: savingSync }] = useEntityMutation(
    GQL.BulkSceneUpdateDocument,
  );
  const [bulkUpdateScenesJob, { loading: savingJob }] = useEntityMutation(
    GQL.BulkSceneUpdateJobDocument,
  );
  const saving = savingSync || savingJob;
  const [setDateFromMTime, { loading: settingDate }] = useEntityMutation(
    GQL.ScenesSetDateFromFileMTimeDocument,
  );

  const [tagOptions, setTagOptions] = useState<EntityOption[]>([]);
  const [searchTags, { data: tagData, loading: tagLoading }] = useLazyQuery(
    GQL.FindTagsDocument,
  );

  const [performerOptions, setPerformerOptions] = useState<EntityOption[]>([]);
  const [searchPerformers, { data: performerData, loading: performerLoading }] =
    useLazyQuery(GQL.FindPerformersDocument);

  const [studioOptions, setStudioOptions] = useState<EntityOption[]>([]);
  const [searchStudios, { data: studioData, loading: studioLoading }] =
    useLazyQuery(GQL.FindStudiosDocument);

  const [galleryOptions, setGalleryOptions] = useState<EntityOption[]>([]);
  const [searchGalleries, { data: galleryData, loading: galleryLoading }] =
    useLazyQuery(GQL.FindGalleriesDocument);

  const [groupOptions, setGroupOptions] = useState<EntityOption[]>([]);
  const [searchGroups, { data: groupData, loading: groupLoading }] =
    useLazyQuery(GQL.FindGroupsDocument);

  useEffect(() => {
    if (tagData)
      setTagOptions(
        tagData.findTags.tags.map((t) => ({ id: t.id, name: t.name })),
      );
  }, [tagData]);
  useEffect(() => {
    if (performerData)
      setPerformerOptions(
        performerData.findPerformers.performers.map((p) => ({
          id: p.id,
          name: p.name,
        })),
      );
  }, [performerData]);
  useEffect(() => {
    if (studioData)
      setStudioOptions(
        studioData.findStudios.studios.map((s) => ({ id: s.id, name: s.name })),
      );
  }, [studioData]);
  useEffect(() => {
    if (galleryData)
      setGalleryOptions(
        galleryData.findGalleries.galleries.map((g) => ({
          id: g.id,
          name: galleryLabel(g),
        })),
      );
  }, [galleryData]);
  useEffect(() => {
    if (groupData)
      setGroupOptions(
        groupData.findGroups.groups.map((g) => ({ id: g.id, name: g.name })),
      );
  }, [groupData]);

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
        await bulkUpdateScenesJob({ variables: { input } });
        toast.success(
          intl.formatMessage({
            id: "toast.started_bulk_update",
            defaultMessage: "Bulk update started",
          }),
        );
      } else {
        await bulkUpdateScenes({ variables: { input } });
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

  // Per-item action: each scene's date is set from its primary file's mtime.
  // Honours the same "explicit ids vs apply-to-filter" toggle as the bulk
  // update form.
  async function handleSetDateFromFileMTime() {
    const input: GQL.ScenesSetDateFromFileMTimeInput =
      applyToAll && applyToAllTarget
        ? {
            apply_to_items_matching_filters: true,
            find_filter: applyToAllTarget.findFilter,
            scene_filter: applyToAllTarget.objectFilter as GQL.SceneFilterType,
          }
        : { ids: items.map((i) => i.id) };
    await setDateFromMTime({ variables: { input } });
    if (applyToAll) {
      toast.success(
        intl.formatMessage({
          id: "toast.started_bulk_update",
          defaultMessage: "Bulk update started",
        }),
      );
    } else {
      onSaved?.();
    }
    onOpenChange(false);
  }

  const existingTagNames: Record<string, string> = {};
  const existingPerformerNames: Record<string, string> = {};
  const existingGalleryNames: Record<string, string> = {};
  const existingGroupNames: Record<string, string> = {};
  for (const item of items) {
    for (const t of item.tags ?? []) existingTagNames[t.id] = t.name;
    for (const p of item.performers ?? [])
      existingPerformerNames[p.id] = p.name;
    for (const g of item.galleries ?? [])
      existingGalleryNames[g.id] = galleryLabel(g);
    for (const g of item.groups ?? [])
      existingGroupNames[g.group.id] = g.group.name;
  }
  const tagIdLists = items.map((i) => (i.tags ?? []).map((t) => t.id));
  const performerIdLists = items.map((i) =>
    (i.performers ?? []).map((p) => p.id),
  );
  const galleryIdLists = items.map((i) => (i.galleries ?? []).map((g) => g.id));
  const groupIdLists = items.map((i) =>
    (i.groups ?? []).map((g) => g.group.id),
  );

  const tagIntersection = getIntersectionIds(tagIdLists);
  const tagUnion = getUnionIds(tagIdLists);
  const performerIntersection = getIntersectionIds(performerIdLists);
  const performerUnion = getUnionIds(performerIdLists);
  const galleryIntersection = getIntersectionIds(galleryIdLists);
  const galleryUnion = getUnionIds(galleryIdLists);
  const groupIntersection = getIntersectionIds(groupIdLists);
  const groupUnion = getUnionIds(groupIdLists);

  return (
    <BulkEditSheet
      open={open}
      onOpenChange={onOpenChange}
      title={intl.formatMessage(
        {
          id: "dialogs.edit_scenes_title",
          defaultMessage: "Edit {count} scenes",
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
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={saving || settingDate}
                onClick={handleSetDateFromFileMTime}
                className="self-start"
                title={intl.formatMessage({
                  id: "actions.set_each_date_from_file_mtime",
                  defaultMessage:
                    "Set each scene's date from its file's modification time",
                })}
              >
                <FileClock className="size-4" />
                {intl.formatMessage({
                  id: "actions.set_each_from_file_mtime",
                  defaultMessage: "Set each from file mtime",
                })}
              </Button>
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

        {/* Performers */}
        <form.Field name="performer_ids">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "performers",
                  defaultMessage: "Performers",
                })}
              </FieldLabel>
              <BulkEntityField
                value={field.state.value}
                onChange={field.handleChange}
                options={performerOptions}
                onSearch={(q) =>
                  searchPerformers({
                    variables: { filter: { q, per_page: 20 } },
                  })
                }
                loading={performerLoading}
                intersectionIds={performerIntersection}
                unionIds={performerUnion}
                existingNames={existingPerformerNames}
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

        {/* Galleries */}
        <form.Field name="gallery_ids">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "galleries",
                  defaultMessage: "Galleries",
                })}
              </FieldLabel>
              <BulkEntityField
                value={field.state.value}
                onChange={field.handleChange}
                options={galleryOptions}
                onSearch={(q) =>
                  searchGalleries({
                    variables: { filter: { q, per_page: 20 } },
                  })
                }
                loading={galleryLoading}
                intersectionIds={galleryIntersection}
                unionIds={galleryUnion}
                existingNames={existingGalleryNames}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Groups */}
        <form.Field name="group_ids">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({ id: "groups", defaultMessage: "Groups" })}
              </FieldLabel>
              <BulkEntityField
                value={field.state.value}
                onChange={field.handleChange}
                options={groupOptions}
                onSearch={(q) =>
                  searchGroups({ variables: { filter: { q, per_page: 20 } } })
                }
                loading={groupLoading}
                intersectionIds={groupIntersection}
                unionIds={groupUnion}
                existingNames={existingGroupNames}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Code */}
        <form.Field name="code">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "scene_code",
                  defaultMessage: "Studio Code",
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
