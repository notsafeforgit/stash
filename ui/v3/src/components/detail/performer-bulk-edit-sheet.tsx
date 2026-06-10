import { useEffect, useMemo, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useEntityMutation } from "src/core/client";
import { useIntl } from "react-intl";
import { toast } from "sonner";
import * as GQL from "src/core/generated-graphql";
import { Field, FieldGroup, FieldLabel } from "src/components/ui/field";
import { BulkBooleanField } from "src/components/forms/bulk-boolean-field";
import { BulkRatingField } from "src/components/forms/bulk-rating-field";
import { BulkSelectField } from "src/components/forms/bulk-select-field";
import { BulkTextField } from "src/components/forms/bulk-text-field";
import { BulkEntityField } from "src/components/forms/bulk-entity-field";
import type { EntityOption } from "src/components/forms/async-entity-select";
import {
  getIntersectionIds,
  getUnionIds,
  makeBulkUpdateIds,
} from "src/utils/bulkUpdate";
import { BulkEditSheet } from "./bulk-edit-sheet";
import type { BulkApplyTarget } from "src/components/list/list-provider";

// ── Types ─────────────────────────────────────────────────────────────────────

export type PerformerBulkItem = {
  id: string;
  name: string;
  disambiguation?: string | null;
  image_path?: string | null;
  birthdate?: string | null;
  death_date?: string | null;
  tags: Array<{ id: string; name: string }>;
};

