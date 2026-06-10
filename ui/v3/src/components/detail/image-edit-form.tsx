import { useEffect, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useEntityMutation } from "src/core/client";
import { useIntl } from "react-intl";
import { Save, RotateCcw, FileClock } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
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
import { dateToString } from "src/utils/date";
import {
  CustomFieldsField,
  type CustomFieldMap,
  customFieldsUpdateInput,
} from "src/components/forms/custom-fields-field";

// ── Types ─────────────────────────────────────────────────────────────────────

type ImageData = NonNullable<GQL.FindImageQuery["findImage"]>;

interface ImageFormValues {
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
  galleries: EntityOption[];
  custom_fields: CustomFieldMap;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function imageToFormValues(i: ImageData): ImageFormValues {
  return {
    title: i.title ?? "",
    code: i.code ?? "",
    date: i.date ?? "",
    details: i.details ?? "",
    photographer: i.photographer ?? "",
    urls: i.urls ?? [],
    rating100: i.rating100 ?? null,
    organized: i.organized,
    studio: i.studio ? { id: i.studio.id, name: i.studio.name } : null,
    performers: i.performers.map((p) => ({ id: p.id, name: p.name })),
    tags: i.tags.map((t) => ({ id: t.id, name: t.name })),
    galleries: i.galleries.map((g) => ({ id: g.id, name: galleryLabel(g) })),
    custom_fields: i.custom_fields ?? {},
  };
}

function formValuesToInput(
  id: string,
  v: ImageFormValues,
): GQL.ImageUpdateInput {
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
    gallery_ids: v.galleries.map((g) => g.id),
    custom_fields: customFieldsUpdateInput(v.custom_fields),
  };
}

// ── Component ─────────────────────────────────────────────────────────────────

interface ImageEditFormProps {
  image: ImageData;
  onSaved?: () => void;
}

export function ImageEditForm({ image, onSaved }: ImageEditFormProps) {
  const intl = useIntl();

  const [updateImage, { loading: saving }] = useEntityMutation(
    GQL.ImageUpdateDocument,
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

  // Gallery search
  const [galleryOptions, setGalleryOptions] = useState<EntityOption[]>([]);
  const [searchGalleries, { data: galleryData, loading: galleryLoading }] =
    useLazyQuery(GQL.FindGalleriesDocument);

  useEffect(() => {
    if (galleryData) {
      setGalleryOptions(
        galleryData.findGalleries.galleries.map((g) => ({
          id: g.id,
          name: galleryLabel(g),
        })),
      );
    }
  }, [galleryData]);

  const form = useForm({
    defaultValues: imageToFormValues(image),
    onSubmit: async ({ value, formApi }) => {
      await updateImage({
        variables: { input: formValuesToInput(image.id, value) },
      });
      formApi.reset(value);
      onSaved?.();
    },
  });

  const busy = saving;

  // Date derived from the primary visual file's mtime, used by the
  // "From file" button next to the Date field.
  const fileMTimeDate = (() => {
    const modTime = image.visual_files?.[0]?.mod_time;
    if (!modTime) return null;
    const d = new Date(modTime);
    if (Number.isNaN(d.getTime())) return null;
    return dateToString(d);
  })();

  // The form is a flex column whose middle child (the field group)
  // scrolls and whose last child (the action bar) is anchored at the
  // bottom — Save / Discard stay visible no matter how far the user
  // scrolls. Requires the parent to give the form a defined height
  // (Sheet does, since SheetContent has `data-[side=right]:h-full`).
  return (
    <form
      className="flex flex-col h-full min-w-0 overflow-x-hidden"
      onSubmit={(e) => {
        e.preventDefault();
        form.handleSubmit();
      }}
    >
      <div className="flex-1 min-h-0 overflow-y-auto p-3">
        <FieldGroup className="gap-4">
          {/* Title */}
          <form.Field name="title">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({ id: "title", defaultMessage: "Title" })}
                </FieldLabel>
                <Input
                  id={field.name}
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
                <div className="flex gap-2">
                  <DatePicker
                    value={field.state.value}
                    onChange={field.handleChange}
                    disabled={busy}
                    className="flex-1"
                  />
                  {fileMTimeDate && (
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={busy}
                      onClick={() => field.handleChange(fileMTimeDate)}
                      title={intl.formatMessage({
                        id: "actions.set_date_from_file_mtime",
                        defaultMessage: "Set date from file modification time",
                      })}
                    >
                      <FileClock className="size-4" />
                      {intl.formatMessage({
                        id: "actions.from_file",
                        defaultMessage: "From file",
                      })}
                    </Button>
                  )}
                </div>
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

          {/* Galleries */}
          <form.Field name="galleries">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "galleries",
                    defaultMessage: "Galleries",
                  })}
                </FieldLabel>
                <EntityMultiSelect
                  value={field.state.value}
                  onChange={field.handleChange}
                  options={galleryOptions}
                  onSearch={(q) =>
                    searchGalleries({
                      variables: { filter: { q, per_page: 20 } },
                    })
                  }
                  loading={galleryLoading}
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
      {/* Action bar — flex sibling pinned at form bottom. With the
          parent providing a defined height (route aside or sheet) the
          form's `flex flex-col h-full` keeps this row anchored
          regardless of how far the user scrolls inside the field
          group. */}
      <div className="shrink-0 flex items-center gap-2 border-t border-border bg-background/95 backdrop-blur-sm h-10 px-3">
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
    </form>
  );
}
