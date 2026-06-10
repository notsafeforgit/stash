import { useEffect, useState } from "react";
import { z } from "zod";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useSmartBack } from "src/hooks/use-smart-back";
import { useIntl } from "react-intl";
import { Save, RotateCcw, PlusIcon, Trash2Icon } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { GenderEnum, CircumcisedEnum } from "src/core/generated-graphql";
import {
  evictQueries,
  removeEntitiesFromCache,
  useEntityMutation,
} from "src/core/client";
import { useToast } from "src/hooks/toast";
import {
  useAvailablePerformerScrapers,
  sourceToInput,
} from "src/components/scrape/use-available-scrapers";
import {
  ScraperMenu,
  type ScrapeAction,
} from "src/components/scrape/scraper-menu";
import { PerformerSearchDialog } from "src/components/scrape/performer-search-dialog";
import { PerformerScrapeMergeDialog } from "src/components/scrape/performer-scrape-merge-dialog";
import type { ScrapeSource } from "src/components/scrape/use-available-scrapers";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Textarea } from "src/components/ui/textarea";
import { Switch } from "src/components/ui/switch";
import { Toggle } from "src/components/ui/toggle";
import { RatingSystem } from "src/components/ui/rating-system";
import {
  Field,
  FieldError,
  FieldLabel,
  FieldTitle,
  FieldGroup,
} from "src/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import {
  type EntityOption,
  EntityMultiSelect,
} from "src/components/forms/async-entity-select";
import { UrlListField } from "src/components/forms/url-list-field";
import {
  StashIdsField,
  type StashIdEntry,
} from "src/components/forms/stash-ids-field";
import { ImageField } from "src/components/forms/image-field";
import { CountrySelect } from "src/components/forms/country-select";
import { DatePicker } from "src/components/ui/date-picker";
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

type PerformerData = NonNullable<GQL.FindPerformerQuery["findPerformer"]>;

export interface AliasEntry {
  alias: string;
  ignore_auto_tag: boolean;
}