interface PerformerBulkFormValues {
  gender: GQL.GenderEnum | null | undefined;
  country: string | null | undefined;
  ethnicity: string | null | undefined;
  hair_color: string | null | undefined;
  eye_color: string | null | undefined;
  circumcised: GQL.CircumcisedEnum | null | undefined;
  fake_tits: string | null | undefined;
  favorite: boolean | undefined;
  ignore_auto_tag: boolean | undefined;
  rating100: number | null | undefined;
  tag_ids: GQL.BulkUpdateIds;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildInitialValues(
  _items: PerformerBulkItem[],
): PerformerBulkFormValues {
  // Add mode starts empty — see scene-bulk-edit-sheet for rationale.
  return {
    gender: undefined,
    country: undefined,
    ethnicity: undefined,
    hair_color: undefined,
    eye_color: undefined,
    circumcised: undefined,
    fake_tits: undefined,
    favorite: undefined,
    ignore_auto_tag: undefined,
    rating100: undefined,
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildEmptyValues(): PerformerBulkFormValues {
  return {
    gender: undefined,
    country: undefined,
    ethnicity: undefined,
    hair_color: undefined,
    eye_color: undefined,
    circumcised: undefined,
    fake_tits: undefined,
    favorite: undefined,
    ignore_auto_tag: undefined,
    rating100: undefined,
    tag_ids: makeBulkUpdateIds([], GQL.BulkUpdateIdMode.Add),
  };
}

function buildMutationInput(
  ids: string[],
  v: PerformerBulkFormValues,
  applyToAll: boolean,
  applyToAllTarget?: BulkApplyTarget,
): GQL.BulkPerformerUpdateInput {
  const base: GQL.BulkPerformerUpdateInput = {
    ids: applyToAll ? [] : ids,
    gender: v.gender,
    country: v.country,
    ethnicity: v.ethnicity,
    hair_color: v.hair_color,
    eye_color: v.eye_color,
    circumcised: v.circumcised,
    fake_tits: v.fake_tits,
    favorite: v.favorite,
    ignore_auto_tag: v.ignore_auto_tag,
    rating100: v.rating100,
    tag_ids: v.tag_ids,
  };
  if (applyToAll && applyToAllTarget) {
    base.apply_to_items_matching_filters = true;
    base.find_filter = applyToAllTarget.findFilter;
    base.performer_filter =
      applyToAllTarget.objectFilter as GQL.PerformerFilterType;
  }
  return base;
}

function getGenderOptions(intl: ReturnType<typeof useIntl>) {
  return [
    {
      value: GQL.GenderEnum.Female,
      label: intl.formatMessage({
        id: "gender_types.FEMALE",
        defaultMessage: "Female",
      }),
    },
    {
      value: GQL.GenderEnum.Male,
      label: intl.formatMessage({
        id: "gender_types.MALE",
        defaultMessage: "Male",
      }),
    },
    {
      value: GQL.GenderEnum.TransgenderFemale,
      label: intl.formatMessage({
        id: "gender_types.TRANSGENDER_FEMALE",
        defaultMessage: "Transgender Female",
      }),
    },
    {
      value: GQL.GenderEnum.TransgenderMale,
      label: intl.formatMessage({
        id: "gender_types.TRANSGENDER_MALE",
        defaultMessage: "Transgender Male",
      }),
    },
    {
      value: GQL.GenderEnum.Intersex,
      label: intl.formatMessage({
        id: "gender_types.INTERSEX",
        defaultMessage: "Intersex",
      }),
    },
    {
      value: GQL.GenderEnum.NonBinary,
      label: intl.formatMessage({
        id: "gender_types.NON_BINARY",
        defaultMessage: "Non-Binary",
      }),
    },
  ];
}

function getCircumcisedOptions(intl: ReturnType<typeof useIntl>) {
  return [
    {
      value: GQL.CircumcisedEnum.Cut,
      label: intl.formatMessage({
        id: "circumcised_types.CUT",
        defaultMessage: "Cut",
      }),
    },
    {
      value: GQL.CircumcisedEnum.Uncut,
      label: intl.formatMessage({
        id: "circumcised_types.UNCUT",
        defaultMessage: "Uncut",
      }),
    },
  ];
}

// ── Component ─────────────────────────────────────────────────────────────────

interface PerformerBulkEditSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: PerformerBulkItem[];
  applyToAllTarget?: BulkApplyTarget;
  totalCount?: number;
  onSaved?: () => void;
}

export function PerformerBulkEditSheet({
  open,
  onOpenChange,
  items,
  applyToAllTarget,
  totalCount,
  onSaved,
}: PerformerBulkEditSheetProps) {
  const intl = useIntl();
  const [applyToAll, setApplyToAll] = useState(false);
  const initialValuesRef = useRef(buildInitialValues(items));

  const [bulkUpdatePerformers, { loading: savingSync }] = useEntityMutation(
    GQL.BulkPerformerUpdateDocument,
  );
  const [bulkUpdatePerformersJob, { loading: savingJob }] = useEntityMutation(
    GQL.BulkPerformerUpdateJobDocument,
  );
  const saving = savingSync || savingJob;

  const [tagOptions, setTagOptions] = useState<EntityOption[]>([]);
  const [searchTags, { data: tagData, loading: tagLoading }] = useLazyQuery(
    GQL.FindTagsDocument,
  );

  useEffect(() => {
    if (tagData)
      setTagOptions(
        tagData.findTags.tags.map((t) => ({ id: t.id, name: t.name })),
      );
  }, [tagData]);

  const genderOptions = useMemo(() => getGenderOptions(intl), [intl]);
  const circumcisedOptions = useMemo(() => getCircumcisedOptions(intl), [intl]);

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
        await bulkUpdatePerformersJob({ variables: { input } });
        toast.success(
          intl.formatMessage({
            id: "toast.started_bulk_update",
            defaultMessage: "Bulk update started",
          }),
        );
      } else {
        await bulkUpdatePerformers({ variables: { input } });
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
          id: "dialogs.edit_performers_title",
          defaultMessage: "Edit {count} performers",
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
        {/* Gender */}
        <form.Field name="gender">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({ id: "gender", defaultMessage: "Gender" })}
              </FieldLabel>
              <BulkSelectField<GQL.GenderEnum>
                value={field.state.value}
                onChange={field.handleChange}
                options={genderOptions}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Country */}
        <form.Field name="country">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "country",
                  defaultMessage: "Country",
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

        {/* Ethnicity */}
        <form.Field name="ethnicity">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "ethnicity",
                  defaultMessage: "Ethnicity",
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

        {/* Hair colour */}
        <form.Field name="hair_color">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "hair_color",
                  defaultMessage: "Hair Colour",
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

        {/* Eye colour */}
        <form.Field name="eye_color">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "eye_color",
                  defaultMessage: "Eye Colour",
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

        {/* Circumcised */}
        <form.Field name="circumcised">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "circumcised",
                  defaultMessage: "Circumcised",
                })}
              </FieldLabel>
              <BulkSelectField<GQL.CircumcisedEnum>
                value={field.state.value}
                onChange={field.handleChange}
                options={circumcisedOptions}
                disabled={saving}
              />
            </Field>
          )}
        </form.Field>

        {/* Fake tits */}
        <form.Field name="fake_tits">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "fake_tits",
                  defaultMessage: "Fake Tits",
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
