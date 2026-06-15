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
import { BulkDateField } from "src/components/forms/bulk-date-field";
import { BulkRatingField } from "src/components/forms/bulk-rating-field";
import { BulkTextField } from "src/components/forms/bulk-text-field";
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

export type ImageBulkItem = {
  id: string;
  tags?: Array<{ id: string; name: string }>;
  performers?: Array<{ id: string; name: string }>;
  galleries?: Array<{
    id: string;
    title?: string | null;
    files?: Array<{ path: string }>;
    folder?: { path: string; basename?: string | null } | null;
  }>;
};

interface ImageBulkFormValues {
  code: string | null | undefined;
  date: string | null | undefined;
  photographer: string | null | undefined;
  rating100: number | null | undefined;
  organized: boolean | undefined;
  studio_id: EntityOption | null | undefined;
  performer_ids: GQL.BulkUpdateIds;
  tag_ids: GQL.BulkUpdateIds;
  gallery_ids: GQL.BulkUpdateIds;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildInitialValues(_items: ImageBulkItem[]): ImageBulkFormValues {
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
    gallery_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildMutationInput(
  ids: string[],
  v: ImageBulkFormValues,
  applyToAll: boolean,
  applyToAllTarget?: BulkApplyTarget,
): GQL.BulkImageUpdateInput {
  const base: GQL.BulkImageUpdateInput = {
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
    gallery_ids: v.gallery_ids,
  };
  if (applyToAll && applyToAllTarget) {
    base.apply_to_items_matching_filters = true;
    base.find_filter = applyToAllTarget.findFilter;
    base.image_filter_ast = applyToAllTarget.filterAST;
  }
  return base;
}

// ── Component ─────────────────────────────────────────────────────────────────

interface ImageBulkEditSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: ImageBulkItem[];
  applyToAllTarget?: BulkApplyTarget;
  totalCount?: number;
  onSaved?: () => void;
}

export function ImageBulkEditSheet({
  open,
  onOpenChange,
  items,
  applyToAllTarget,
  totalCount,
  onSaved,
}: ImageBulkEditSheetProps) {
  const intl = useIntl();
  const [applyToAll, setApplyToAll] = useState(false);
  const [sheetItems, setSheetItems] = useState(items);
  const [sheetApplyToAllTarget, setSheetApplyToAllTarget] =
    useState(applyToAllTarget);
  const [sheetTotalCount, setSheetTotalCount] = useState(totalCount);
  const applyToAllRef = useRef(applyToAll);
  const itemsRef = useRef(items);
  const applyToAllTargetRef = useRef(applyToAllTarget);
  const onSavedRef = useRef(onSaved);
  applyToAllRef.current = applyToAll;
  onSavedRef.current = onSaved;

  const [bulkUpdateImages, { loading: savingSync }] = useEntityMutation(
    GQL.BulkImageUpdateDocument,
  );
  const [bulkUpdateImagesJob, { loading: savingJob }] = useEntityMutation(
    GQL.BulkImageUpdateJobDocument,
  );
  const saving = savingSync || savingJob;
  const [setDateFromMTime, { loading: settingDate }] = useEntityMutation(
    GQL.ImagesSetDateFromFileMTimeDocument,
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

  const form = useForm({
    defaultValues: buildInitialValues(items),
    onSubmit: async ({ value }) => {
      const currentApplyToAll = applyToAllRef.current;
      const currentApplyToAllTarget = applyToAllTargetRef.current;
      const ids = itemsRef.current.map((i) => i.id);
      const input = buildMutationInput(
        ids,
        value,
        currentApplyToAll,
        currentApplyToAllTarget,
      );
      if (currentApplyToAll) {
        await bulkUpdateImagesJob({ variables: { input } });
        toast.success(
          intl.formatMessage({
            id: "toast.started_bulk_update",
            defaultMessage: "Bulk update started",
          }),
        );
      } else {
        await bulkUpdateImages({ variables: { input } });
        onSavedRef.current?.();
      }
      onOpenChange(false);
    },
  });
  const wasOpenRef = useRef(false);

  useEffect(() => {
    if (open && !wasOpenRef.current) {
      const initial = buildInitialValues(items);
      itemsRef.current = items;
      applyToAllTargetRef.current = applyToAllTarget;
      setSheetItems(items);
      setSheetApplyToAllTarget(applyToAllTarget);
      setSheetTotalCount(totalCount);
      applyToAllRef.current = false;
      setApplyToAll(false);
      form.reset(initial);
    }
    wasOpenRef.current = open;
  }, [open, form, items, applyToAllTarget, totalCount]);

  function handleApplyToAllChange(v: boolean) {
    applyToAllRef.current = v;
    setApplyToAll(v);
  }

  // Per-item action: each image's date is set from its primary file's mtime.
  async function handleSetDateFromFileMTime() {
    const currentApplyToAll = applyToAllRef.current;
    const currentApplyToAllTarget = applyToAllTargetRef.current;
    const input: GQL.ImagesSetDateFromFileMTimeInput =
      currentApplyToAll && currentApplyToAllTarget
        ? {
            apply_to_items_matching_filters: true,
            find_filter: currentApplyToAllTarget.findFilter,
            image_filter_ast: currentApplyToAllTarget.filterAST,
          }
        : { ids: itemsRef.current.map((i) => i.id) };
    await setDateFromMTime({ variables: { input } });
    if (currentApplyToAll) {
      toast.success(
        intl.formatMessage({
          id: "toast.started_bulk_update",
          defaultMessage: "Bulk update started",
        }),
      );
    } else {
      onSavedRef.current?.();
    }
    onOpenChange(false);
  }

  const existingTagNames: Record<string, string> = {};
  const existingPerformerNames: Record<string, string> = {};
  const existingGalleryNames: Record<string, string> = {};
  for (const item of sheetItems) {
    for (const t of item.tags ?? []) existingTagNames[t.id] = t.name;
    for (const p of item.performers ?? [])
      existingPerformerNames[p.id] = p.name;
    for (const g of item.galleries ?? [])
      existingGalleryNames[g.id] = galleryLabel(g);
  }
  const tagIdLists = sheetItems.map((i) => (i.tags ?? []).map((t) => t.id));
  const performerIdLists = sheetItems.map((i) =>
    (i.performers ?? []).map((p) => p.id),
  );
  const galleryIdLists = sheetItems.map((i) =>
    (i.galleries ?? []).map((g) => g.id),
  );

  const tagIntersection = getIntersectionIds(tagIdLists);
  const tagUnion = getUnionIds(tagIdLists);
  const performerIntersection = getIntersectionIds(performerIdLists);
  const performerUnion = getUnionIds(performerIdLists);
  const galleryIntersection = getIntersectionIds(galleryIdLists);
  const galleryUnion = getUnionIds(galleryIdLists);

  return (
    <BulkEditSheet
      open={open}
      onOpenChange={onOpenChange}
      title={intl.formatMessage(
        {
          id: "dialogs.edit_images_title",
          defaultMessage: "Edit {count} images",
        },
        { count: sheetItems.length },
      )}
      saving={saving}
      onSubmit={form.handleSubmit}
      applyToAllTarget={sheetApplyToAllTarget}
      totalCount={sheetTotalCount}
      itemCount={sheetItems.length}
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
                    "Set each image's date from its file's modification time",
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