export interface PerformerFormValues {
  name: string;
  image: string | null;
  disambiguation: string;
  aliases: AliasEntry[];
  gender: GenderEnum | "";
  birthdate: string;
  death_date: string;
  country: string;
  ethnicity: string;
  hair_color: string;
  eye_color: string;
  height_cm: string;
  weight: string;
  measurements: string;
  penis_length: string;
  circumcised: CircumcisedEnum | "";
  fake_tits: string;
  career_start: string;
  career_end: string;
  tattoos: string;
  piercings: string;
  details: string;
  urls: string[];
  tags: EntityOption[];
  rating100: number | null;
  favorite: boolean;
  ignore_auto_tag: boolean;
  stash_ids: StashIdEntry[];
  custom_fields: CustomFieldMap;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function emptyPerformerFormValues(): PerformerFormValues {
  return {
    name: "",
    image: null,
    disambiguation: "",
    aliases: [],
    gender: "",
    birthdate: "",
    death_date: "",
    country: "",
    ethnicity: "",
    hair_color: "",
    eye_color: "",
    height_cm: "",
    weight: "",
    measurements: "",
    penis_length: "",
    circumcised: "",
    fake_tits: "",
    career_start: "",
    career_end: "",
    tattoos: "",
    piercings: "",
    details: "",
    urls: [],
    tags: [],
    rating100: null,
    favorite: false,
    ignore_auto_tag: false,
    stash_ids: [],
    custom_fields: {},
  };
}

function performerToFormValues(p: PerformerData): PerformerFormValues {
  return {
    name: p.name,
    image: null,
    disambiguation: p.disambiguation ?? "",
    aliases: p.aliases.map((a) => ({
      alias: a.alias,
      ignore_auto_tag: a.ignore_auto_tag,
    })),
    gender: p.gender ?? "",
    birthdate: p.birthdate ?? "",
    death_date: p.death_date ?? "",
    country: p.country ?? "",
    ethnicity: p.ethnicity ?? "",
    hair_color: p.hair_color ?? "",
    eye_color: p.eye_color ?? "",
    height_cm: p.height_cm != null ? String(p.height_cm) : "",
    weight: p.weight != null ? String(p.weight) : "",
    measurements: p.measurements ?? "",
    penis_length: p.penis_length != null ? String(p.penis_length) : "",
    circumcised: p.circumcised ?? "",
    fake_tits: p.fake_tits ?? "",
    career_start: p.career_start ?? "",
    career_end: p.career_end ?? "",
    tattoos: p.tattoos ?? "",
    piercings: p.piercings ?? "",
    details: p.details ?? "",
    urls: p.urls ?? [],
    tags: p.tags.map((t) => ({ id: t.id, name: t.name })),
    rating100: p.rating100 ?? null,
    favorite: p.favorite,
    ignore_auto_tag: p.ignore_auto_tag,
    stash_ids: p.stash_ids.map((s) => ({
      endpoint: s.endpoint,
      stash_id: s.stash_id,
    })),
    custom_fields: p.custom_fields ?? {},
  };
}

function formValuesToInput(
  id: string,
  v: PerformerFormValues,
): GQL.PerformerUpdateInput {
  return {
    id,
    name: v.name || undefined,
    // null = unchanged, "" = clear, non-empty = new image data
    image_input: v.image === null ? undefined : { data: v.image },
    disambiguation: v.disambiguation || null,
    aliases: v.aliases
      .filter((a) => a.alias.trim())
      .map((a) => ({
        alias: a.alias.trim(),
        ignore_auto_tag: a.ignore_auto_tag,
      })),
    gender: v.gender || null,
    birthdate: v.birthdate || null,
    death_date: v.death_date || null,
    country: v.country || null,
    ethnicity: v.ethnicity || null,
    hair_color: v.hair_color || null,
    eye_color: v.eye_color || null,
    height_cm: v.height_cm ? parseInt(v.height_cm, 10) : null,
    weight: v.weight ? parseInt(v.weight, 10) : null,
    measurements: v.measurements || null,
    penis_length: v.penis_length ? parseFloat(v.penis_length) : null,
    circumcised: v.circumcised || null,
    fake_tits: v.fake_tits || null,
    career_start: v.career_start || null,
    career_end: v.career_end || null,
    tattoos: v.tattoos || null,
    piercings: v.piercings || null,
    details: v.details || null,
    urls: v.urls.filter(Boolean),
    tag_ids: v.tags.map((t) => t.id),
    rating100: v.rating100,
    favorite: v.favorite,
    ignore_auto_tag: v.ignore_auto_tag,
    stash_ids: v.stash_ids
      .filter((s) => s.endpoint && s.stash_id)
      .map((s) => ({ endpoint: s.endpoint, stash_id: s.stash_id })),
    custom_fields: customFieldsUpdateInput(v.custom_fields),
  };
}

function formValuesToCreateInput(
  v: PerformerFormValues,
): GQL.PerformerCreateInput {
  return {
    name: v.name,
    image_input: v.image ? { data: v.image } : undefined,
    disambiguation: v.disambiguation || undefined,
    aliases: v.aliases
      .filter((a) => a.alias.trim())
      .map((a) => ({
        alias: a.alias.trim(),
        ignore_auto_tag: a.ignore_auto_tag,
      })),
    gender: v.gender || undefined,
    birthdate: v.birthdate || undefined,
    death_date: v.death_date || undefined,
    country: v.country || undefined,
    ethnicity: v.ethnicity || undefined,
    hair_color: v.hair_color || undefined,
    eye_color: v.eye_color || undefined,
    height_cm: v.height_cm ? parseInt(v.height_cm, 10) : undefined,
    weight: v.weight ? parseInt(v.weight, 10) : undefined,
    measurements: v.measurements || undefined,
    penis_length: v.penis_length ? parseFloat(v.penis_length) : undefined,
    circumcised: v.circumcised || undefined,
    fake_tits: v.fake_tits || undefined,
    career_start: v.career_start || undefined,
    career_end: v.career_end || undefined,
    tattoos: v.tattoos || undefined,
    piercings: v.piercings || undefined,
    details: v.details || undefined,
    urls: v.urls.filter(Boolean),
    tag_ids: v.tags.map((t) => t.id),
    rating100: v.rating100,
    favorite: v.favorite,
    ignore_auto_tag: v.ignore_auto_tag,
    stash_ids: v.stash_ids
      .filter((s) => s.endpoint && s.stash_id)
      .map((s) => ({ endpoint: s.endpoint, stash_id: s.stash_id })),
    custom_fields: v.custom_fields,
  };
}

function getGenderOptions(
  intl: ReturnType<typeof useIntl>,
): { value: GenderEnum | ""; label: string }[] {
  return [
    { value: "", label: "—" },
    {
      value: GenderEnum.Female,
      label: intl.formatMessage({
        id: "gender_types.FEMALE",
        defaultMessage: "Female",
      }),
    },
    {
      value: GenderEnum.Male,
      label: intl.formatMessage({
        id: "gender_types.MALE",
        defaultMessage: "Male",
      }),
    },
    {
      value: GenderEnum.TransgenderFemale,
      label: intl.formatMessage({
        id: "gender_types.TRANSGENDER_FEMALE",
        defaultMessage: "Transgender Female",
      }),
    },
    {
      value: GenderEnum.TransgenderMale,
      label: intl.formatMessage({
        id: "gender_types.TRANSGENDER_MALE",
        defaultMessage: "Transgender Male",
      }),
    },
    {
      value: GenderEnum.Intersex,
      label: intl.formatMessage({
        id: "gender_types.INTERSEX",
        defaultMessage: "Intersex",
      }),
    },
    {
      value: GenderEnum.NonBinary,
      label: intl.formatMessage({
        id: "gender_types.NON_BINARY",
        defaultMessage: "Non-Binary",
      }),
    },
  ];
}

function getCircumcisedOptions(
  intl: ReturnType<typeof useIntl>,
): { value: CircumcisedEnum | ""; label: string }[] {
  return [
    { value: "", label: "—" },
    {
      value: CircumcisedEnum.Cut,
      label: intl.formatMessage({
        id: "circumcised_types.CUT",
        defaultMessage: "Cut",
      }),
    },
    {
      value: CircumcisedEnum.Uncut,
      label: intl.formatMessage({
        id: "circumcised_types.UNCUT",
        defaultMessage: "Uncut",
      }),
    },
  ];
}

// ── Alias list field ───────────────────────────────────────────────────────────

interface AliasListFieldProps {
  value: AliasEntry[];
  onChange: (entries: AliasEntry[]) => void;
  disabled?: boolean;
}

function AliasListField({ value, onChange, disabled }: AliasListFieldProps) {
  const intl = useIntl();

  function updateAlias(index: number, alias: string) {
    const next = [...value];
    next[index] = { ...next[index], alias };
    onChange(next);
  }

  function updateAutoTagEnabled(index: number, enabled: boolean) {
    // Storage stays as `ignore_auto_tag` (matches the backend's
    // PerformerAlias schema) but the UI is the inverse: a Toggle
    // labelled "Auto-tag" reads as on when the alias contributes to
    // auto-tagging. Inverse here keeps the persisted shape unchanged.
    const next = [...value];
    next[index] = { ...next[index], ignore_auto_tag: !enabled };
    onChange(next);
  }

  function remove(index: number) {
    onChange(value.filter((_, i) => i !== index));
  }

  function add() {
    onChange([...value, { alias: "", ignore_auto_tag: false }]);
  }

  return (
    <div className="flex flex-col gap-1.5">
      {value.map((entry, i) => (
        <div key={i} className="flex items-center gap-2">
          <InputGroup className="flex-1">
            <InputGroupInput
              value={entry.alias}
              disabled={disabled}
              onChange={(e) => updateAlias(i, e.target.value)}
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
          <Toggle
            variant="outline"
            size="sm"
            pressed={!entry.ignore_auto_tag}
            onPressedChange={(pressed) => updateAutoTagEnabled(i, pressed)}
            disabled={disabled}
            aria-label={intl.formatMessage({
              id: "performer_alias_auto_tag_aria",
              defaultMessage: "Use this alias for auto-tagging",
            })}
            title={intl.formatMessage({
              id: "performer_alias_auto_tag_aria",
              defaultMessage: "Use this alias for auto-tagging",
            })}
          >
            {intl.formatMessage({
              id: "auto_tag",
              defaultMessage: "Auto-tag",
            })}
          </Toggle>
        </div>
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

type PerformerEditFormProps =
  | {
      mode?: "edit";
      performer: PerformerData;
      /** Called after a successful save — use to close a containing sheet/dialog. */
      onSaved?: () => void;
      /** Called after deletion — use to close a containing sheet/dialog instead of navigating back. */
      onDeleted?: () => void;
    }
  | {
      mode: "create";
      /** Called after the new performer is created with its server-assigned id. */
      onCreated?: (id: string) => void;
    };

export function PerformerEditForm(props: PerformerEditFormProps) {
  const isCreate = props.mode === "create";
  const performer = isCreate ? null : props.performer;
  const intl = useIntl();
  const goBack = useSmartBack("/performers");
  const genderOptions = getGenderOptions(intl);
  const circumcisedOptions = getCircumcisedOptions(intl);

  // ── Mutations ──
  const [updatePerformer, { loading: updating }] = useEntityMutation(
    GQL.PerformerUpdateDocument,
  );
  const [createPerformer, { loading: creating }] = useEntityMutation(
    GQL.PerformerCreateDocument,
  );
  const [destroyPerformer, { loading: deleting }] = useEntityMutation(
    GQL.PerformerDestroyDocument,
  );
  const saving = updating || creating;

  // ── Async search ──
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

  // ── Form ──
  const form = useForm({
    defaultValues: performer
      ? performerToFormValues(performer)
      : emptyPerformerFormValues(),
    onSubmit: async ({ value, formApi }) => {
      if (isCreate) {
        const result = await createPerformer({
          variables: { input: formValuesToCreateInput(value) },
          update(cache) {
            evictQueries(cache, [GQL.FindPerformersDocument]);
          },
        });
        const newId = result.data?.performerCreate?.id;
        formApi.reset();
        if (newId) props.onCreated?.(newId);
      } else {
        await updatePerformer({
          variables: { input: formValuesToInput(props.performer.id, value) },
        });
        formApi.reset(value);
        props.onSaved?.();
      }
    },
  });

  // ── Scrape ──
  const toast = useToast();
  const {
    scrapers,
    stashBoxes,
    hasAny: hasAnyScrapeSource,
  } = useAvailablePerformerScrapers();
  const [scrapeSearchSource, setScrapeSearchSource] =
    useState<ScrapeSource | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [mergePayload, setMergePayload] =
    useState<GQL.ScrapedPerformerDataFragment | null>(null);
  const [mergeSource, setMergeSource] = useState<ScrapeSource | null>(null);
  const [mergeSnapshot, setMergeSnapshot] =
    useState<PerformerFormValues | null>(null);
  const [runFragmentScrape, { loading: scraping }] = useLazyQuery(
    GQL.ScrapeSinglePerformerDocument,
  );

  function openMergeWith(
    scraped: GQL.ScrapedPerformerDataFragment,
    source: ScrapeSource,
  ) {
    setMergePayload(scraped);
    setMergeSource(source);
    setMergeSnapshot(form.state.values);
    setMergeOpen(true);
  }

  async function handleScraperPick(source: ScrapeSource, action: ScrapeAction) {
    if (action === GQL.ScrapeType.Name) {
      setScrapeSearchSource(source);
      setSearchOpen(true);
      return;
    }
    // FRAGMENT: probe with the current form state. The mapper sends only
    // populated fields so an empty form doesn't drown the scraper in nulls.
    const v = form.state.values;
    const performerInput: GQL.ScrapedPerformerInput = {
      name: v.name || undefined,
      disambiguation: v.disambiguation || undefined,
      gender: v.gender || undefined,
      birthdate: v.birthdate || undefined,
      death_date: v.death_date || undefined,
      country: v.country || undefined,
      ethnicity: v.ethnicity || undefined,
      hair_color: v.hair_color || undefined,
      eye_color: v.eye_color || undefined,
      height: v.height_cm || undefined,
      weight: v.weight || undefined,
      measurements: v.measurements || undefined,
      penis_length: v.penis_length || undefined,
      circumcised: v.circumcised || undefined,
      fake_tits: v.fake_tits || undefined,
      career_start: v.career_start || undefined,
      career_end: v.career_end || undefined,
      tattoos: v.tattoos || undefined,
      piercings: v.piercings || undefined,
      details: v.details || undefined,
      aliases: v.aliases.map((a) => a.alias).join(", ") || undefined,
      urls: v.urls.length > 0 ? v.urls : undefined,
      stored_id: performer?.id,
    };
    try {
      const result = await runFragmentScrape({
        variables: {
          source: sourceToInput(source),
          input: { performer_input: performerInput },
        },
      });
      const hits = result.data?.scrapeSinglePerformer ?? [];
      if (hits.length === 0) {
        toast.error(
          intl.formatMessage({
            id: "scrape.no_match",
            defaultMessage: "No match returned by scraper.",
          }),
        );
        return;
      }
      // Multiple-result picker isn't built yet — use the first hit.
      // Per-scraper this is usually the right call (fragment scrapers are
      // usually deterministic).
      openMergeWith(hits[0] as GQL.ScrapedPerformerDataFragment, source);
    } catch (e) {
      toast.error(e);
    }
  }

  function handleSearchSelect(scraped: GQL.ScrapedPerformerDataFragment) {
    setSearchOpen(false);
    if (scrapeSearchSource) openMergeWith(scraped, scrapeSearchSource);
  }

  function applyScrapePatch(patch: Partial<PerformerFormValues>) {
    for (const [key, value] of Object.entries(patch)) {
      form.setFieldValue(
        key as keyof PerformerFormValues,
        value as PerformerFormValues[keyof PerformerFormValues],
      );
    }
  }

  const busy = saving || deleting || scraping;

  const [deleteOpen, setDeleteOpen] = useState(false);

  async function handleConfirmedDelete() {
    if (!performer) return;
    await destroyPerformer({
      variables: { id: performer.id },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Performer",
          listFieldName: "findPerformers",
          itemsField: "performers",
          ids: [performer.id],
        });
      },
    });
    if (!isCreate && props.onDeleted) props.onDeleted();
    else goBack();
  }

  // ── Render ──
  // The form is laid out as a flex column whose middle child (the
  // field group) scrolls and whose last child (the action bar) is
  // anchored at the bottom — Save / Discard stay visible regardless
  // of how far the user has scrolled, without pulling the bar out
  // of the form via portal. Works inline (route aside / tab pane)
  // and inside Sheet wrappers as long as the parent gives the form
  // a defined height.
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
                  id: "performer_image",
                  defaultMessage: "Performer Image",
                })}
                value={field.state.value}
                onChange={field.handleChange}
                existingImagePath={performer?.image_path}
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

