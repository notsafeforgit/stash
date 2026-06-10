import { useEffect, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useSmartBack } from "src/hooks/use-smart-back";
import { useIntl } from "react-intl";
import { Save, RotateCcw, Trash2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import { galleryLabel } from "src/lib/gallery-utils";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Textarea } from "src/components/ui/textarea";
import { Switch } from "src/components/ui/switch";
import { RatingSystem } from "src/components/ui/rating-system";
import { Field, FieldLabel, FieldGroup } from "src/components/ui/field";
import {
  type EntityOption,
  EntitySingleSelect,
  EntityMultiSelect,
} from "src/components/forms/async-entity-select";
import { UrlListField } from "src/components/forms/url-list-field";
import { DatePicker } from "src/components/ui/date-picker";
import { DeleteDialog } from "src/components/detail/delete-dialog";
import {
  CustomFieldsField,
  type CustomFieldMap,
  customFieldsUpdateInput,
} from "src/components/forms/custom-fields-field";

// ── Types ─────────────────────────────────────────────────────────────────────

type GalleryData = NonNullable<GQL.FindGalleryQuery["findGallery"]>;

interface GalleryFormValues {
  title: string;
  code: string;
  date: string;
  details: string;
  photographer: string;
  urls: string[];
  rating100: number | null;
  organized: boolean;
  studio: EntityOption | null;
  performers: EntityOption[];
  tags: EntityOption[];
  scenes: EntityOption[];
  custom_fields: CustomFieldMap;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function galleryToFormValues(g: GalleryData): GalleryFormValues {
  return {
    title: g.title ?? "",
    code: g.code ?? "",
    date: g.date ?? "",
    details: g.details ?? "",
    photographer: g.photographer ?? "",
    urls: g.urls ?? [],
    rating100: g.rating100 ?? null,
    organized: g.organized,
    studio: g.studio ? { id: g.studio.id, name: g.studio.name } : null,
    performers: g.performers.map((p) => ({ id: p.id, name: p.name })),
    tags: g.tags.map((t) => ({ id: t.id, name: t.name })),
    scenes: g.scenes.map((s) => ({ id: s.id, name: objectTitle(s) || s.id })),
    custom_fields: g.custom_fields ?? {},
  };
}

function formValuesToInput(
  id: string,
  v: GalleryFormValues,
): GQL.GalleryUpdateInput {
  return {
    id,
    title: v.title || null,
    code: v.code || null,
    date: v.date || null,
    details: v.details || null,
    photographer: v.photographer || null,
    urls: v.urls.filter(Boolean),
    rating100: v.rating100,
    organized: v.organized,
    studio_id: v.studio?.id ?? null,
    performer_ids: v.performers.map((p) => p.id),
    tag_ids: v.tags.map((t) => t.id),
    scene_ids: v.scenes.map((s) => s.id),
    custom_fields: customFieldsUpdateInput(v.custom_fields),
  };
}

// ── Component ─────────────────────────────────────────────────────────────────

interface GalleryEditFormProps {
  gallery: GalleryData;
  onSaved?: () => void;
  /** Called after deletion — use to close a containing sheet/dialog instead of navigating back. */
  onDeleted?: () => void;
}

export function GalleryEditForm({
  gallery,
  onSaved,
  onDeleted,
}: GalleryEditFormProps) {
  const intl = useIntl();
  const goBack = useSmartBack("/galleries");

  const [updateGallery, { loading: saving }] = useEntityMutation(
    GQL.GalleryUpdateDocument,
  );
  const [destroyGallery, { loading: deleting }] = useEntityMutation(
    GQL.GalleryDestroyDocument,
  );

  // Studio search
  const [studioOptions, setStudioOptions] = useState<EntityOption[]>([]);
  const [searchStudios, { data: studioData, loading: studioLoading }] =
    useLazyQuery(GQL.FindStudiosDocument);

  useEffect(() => {
    if (studioData) {
      setStudioOptions(
        studioData.findStudios.studios.map((s) => ({ id: s.id, name: s.name })),
      );
    }
  }, [studioData]);

  // Performer search
  const [performerOptions, setPerformerOptions] = useState<EntityOption[]>([]);
  const [searchPerformers, { data: performerData, loading: performerLoading }] =
    useLazyQuery(GQL.FindPerformersDocument);

  useEffect(() => {
    if (performerData) {
      setPerformerOptions(
        performerData.findPerformers.performers.map((p) => ({
          id: p.id,
          name: p.name,
        })),
      );
    }
  }, [performerData]);

  // Tag search
  const [tagOptions, setTagOptions] = useState<EntityOption[]>([]);
  const [searchTags, { data: tagData, loading: tagLoading }] = useLazyQuery(
    GQL.FindTagsDocument,
  );

  useEffect(() => {
    if (tagData) {
      setTagOptions(
        tagData.findTags.tags.map((t) => ({ id: t.id, name: t.name })),
      );
    }
  }, [tagData]);

  // Scene search
  const [sceneOptions, setSceneOptions] = useState<EntityOption[]>([]);
  const [searchScenes, { data: sceneData, loading: sceneLoading }] =
    useLazyQuery(GQL.FindScenesDocument);

  useEffect(() => {
    if (sceneData) {
      setSceneOptions(
        sceneData.findScenes.scenes.map((s) => ({
          id: s.id,
          name: objectTitle(s) || s.id,
        })),
      );
    }
  }, [sceneData]);

  const form = useForm({
    defaultValues: galleryToFormValues(gallery),
    onSubmit: async ({ value, formApi }) => {
      await updateGallery({
        variables: { input: formValuesToInput(gallery.id, value) },
      });
      formApi.reset(value);
      onSaved?.();
    },
  });

  const busy = saving || deleting;

  const [deleteOpen, setDeleteOpen] = useState(false);

  async function handleConfirmedDelete({
    deleteFile,
    deleteGenerated,
  }: {
    deleteFile: boolean;
    deleteGenerated: boolean;
  }) {
    await destroyGallery({
      variables: {
        ids: [gallery.id],
        delete_file: deleteFile,
        delete_generated: deleteGenerated,
      },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Gallery",
          listFieldName: "findGalleries",
          itemsField: "galleries",
          ids: [gallery.id],
        });
      },
    });
    if (onDeleted) onDeleted();
    else goBack();
  }

  return (
    <form
      className="flex flex-col h-full min-w-0 overflow-x-hidden"
      onSubmit={(e) => {
        e.preventDefault();
        form.handleSubmit();
      }}
    >
      <div className="flex-1 min-h-0 overflow-y-auto">
        <FieldGroup className="gap-4 px-3 pb-3">
          {/* Title */}
          <form.Field name="title">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({ id: "title", defaultMessage: "Title" })}
                </FieldLabel>
                <Input
                  id={field.name}
                  name={field.name}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Date */}
          <form.Field name="date">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({ id: "date", defaultMessage: "Date" })}
                </FieldLabel>
                <DatePicker
                  value={field.state.value}
                  onChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Details */}
          <form.Field name="details">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "details",
                    defaultMessage: "Details",
                  })}
                </FieldLabel>
                <Textarea
                  id={field.name}
                  name={field.name}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  disabled={busy}
                  rows={4}
                />
              </Field>
            )}
          </form.Field>

          {/* URLs */}
          <form.Field name="urls">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({ id: "urls", defaultMessage: "URLs" })}
                </FieldLabel>
                <UrlListField
                  value={field.state.value}
                  onChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Studio */}
          <form.Field name="studio">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "studio",
                    defaultMessage: "Studio",
                  })}
                </FieldLabel>
                <EntitySingleSelect
                  value={field.state.value}
                  onChange={field.handleChange}
                  options={studioOptions}
                  onSearch={(q) =>
                    searchStudios({
                      variables: { filter: { q, per_page: 20 } },
                    })
                  }
                  loading={studioLoading}
                  placeholder={intl.formatMessage({
                    id: "actions.search",
                    defaultMessage: "Search…",
                  })}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Performers */}
          <form.Field name="performers">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "performers",
                    defaultMessage: "Performers",
                  })}
                </FieldLabel>
                <EntityMultiSelect
                  value={field.state.value}
                  onChange={field.handleChange}
                  options={performerOptions}
                  onSearch={(q) =>
                    searchPerformers({
                      variables: { filter: { q, per_page: 20 } },
                    })
                  }
                  loading={performerLoading}
                  placeholder={intl.formatMessage({
                    id: "actions.search",
                    defaultMessage: "Search…",
                  })}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Tags */}
          <form.Field name="tags">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
                </FieldLabel>
                <EntityMultiSelect
                  value={field.state.value}
                  onChange={field.handleChange}
                  options={tagOptions}
                  onSearch={(q) =>
                    searchTags({ variables: { filter: { q, per_page: 20 } } })
                  }
                  loading={tagLoading}
                  placeholder={intl.formatMessage({
                    id: "actions.search",
                    defaultMessage: "Search…",
                  })}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Scenes */}
          <form.Field name="scenes">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "scenes",
                    defaultMessage: "Scenes",
                  })}
                </FieldLabel>
                <EntityMultiSelect
                  value={field.state.value}
                  onChange={field.handleChange}
                  options={sceneOptions}
                  onSearch={(q) =>
                    searchScenes({ variables: { filter: { q, per_page: 20 } } })
                  }
                  loading={sceneLoading}
                  placeholder={intl.formatMessage({
                    id: "actions.search",
                    defaultMessage: "Search…",
                  })}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Rating */}
          <form.Field name="rating100">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "rating",
                    defaultMessage: "Rating",
                  })}
                </FieldLabel>
                <RatingSystem
                  value={field.state.value}
                  onSetRating={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Organized */}
          <form.Field name="organized">
            {(field) => (
              <Field className="flex-row items-center justify-between">
                <FieldLabel>
                  {intl.formatMessage({
                    id: "organized",
                    defaultMessage: "Organized",
                  })}
                </FieldLabel>
                <Switch
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Code */}
          <form.Field name="code">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "scene_code",
                    defaultMessage: "Scene code",
                  })}
                </FieldLabel>
                <Input
                  id={field.name}
                  name={field.name}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Photographer */}
          <form.Field name="photographer">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "photographer",
                    defaultMessage: "Photographer",
                  })}
                </FieldLabel>
                <Input
                  id={field.name}
                  name={field.name}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Custom fields */}
          <form.Field name="custom_fields">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({ id: "custom_fields.title" })}
                </FieldLabel>
                <CustomFieldsField
                  value={field.state.value}
                  onChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>
        </FieldGroup>
      </div>

      {/* Action bar — flex sibling pinned to the form's bottom. */}
      <div className="shrink-0 flex items-center justify-between gap-2 border-t border-border bg-background/95 backdrop-blur-sm h-10 px-3">
        <div className="flex items-center gap-2">
          <form.Subscribe
            selector={(s) => ({
              isSubmitting: s.isSubmitting,
              isDirty: s.isDirty,
            })}
          >
            {({ isSubmitting, isDirty }) => (
              <>
                <Button
                  type="submit"
                  size="sm"
                  disabled={busy || isSubmitting || !isDirty}
                >
                  <Save />
                  {intl.formatMessage({
                    id: "actions.save",
                    defaultMessage: "Save",
                  })}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={busy || !isDirty}
                  onClick={() => form.reset()}
                >
                  <RotateCcw />
                  {intl.formatMessage({
                    id: "actions.discard",
                    defaultMessage: "Discard",
                  })}
                </Button>
              </>
            )}
          </form.Subscribe>
        </div>
        <Button
          type="button"
          variant="destructive"
          size="sm"
          disabled={busy}
          onClick={() => setDeleteOpen(true)}
        >
          <Trash2 />
          {intl.formatMessage({
            id: "actions.delete",
            defaultMessage: "Delete",
          })}
        </Button>
      </div>
      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={galleryLabel(gallery)}
        showFileOptions
        deleteFileLabel={intl.formatMessage({
          id: "dialogs.delete_gallery_files",
          defaultMessage: "Delete gallery files",
        })}
        onConfirm={handleConfirmedDelete}
      />
    </form>
  );
}
