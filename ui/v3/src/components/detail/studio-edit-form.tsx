import { useEffect, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useSmartBack } from "src/hooks/use-smart-back";
import { useIntl } from "react-intl";
import { Save, RotateCcw, PlusIcon, Trash2Icon } from "lucide-react";
import { z } from "zod";
import * as GQL from "src/core/generated-graphql";
import {
  evictQueries,
  removeEntitiesFromCache,
  useEntityMutation,
} from "src/core/client";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Textarea } from "src/components/ui/textarea";
import { Switch } from "src/components/ui/switch";
import {
  Field,
  FieldError,
  FieldLabel,
  FieldTitle,
  FieldGroup,
} from "src/components/ui/field";
import { RatingSystem } from "src/components/ui/rating-system";
import {
  type EntityOption,
  EntitySingleSelect,
  EntityMultiSelect,
} from "src/components/forms/async-entity-select";
import { UrlListField } from "src/components/forms/url-list-field";
import {
  StashIdsField,
  type StashIdEntry,
} from "src/components/forms/stash-ids-field";
import { ImageField } from "src/components/forms/image-field";
import { DeleteDialog } from "src/components/detail/delete-dialog";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "src/components/ui/input-group";
import {
  CustomFieldsField,
  type CustomFieldMap,
  customFieldsUpdateInput,
} from "src/components/forms/custom-fields-field";

// ── Types ─────────────────────────────────────────────────────────────────────

type StudioData = NonNullable<GQL.FindStudioQuery["findStudio"]>;