          {/* Aliases — placed directly after Name so the most-likely
            secondary identity field is visible without scrolling past
            demographics. */}
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

          {/* Disambiguation */}
          <form.Field name="disambiguation">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "disambiguation",
                    defaultMessage: "Disambiguation",
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

          {/* Gender */}
          <form.Field name="gender">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "gender",
                    defaultMessage: "Gender",
                  })}
                </FieldLabel>
                <Select
                  value={field.state.value as string}
                  onValueChange={(v) =>
                    field.handleChange(v as GenderEnum | "")
                  }
                  disabled={busy}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue>
                      {
                        genderOptions.find((o) => o.value === field.state.value)
                          ?.label
                      }
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {genderOptions.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}
          </form.Field>

          {/* Birthdate */}
          <form.Field name="birthdate">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "birthdate",
                    defaultMessage: "Birthdate",
                  })}
                </FieldLabel>
                <DatePicker
                  value={field.state.value}
                  onChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Death date */}
          <form.Field name="death_date">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "death_date",
                    defaultMessage: "Death date",
                  })}
                </FieldLabel>
                <DatePicker
                  value={field.state.value}
                  onChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Country */}
          <form.Field name="country">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "country",
                    defaultMessage: "Country",
                  })}
                </FieldLabel>
                <CountrySelect
                  id={field.name}
                  value={field.state.value}
                  onChange={field.handleChange}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Ethnicity */}
          <form.Field name="ethnicity">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "ethnicity",
                    defaultMessage: "Ethnicity",
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

          {/* Details / bio */}
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

          {/* Hair colour */}
          <form.Field name="hair_color">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "hair_color",
                    defaultMessage: "Hair colour",
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

          {/* Eye colour */}
          <form.Field name="eye_color">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "eye_color",
                    defaultMessage: "Eye colour",
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

          {/* Height */}
          <form.Field name="height_cm">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "height",
                    defaultMessage: "Height (cm)",
                  })}
                </FieldLabel>
                <Input
                  id={field.name}
                  name={field.name}
                  type="number"
                  min={0}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Weight */}
          <form.Field name="weight">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "weight",
                    defaultMessage: "Weight (kg)",
                  })}
                </FieldLabel>
                <Input
                  id={field.name}
                  name={field.name}
                  type="number"
                  min={0}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  disabled={busy}
                />
              </Field>
            )}
          </form.Field>

          {/* Measurements */}
          <form.Field name="measurements">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "measurements",
                    defaultMessage: "Measurements",
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

          {/* Penis length */}
          <form.Field name="penis_length">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "penis_length",
                    defaultMessage: "Penis length (cm)",
                  })}
                </FieldLabel>
                <Input
                  id={field.name}
                  name={field.name}
                  type="number"
                  min={0}
                  step="0.1"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  disabled={busy}
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
                <Select
                  value={field.state.value as string}
                  onValueChange={(v) =>
                    field.handleChange(v as CircumcisedEnum | "")
                  }
                  disabled={busy}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue>
                      {
                        circumcisedOptions.find(
                          (o) => o.value === field.state.value,
                        )?.label
                      }
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {circumcisedOptions.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}
          </form.Field>

          {/* Fake tits */}
          <form.Field name="fake_tits">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "fake_tits",
                    defaultMessage: "Fake tits",
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

          {/* Career start */}
          <form.Field name="career_start">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "career_start",
                    defaultMessage: "Career start",
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

          {/* Career end */}
          <form.Field name="career_end">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "career_end",
                    defaultMessage: "Career end",
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

          {/* Tattoos */}
          <form.Field name="tattoos">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "tattoos",
                    defaultMessage: "Tattoos",
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

          {/* Piercings */}
          <form.Field name="piercings">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {intl.formatMessage({
                    id: "piercings",
                    defaultMessage: "Piercings",
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
                  searchType="performer"
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

      {/* ── Action bar ── flex sibling pinned to the bottom of the
          form's column. Save / Discard stay visible without sticky
          positioning; the scrape menu rides along here too (replaces
          the previous top-of-form row). Delete is intentionally
          absent — the entity actions menu in the toolbar remains
          the canonical delete affordance, freeing this bar for the
          always-on save/discard pair. All three buttons sit in one
          flex row at uniform gap rather than `justify-between`, so
          the visual rhythm doesn't break with a wide hole between
          Discard and Scrape. */}
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
        {hasAnyScrapeSource && (
          <ScraperMenu
            scrapers={scrapers}
            stashBoxes={stashBoxes}
            onPick={handleScraperPick}
            disabled={busy}
          />
        )}
      </div>
      {performer && (
        <DeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          entityName={performer.name}
          onConfirm={handleConfirmedDelete}
        />
      )}
      <PerformerSearchDialog
        open={searchOpen}
        onOpenChange={setSearchOpen}
        source={scrapeSearchSource}
        initialQuery={form.state.values.name}
        onSelect={handleSearchSelect}
      />
      <PerformerScrapeMergeDialog
        open={mergeOpen}
        onOpenChange={setMergeOpen}
        current={mergeSnapshot ?? form.state.values}
        existingImagePath={performer?.image_path}
        scraped={mergePayload}
        source={mergeSource}
        onApply={applyScrapePatch}
      />
    </form>
  );
}
