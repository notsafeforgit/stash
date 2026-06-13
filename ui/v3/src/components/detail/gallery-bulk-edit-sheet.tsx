import { useEffect, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useEntityMutation } from "src/core/client";
import { useIntl } from "react-intl";
import { toast } from "sonner";
import * as GQL from "src/core/generated-graphql";
import { Field, FieldGroup, FieldLabel } from "src/components/ui/field";
import { BulkBooleanField } from "src/components/forms/bulk-boolean-field";
import { BulkDateField } from "src/components/forms/bulk-date-field";
import { BulkRatingField } from "src/components/forms/bulk-rating-field";
import { BulkTextField } from "src/components/forms/bulk-text-field";
import { objectTitle } from "src/core/files";
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

export type GalleryBulkItem = {
  id: string;
  tags?: Array<{ id: string; name: string }>;
  performers?: Array<{ id: string; name: string }>;
  scenes?: Array<{
    id: string;
    title?: string | null;
    files?: Array<{ path: string }>;
  }>;
};

interface GalleryBulkFormValues {
  code: string | null | undefined;
  date: string | null | undefined;
  photographer: string | null | undefined;
  rating100: number | null | undefined;
  organized: boolean | undefined;
  studio_id: EntityOption | null | undefined;
  performer_ids: GQL.BulkUpdateIds;
  tag_ids: GQL.BulkUpdateIds;
  scene_ids: GQL.BulkUpdateIds;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildInitialValues(_items: GalleryBulkItem[]): GalleryBulkFormValues {
  // Add mode starts empty — see scene-bulk-edit-sheet for rationale.
  return {
    code: undefined,
    date: undefined,
    photographer: undefined,
    rating100: undefined,
    organized: undefined,
    studio_id: undefined,
    performer_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    scene_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildEmptyValues(): GalleryBulkFormValues {
  return {
    code: undefined,
    date: undefined,
    photographer: undefined,
    rating100: undefined,
    organized: undefined,
    studio_id: undefined,
    performer_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
    scene_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildMutationInput(
  ids: string[],
  v: GalleryBulkFormValues,
  applyToAll: boolean,
  applyToAllTarget?: BulkApplyTarget,
): GQL.BulkGalleryUpdateInput {
  const base: GQL.BulkGalleryUpdateInput = {
    ids: applyToAll ? [] : ids,
    code: v.code,
    date: v.date,
    photographer: v.photographer,
    rating100: v.rating100,
    organized: v.organized,
    studio_id:
      v.studio_id === undefined ? undefined : (v.studio_id?.id ?? null),
    performer_ids: v.performer_ids,
    tag_ids: v.tag_ids,
    scene_ids: v.scene_ids,
  };
  if (applyToAll && applyToAllTarget) {
    base.apply_to_items_matching_filters = true;
    base.find_filter = applyToAllTarget.findFilter;
    base.gallery_filter_ast = applyToAllTarget.filterAST;
  }
  return base;
}

// ── Component ─────────────────────────────────────────────────────────────────

interface GalleryBulkEditSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: GalleryBulkItem[];
  applyToAllTarget?: BulkApplyTarget;
  totalCount?: number;
  onSaved?: () => void;
}

export function GalleryBulkEditSheet({
  open,
  onOpenChange,
  items,
  applyToAllTarget,
  totalCount,
  onSaved,
}: GalleryBulkEditSheetProps) {
  const intl = useIntl();
  const [applyToAll, setApplyToAll] = useState(false);
  const initialValuesRef = useRef(buildInitialValues(items));

  const [bulkUpdateGalleries, { loading: savingSync }] = useEntityMutation(
    GQL.BulkGalleryUpdateDocument,
  );
  const [bulkUpdateGalleriesJob, { loading: savingJob }] = useEntityMutation(
    GQL.BulkGalleryUpdateJobDocument,
  );
  const saving = savingSync || savingJob;

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

  const [sceneOptions, setSceneOptions] = useState<EntityOption[]>([]);
  const [searchScenes, { data: sceneData, loading: sceneLoading }] =
    useLazyQuery(GQL.FindScenesDocument);

  useEffect(() => {
    if (sceneData)
      setSceneOptions(
        sceneData.findScenes.scenes.map((s) => ({
          id: s.id,
          name: objectTitle(s),
        })),
      );
  }, [sceneData]);

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
        await bulkUpdateGalleriesJob({ variables: { input } });
        toast.success(
          intl.formatMessage({
            id: "toast.started_bulk_update",
            defaultMessage: "Bulk update started",
          }),
        );
      } else {
        await bulkUpdateGalleries({ variables: { input } });
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
  const existingPerformerNames: Record<string, string> = {};
  const existingSceneNames: Record<string, string> = {};
  for (const item of items) {
    for (const t of item.tags ?? []) existingTagNames[t.id] = t.name;
    for (const p of item.performers ?? [])
      existingPerformerNames[p.id] = p.name;
    for (const s of item.scenes ?? [])
      existingSceneNames[s.id] = objectTitle(s);
  }
  const tagIdLists = items.map((i) => (i.tags ?? []).map((t) => t.id));
  const performerIdLists = items.map((i) =>
    (i.performers ?? []).map((p) => p.id),
  );
  const sceneIdLists = items.map((i) => (i.scenes ?? []).map((s) => s.id));

  const tagIntersection = getIntersectionIds(tagIdLists);
  const tagUnion = getUnionIds(tagIdLists);
  const performerIntersection = getIntersectionIds(performerIdLists);
  const performerUnion = getUnionIds(performerIdLists);
  const sceneIntersection = getIntersectionIds(sceneIdLists);
  const sceneUnion = getUnionIds(sceneIdLists);

  return (
    <BulkEditSheet
      open={open}
      onOpenChange={onOpenChange}
      title={intl.formatMessage(
        {
          id: "dialogs.edit_galleries_title",
          defaultMessage: "Edit {count} galleries",
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

        {/* Scenes */}
        <form.Field name="scene_ids">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "scenes",
                  defaultMessage: "Scenes",
                })}
              </FieldLabel>
              <BulkEntityField
                value={field.state.value}
                onChange={field.handleChange}
                options={sceneOptions}
                onSearch={(q) =>
                  searchScenes({ variables: { filter: { q, per_page: 20 } } })
                }
                loading={sceneLoading}
                intersectionIds={sceneIntersection}
                unionIds={sceneUnion}
                existingNames={existingSceneNames}
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

        {/* Photographer */}
        <form.Field name="photographer">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "photographer",
                  defaultMessage: "Photographer",
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