interface StudioFormValues {
  name: string;
  image: string | null;
  aliases: string[];
  details: string;
  urls: string[];
  parent: EntityOption | null;
  tags: EntityOption[];
  rating100: number | null;
  favorite: boolean;
  ignore_auto_tag: boolean;
  organized: boolean;
  stash_ids: StashIdEntry[];
  custom_fields: CustomFieldMap;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function emptyStudioFormValues(): StudioFormValues {
  return {
    name: "",
    image: null,
    aliases: [],
    details: "",
    urls: [],
    parent: null,
    tags: [],
    rating100: null,
    favorite: false,
    ignore_auto_tag: false,
    organized: false,
    stash_ids: [],
    custom_fields: {},
  };
}

function studioToFormValues(s: StudioData): StudioFormValues {
  return {
    name: s.name,
    image: null,
    aliases: s.aliases ?? [],
    details: s.details ?? "",
    urls: s.urls ?? [],
    parent: s.parent_studio
      ? { id: s.parent_studio.id, name: s.parent_studio.name }
      : null,
    tags: s.tags.map((t) => ({ id: t.id, name: t.name })),
    rating100: s.rating100 ?? null,
    favorite: s.favorite,
    ignore_auto_tag: s.ignore_auto_tag,
    organized: s.organized,
    stash_ids: s.stash_ids.map((si) => ({
      endpoint: si.endpoint,
      stash_id: si.stash_id,
    })),
    custom_fields: s.custom_fields ?? {},
  };
}

function formValuesToInput(
  id: string,
  v: StudioFormValues,
): GQL.StudioUpdateInput {
  return {
    id,
    name: v.name || undefined,
    // null = no change (omit), "" = clear, non-empty = new image data
    image_input: v.image === null ? undefined : { data: v.image },
    aliases: v.aliases.filter(Boolean),
    details: v.details || null,
    urls: v.urls.filter(Boolean),
    parent_id: v.parent?.id ?? null,
    tag_ids: v.tags.map((t) => t.id),
    rating100: v.rating100,
    favorite: v.favorite,
    ignore_auto_tag: v.ignore_auto_tag,
    organized: v.organized,
    stash_ids: v.stash_ids
      .filter((s) => s.endpoint && s.stash_id)
      .map((s) => ({ endpoint: s.endpoint, stash_id: s.stash_id })),
    custom_fields: customFieldsUpdateInput(v.custom_fields),
  };
}

function formValuesToCreateInput(v: StudioFormValues): GQL.StudioCreateInput {
  return {
    name: v.name,
    image_input: v.image ? { data: v.image } : undefined,
    aliases: v.aliases.filter(Boolean),
    details: v.details || undefined,
    urls: v.urls.filter(Boolean),
    parent_id: v.parent?.id ?? undefined,
    tag_ids: v.tags.map((t) => t.id),
    rating100: v.rating100,
    favorite: v.favorite,
    ignore_auto_tag: v.ignore_auto_tag,
    organized: v.organized,
    stash_ids: v.stash_ids
      .filter((s) => s.endpoint && s.stash_id)
      .map((s) => ({ endpoint: s.endpoint, stash_id: s.stash_id })),
    custom_fields: v.custom_fields,
  };
}

// ── Simple alias list ─────────────────────────────────────────────────────────

function AliasListField({
  value,
  onChange,
  disabled,
}: {
  value: string[];
  onChange: (v: string[]) => void;
  disabled?: boolean;
}) {
  const intl = useIntl();

  function update(i: number, v: string) {
    const next = [...value];
    next[i] = v;
    onChange(next);
  }

  return (
    <div className="flex flex-col gap-1.5">
      {value.map((alias, i) => (
        <InputGroup key={i}>
          <InputGroupInput
            value={alias}
            disabled={disabled}
            onChange={(e) => update(i, e.target.value)}
          />
          <InputGroupAddon align="inline-end">
            <InputGroupButton
              size="icon-xs"
              variant="ghost"
              disabled={disabled}
              aria-label={intl.formatMessage({
                id: "actions.remove",
                defaultMessage: "Remove",
              })}
              onClick={() => onChange(value.filter((_, idx) => idx !== i))}
            >
              <Trash2Icon className="pointer-events-none size-3.5" />
            </InputGroupButton>
          </InputGroupAddon>
        </InputGroup>
      ))}
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={disabled}
        className="w-fit"
        onClick={() => onChange([...value, ""])}
      >
        <PlusIcon className="size-3.5" />
        {intl.formatMessage({
          id: "actions.add_alias",
          defaultMessage: "Add alias",
        })}
      </Button>
    </div>
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

type StudioEditFormProps =
  | {
      mode?: "edit";
      studio: StudioData;
      onSaved?: () => void;
      /** Called after deletion — use to close a containing sheet/dialog instead of navigating back. */
      onDeleted?: () => void;
    }
  | {
      mode: "create";
      /** Called after the new studio is created with its server-assigned id. */
      onCreated?: (id: string) => void;
    };

export function StudioEditForm(props: StudioEditFormProps) {
  const isCreate = props.mode === "create";
  const studio = isCreate ? null : props.studio;
  const intl = useIntl();
  const goBack = useSmartBack("/studios");

  const [updateStudio, { loading: updating }] = useEntityMutation(
    GQL.StudioUpdateDocument,
  );
  const [createStudio, { loading: creating }] = useEntityMutation(
    GQL.StudioCreateDocument,
  );
  const [destroyStudio, { loading: deleting }] = useEntityMutation(
    GQL.StudioDestroyDocument,
  );
  const saving = updating || creating;

  // Studio search (for parent)
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

  const form = useForm({
    defaultValues: studio
      ? studioToFormValues(studio)
      : emptyStudioFormValues(),
    onSubmit: async ({ value, formApi }) => {
      if (isCreate) {
        const result = await createStudio({
          variables: { input: formValuesToCreateInput(value) },
          update(cache) {
            evictQueries(cache, [GQL.FindStudiosDocument]);
          },
        });
        const newId = result.data?.studioCreate?.id;
        formApi.reset();
        if (newId) props.onCreated?.(newId);
      } else {
        await updateStudio({
          variables: { input: formValuesToInput(props.studio.id, value) },
        });
        formApi.reset(value);
        props.onSaved?.();
      }
    },
  });

  const busy = saving || deleting;

  const [deleteOpen, setDeleteOpen] = useState(false);

  async function handleConfirmedDelete() {
    if (!studio) return;
    await destroyStudio({
      variables: { id: studio.id },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Studio",
          listFieldName: "findStudios",
          itemsField: "studios",
          ids: [studio.id],
        });
      },
    });
    if (!isCreate && props.onDeleted) props.onDeleted();
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
        <FieldGroup className="gap-4 px-3 pt-4 pb-3">
          {/* Image */}
          <form.Field name="image">
            {(field) => (
              <ImageField
                label={intl.formatMessage({
                  id: "cover_image",
                  defaultMessage: "Cover Image",
                })}
                value={field.state.value}
                onChange={field.handleChange}
                existingImagePath={studio?.image_path}
                disabled={busy}
              />
            )}
          </form.Field>

          {/* Name */}
          <form.Field
            name="name"
            validators={{ onChange: z.string().min(1, "Required") }}
          >
            {(field) => {
              const hasError =
                field.state.meta.isTouched &&
                field.state.meta.errors.length > 0;
              return (
                <Field data-invalid={hasError ? "true" : undefined}>
                  <FieldLabel htmlFor={field.name}>
                    {intl.formatMessage({ id: "name", defaultMessage: "Name" })}
                  </FieldLabel>
                  <Input
                    id={field.name}
                    name={field.name}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    aria-invalid={hasError || undefined}
                    aria-describedby={
                      hasError ? `${field.name}-error` : undefined
                    }
                    disabled={busy}
                  />
                  <FieldError
                    id={`${field.name}-error`}
                    errors={field.state.meta.errors.map((e) =>
                      e != null ? { message: String(e) } : undefined,
                    )}
                  />
                </Field>
              );
            }}
          </form.Field>

          {/* Aliases */}
          <form.Field name="aliases">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "aliases",
                    defaultMessage: "Aliases",
                  })}
                </FieldLabel>
                <AliasListField
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

          {/* Parent studio */}
          <form.Field name="parent">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "parent_studio",
                    defaultMessage: "Parent studio",
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

          {/* Favourite */}
          <form.Field name="favorite">
            {(field) => (
              <Field orientation="horizontal">
                <FieldTitle>
                  {intl.formatMessage({
                    id: "favourite",
                    defaultMessage: "Favourite",
                  })}
                </FieldTitle>
                <Switch
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Organized */}
          <form.Field name="organized">
            {(field) => (
              <Field orientation="horizontal">
                <FieldTitle>
                  {intl.formatMessage({
                    id: "organized",
                    defaultMessage: "Organized",
                  })}
                </FieldTitle>
                <Switch
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                  disabled={busy}
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

          {/* Ignore auto-tag */}
          <form.Field name="ignore_auto_tag">
            {(field) => (
              <Field orientation="horizontal">
                <FieldTitle>
                  {intl.formatMessage({
                    id: "ignore_auto_tag",
                    defaultMessage: "Ignore auto-tag",
                  })}
                </FieldTitle>
                <Switch
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Stash IDs */}
          <form.Field name="stash_ids">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "stash_ids",
                    defaultMessage: "Stash IDs",
                  })}
                </FieldLabel>
                <StashIdsField
                  value={field.state.value}
                  onChange={field.handleChange}
                  disabled={busy}
                  searchType="studio"
                  searchQuery={form.state.values.name}
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
                {intl.formatMessage(
                  isCreate
                    ? { id: "actions.create", defaultMessage: "Create" }
                    : { id: "actions.save", defaultMessage: "Save" },
                )}
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
      {studio && (
        <DeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          entityName={studio.name}
          onConfirm={handleConfirmedDelete}
        />
      )}
    </form>
  );
}
