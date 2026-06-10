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
import {
  type EntityOption,
  EntityMultiSelect,
} from "src/components/forms/async-entity-select";
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

type TagData = NonNullable<GQL.FindTagQuery["findTag"]>;

interface TagFormValues {
  name: string;
  image: string | null;
  sort_name: string;
  description: string;
  aliases: string[];
  favorite: boolean;
  ignore_auto_tag: boolean;
  parents: EntityOption[];
  children: EntityOption[];
  stash_ids: StashIdEntry[];
  custom_fields: CustomFieldMap;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function emptyTagFormValues(): TagFormValues {
  return {
    name: "",
    image: null,
    sort_name: "",
    description: "",
    aliases: [],
    favorite: false,
    ignore_auto_tag: false,
    parents: [],
    children: [],
    stash_ids: [],
    custom_fields: {},
  };
}

function tagToFormValues(t: TagData): TagFormValues {
  return {
    name: t.name,
    image: null,
    sort_name: t.sort_name ?? "",
    description: t.description ?? "",
    aliases: t.aliases ?? [],
    favorite: t.favorite,
    ignore_auto_tag: t.ignore_auto_tag,
    parents: t.parents.map((p) => ({ id: p.id, name: p.name })),
    children: t.children.map((c) => ({ id: c.id, name: c.name })),
    stash_ids: t.stash_ids.map((s) => ({
      endpoint: s.endpoint,
      stash_id: s.stash_id,
    })),
    custom_fields: t.custom_fields ?? {},
  };
}

function formValuesToInput(id: string, v: TagFormValues): GQL.TagUpdateInput {
  return {
    id,
    name: v.name || undefined,
    // null = no change (omit), "" = clear, non-empty = new image data
    image_input: v.image === null ? undefined : { data: v.image },
    sort_name: v.sort_name || null,
    description: v.description || null,
    aliases: v.aliases.filter(Boolean),
    favorite: v.favorite,
    ignore_auto_tag: v.ignore_auto_tag,
    parent_ids: v.parents.map((p) => p.id),
    child_ids: v.children.map((c) => c.id),
    stash_ids: v.stash_ids
      .filter((s) => s.endpoint && s.stash_id)
      .map((s) => ({ endpoint: s.endpoint, stash_id: s.stash_id })),
    custom_fields: customFieldsUpdateInput(v.custom_fields),
  };
}

function formValuesToCreateInput(v: TagFormValues): GQL.TagCreateInput {
  return {
    name: v.name,
    image_input: v.image ? { data: v.image } : undefined,
    sort_name: v.sort_name || undefined,
    description: v.description || undefined,
    aliases: v.aliases.filter(Boolean),
    favorite: v.favorite,
    ignore_auto_tag: v.ignore_auto_tag,
    parent_ids: v.parents.map((p) => p.id),
    child_ids: v.children.map((c) => c.id),
    stash_ids: v.stash_ids
      .filter((s) => s.endpoint && s.stash_id)
      .map((s) => ({ endpoint: s.endpoint, stash_id: s.stash_id })),
    custom_fields: v.custom_fields,
  };
}

// ── Simple string alias list ───────────────────────────────────────────────────

function SimpleAliasListField({
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

  function remove(i: number) {
    onChange(value.filter((_, idx) => idx !== i));
  }

  function add() {
    onChange([...value, ""]);
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
              onClick={() => remove(i)}
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
        onClick={add}
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

type TagEditFormProps =
  | {
      mode?: "edit";
      tag: TagData;
      onSaved?: () => void;
      /** Called after deletion — use to close a containing sheet/dialog instead of navigating back. */
      onDeleted?: () => void;
    }
  | {
      mode: "create";
      /** Called after the new tag is created with its server-assigned id. */
      onCreated?: (id: string) => void;
    };

export function TagEditForm(props: TagEditFormProps) {
  const isCreate = props.mode === "create";
  const tag = isCreate ? null : props.tag;
  const intl = useIntl();
  const goBack = useSmartBack("/tags");

  const [updateTag, { loading: updating }] = useEntityMutation(
    GQL.TagUpdateDocument,
  );
  const [createTag, { loading: creating }] = useEntityMutation(
    GQL.TagCreateDocument,
  );
  const [destroyTag, { loading: deleting }] = useEntityMutation(
    GQL.TagDestroyDocument,
  );
  const saving = updating || creating;

  // Tag search (for parents / children)
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
    defaultValues: tag ? tagToFormValues(tag) : emptyTagFormValues(),
    onSubmit: async ({ value, formApi }) => {
      if (isCreate) {
        const result = await createTag({
          variables: { input: formValuesToCreateInput(value) },
          update(cache) {
            evictQueries(cache, [GQL.FindTagsDocument]);
          },
        });
        const newId = result.data?.tagCreate?.id;
        formApi.reset();
        if (newId) props.onCreated?.(newId);
      } else {
        await updateTag({
          variables: { input: formValuesToInput(props.tag.id, value) },
        });
        formApi.reset(value);
        props.onSaved?.();
      }
    },
  });

  const busy = saving || deleting;

  const [deleteOpen, setDeleteOpen] = useState(false);

  async function handleConfirmedDelete() {
    if (!tag) return;
    await destroyTag({
      variables: { id: tag.id },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Tag",
          listFieldName: "findTags",
          itemsField: "tags",
          ids: [tag.id],
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
                existingImagePath={tag?.image_path}
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
                <SimpleAliasListField
                  value={field.state.value}
                  onChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Description */}
          <form.Field name="description">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "description",
                    defaultMessage: "Description",
                  })}
                </FieldLabel>
                <Textarea
                  id={field.name}
                  name={field.name}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  disabled={busy}
                  rows={3}
                />
              </Field>
            )}
          </form.Field>

          {/* Parent tags */}
          <form.Field name="parents">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "parent_tags",
                    defaultMessage: "Parent tags",
                  })}
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

          {/* Child tags */}
          <form.Field name="children">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "sub_tags",
                    defaultMessage: "Sub-tags",
                  })}
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

          {/* Sort name */}
          <form.Field name="sort_name">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "sort_name",
                    defaultMessage: "Sort name",
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
                  searchType="tag"
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
      {tag && (
        <DeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          entityName={tag.name}
          onConfirm={handleConfirmedDelete}
        />
      )}
    </form>
  );
}
