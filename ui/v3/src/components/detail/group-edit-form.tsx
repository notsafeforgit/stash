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
import {
  Field,
  FieldError,
  FieldLabel,
  FieldGroup,
} from "src/components/ui/field";
import { RatingSystem } from "src/components/ui/rating-system";
import { DatePicker } from "src/components/ui/date-picker";
import {
  type EntityOption,
  EntitySingleSelect,
  EntityMultiSelect,
} from "src/components/forms/async-entity-select";
import { UrlListField } from "src/components/forms/url-list-field";
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

type GroupData = NonNullable<GQL.FindGroupQuery["findGroup"]>;

interface GroupFormValues {
  name: string;
  front_image: string | null;
  back_image: string | null;
  aliases: string[];
  date: string;
  director: string;
  synopsis: string;
  urls: string[];
  studio: EntityOption | null;
  tags: EntityOption[];
  rating100: number | null;
  custom_fields: CustomFieldMap;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function emptyGroupFormValues(): GroupFormValues {
  return {
    name: "",
    front_image: null,
    back_image: null,
    aliases: [],
    date: "",
    director: "",
    synopsis: "",
    urls: [],
    studio: null,
    tags: [],
    rating100: null,
    custom_fields: {},
  };
}

function groupToFormValues(g: GroupData): GroupFormValues {
  return {
    name: g.name,
    front_image: null,
    back_image: null,
    aliases: g.aliases
      ? g.aliases
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean)
      : [],
    date: g.date ?? "",
    director: g.director ?? "",
    synopsis: g.synopsis ?? "",
    urls: g.urls ?? [],
    studio: g.studio ? { id: g.studio.id, name: g.studio.name } : null,
    tags: g.tags.map((t) => ({ id: t.id, name: t.name })),
    rating100: g.rating100 ?? null,
    custom_fields: g.custom_fields ?? {},
  };
}

function formValuesToInput(
  id: string,
  v: GroupFormValues,
): GQL.GroupUpdateInput {
  return {
    id,
    name: v.name || undefined,
    // null = no change (omit), "" = clear, non-empty = new image data
    front_image_input:
      v.front_image === null ? undefined : { data: v.front_image },
    back_image_input:
      v.back_image === null ? undefined : { data: v.back_image },
    aliases: v.aliases.filter(Boolean).join(", ") || null,
    date: v.date || null,
    director: v.director || null,
    synopsis: v.synopsis || null,
    urls: v.urls.filter(Boolean),
    studio_id: v.studio?.id ?? null,
    tag_ids: v.tags.map((t) => t.id),
    rating100: v.rating100,
    custom_fields: customFieldsUpdateInput(v.custom_fields),
  };
}

function formValuesToCreateInput(v: GroupFormValues): GQL.GroupCreateInput {
  return {
    name: v.name,
    front_image_input: v.front_image ? { data: v.front_image } : undefined,
    back_image_input: v.back_image ? { data: v.back_image } : undefined,
    aliases: v.aliases.filter(Boolean).join(", ") || undefined,
    date: v.date || undefined,
    director: v.director || undefined,
    synopsis: v.synopsis || undefined,
    urls: v.urls.filter(Boolean),
    studio_id: v.studio?.id ?? undefined,
    tag_ids: v.tags.map((t) => t.id),
    rating100: v.rating100,
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

type GroupEditFormProps =
  | {
      mode?: "edit";
      group: GroupData;
      onSaved?: () => void;
      /** Called after deletion — use to close a containing sheet/dialog instead of navigating back. */
      onDeleted?: () => void;
    }
  | {
      mode: "create";
      /** Called after the new group is created with its server-assigned id. */
      onCreated?: (id: string) => void;
    };

export function GroupEditForm(props: GroupEditFormProps) {
  const isCreate = props.mode === "create";
  const group = isCreate ? null : props.group;
  const intl = useIntl();
  const goBack = useSmartBack("/groups");

  const [updateGroup, { loading: updating }] = useEntityMutation(
    GQL.GroupUpdateDocument,
  );
  const [createGroup, { loading: creating }] = useEntityMutation(
    GQL.GroupCreateDocument,
  );
  const [destroyGroup, { loading: deleting }] = useEntityMutation(
    GQL.GroupDestroyDocument,
  );
  const saving = updating || creating;

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
    defaultValues: group ? groupToFormValues(group) : emptyGroupFormValues(),
    onSubmit: async ({ value, formApi }) => {
      if (isCreate) {
        const result = await createGroup({
          variables: { input: formValuesToCreateInput(value) },
          update(cache) {
            evictQueries(cache, [GQL.FindGroupsDocument]);
          },
        });
        const newId = result.data?.groupCreate?.id;
        formApi.reset();
        if (newId) props.onCreated?.(newId);
      } else {
        await updateGroup({
          variables: { input: formValuesToInput(props.group.id, value) },
        });
        formApi.reset(value);
        props.onSaved?.();
      }
    },
  });

  const busy = saving || deleting;

  const [deleteOpen, setDeleteOpen] = useState(false);

  async function handleConfirmedDelete() {
    if (!group) return;
    await destroyGroup({
      variables: { id: group.id },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Group",
          listFieldName: "findGroups",
          itemsField: "groups",
          ids: [group.id],
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
          {/* Front image */}
          <form.Field name="front_image">
            {(field) => (
              <ImageField
                label={intl.formatMessage({
                  id: "front_image",
                  defaultMessage: "Front image",
                })}
                value={field.state.value}
                onChange={field.handleChange}
                existingImagePath={group?.front_image_path}
                disabled={busy}
                setLabelId="actions.set_front_image"
              />
            )}
          </form.Field>

          {/* Back image */}
          <form.Field name="back_image">
            {(field) => (
              <ImageField
                label={intl.formatMessage({
                  id: "back_image",
                  defaultMessage: "Back image",
                })}
                value={field.state.value}
                onChange={field.handleChange}
                existingImagePath={group?.back_image_path}
                disabled={busy}
                setLabelId="actions.set_back_image"
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

          {/* Synopsis */}
          <form.Field name="synopsis">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "synopsis",
                    defaultMessage: "Synopsis",
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

          {/* Director */}
          <form.Field name="director">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "director",
                    defaultMessage: "Director",
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
      {group && (
        <DeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          entityName={group.name}
          onConfirm={handleConfirmedDelete}
        />
      )}
    </form>
  );
}
