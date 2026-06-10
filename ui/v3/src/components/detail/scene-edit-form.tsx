import { useEffect, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import { Save, RotateCcw, FileClock } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { galleryLabel } from "src/lib/gallery-utils";
import { evictQueries, useEntityMutation } from "src/core/client";
import { useToast } from "src/hooks/toast";
import {
  useAvailableSceneScrapers,
  sourceToInput,
} from "src/components/scrape/use-available-scrapers";
import type { ScrapeSource } from "src/components/scrape/use-available-scrapers";
import {
  ScraperMenu,
  type ScrapeAction,
} from "src/components/scrape/scraper-menu";
import { SceneSearchDialog } from "src/components/scrape/scene-search-dialog";
import { SceneScrapeMergeDialog } from "src/components/scrape/scene-scrape-merge-dialog";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Textarea } from "src/components/ui/textarea";
import { Switch } from "src/components/ui/switch";
import { RatingSystem } from "src/components/ui/rating-system";
import {
  Field,
  FieldLabel,
  FieldTitle,
  FieldGroup,
} from "src/components/ui/field";
import {
  type EntityOption,
  EntityMultiSelect,
  EntitySingleSelect,
} from "src/components/forms/async-entity-select";
import { UrlListField } from "src/components/forms/url-list-field";
import {
  GroupsField,
  type GroupEntry,
} from "src/components/forms/groups-field";
import {
  StashIdsField,
  type StashIdEntry,
} from "src/components/forms/stash-ids-field";
import { DatePicker } from "src/components/ui/date-picker";
import { dateToString } from "src/utils/date";
import {
  CustomFieldsField,
  type CustomFieldMap,
  customFieldsUpdateInput,
} from "src/components/forms/custom-fields-field";

// ── Types ─────────────────────────────────────────────────────────────────────

export type SceneData = NonNullable<GQL.FindSceneQuery["findScene"]>;

export interface SceneFormValues {
  title: string;
  code: string;
  director: string;
  details: string;
  date: string;
  rating100: number | null;
  organized: boolean;
  urls: string[];
  performers: EntityOption[];
  tags: EntityOption[];
  galleries: EntityOption[];
  studio: EntityOption | null;
  groups: GroupEntry[];
  stash_ids: StashIdEntry[];
  custom_fields: CustomFieldMap;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

export function emptySceneFormValues(): SceneFormValues {
  return {
    title: "",
    code: "",
    director: "",
    details: "",
    date: "",
    rating100: null,
    organized: false,
    urls: [],
    performers: [],
    tags: [],
    galleries: [],
    studio: null,
    groups: [],
    stash_ids: [],
    custom_fields: {},
  };
}

export function sceneToFormValues(scene: SceneData): SceneFormValues {
  return {
    title: scene.title ?? "",
    code: scene.code ?? "",
    director: scene.director ?? "",
    details: scene.details ?? "",
    date: scene.date ?? "",
    rating100: scene.rating100 ?? null,
    organized: scene.organized,
    urls: scene.urls,
    performers: scene.performers.map((p) => ({ id: p.id, name: p.name })),
    tags: scene.tags.map((t) => ({ id: t.id, name: t.name })),
    galleries: scene.galleries.map((g) => ({
      id: g.id,
      name: galleryLabel(g),
    })),
    studio: scene.studio
      ? { id: scene.studio.id, name: scene.studio.name }
      : null,
    groups: scene.groups.map((sg) => ({
      group_id: sg.group.id,
      group_name: sg.group.name,
      scene_index: sg.scene_index ?? null,
    })),
    stash_ids: scene.stash_ids.map((s) => ({
      endpoint: s.endpoint,
      stash_id: s.stash_id,
    })),
    custom_fields: scene.custom_fields ?? {},
  };
}

function formValuesToInput(
  id: string,
  v: SceneFormValues,
): GQL.SceneUpdateInput {
  return {
    id,
    title: v.title || null,
    code: v.code || null,
    director: v.director || null,
    details: v.details || null,
    date: v.date || null,
    rating100: v.rating100,
    organized: v.organized,
    urls: v.urls.filter(Boolean),
    performer_ids: v.performers.map((p) => p.id),
    tag_ids: v.tags.map((t) => t.id),
    gallery_ids: v.galleries.map((g) => g.id),
    studio_id: v.studio?.id ?? null,
    groups: v.groups.map((g) => ({
      group_id: g.group_id,
      scene_index: g.scene_index,
    })),
    stash_ids: v.stash_ids
      .filter((s) => s.endpoint && s.stash_id)
      .map((s) => ({ endpoint: s.endpoint, stash_id: s.stash_id })),
    custom_fields: customFieldsUpdateInput(v.custom_fields),
  };
}

function formValuesToCreateInput(v: SceneFormValues): GQL.SceneCreateInput {
  return {
    title: v.title || undefined,
    code: v.code || undefined,
    director: v.director || undefined,
    details: v.details || undefined,
    date: v.date || undefined,
    rating100: v.rating100 ?? undefined,
    organized: v.organized,
    urls: v.urls.filter(Boolean),
    performer_ids: v.performers.map((p) => p.id),
    tag_ids: v.tags.map((t) => t.id),
    gallery_ids: v.galleries.map((g) => g.id),
    studio_id: v.studio?.id ?? undefined,
    groups: v.groups.map((g) => ({
      group_id: g.group_id,
      scene_index: g.scene_index,
    })),
    stash_ids: v.stash_ids
      .filter((s) => s.endpoint && s.stash_id)
      .map((s) => ({ endpoint: s.endpoint, stash_id: s.stash_id })),
    custom_fields: v.custom_fields,
  };
}

// ── Component ─────────────────────────────────────────────────────────────────

export type SceneEditFormProps =
  | {
      mode?: "edit";
      scene: SceneData;
      /** Called after a successful save — use to close a containing sheet/dialog. */
      onSaved?: () => void;
    }
  | {
      mode: "create";
      /** Pre-fill the form. Used by the split-from-file flow to seed values
       *  from the parent scene. */
      initialValues?: Partial<SceneFormValues>;
      /** Extra fields merged into the SceneCreateInput (e.g. `file_ids` when
       *  splitting a file out of an existing scene). Form-derived fields
       *  always win over these on key collision. */
      createInputExtras?: Partial<GQL.SceneCreateInput>;
      /** Called after a successful create with the new scene's id — use to
       *  close a containing sheet/dialog and navigate. */
      onCreated?: (id: string) => void;
    };

export function SceneEditForm(props: SceneEditFormProps) {
  const isCreate = props.mode === "create";
  const scene = isCreate ? null : props.scene;
  const intl = useIntl();
  const toast = useToast();

  // ── Mutations ──
  const [updateScene, { loading: updating }] = useEntityMutation(
    GQL.SceneUpdateDocument,
  );
  const [createScene, { loading: creating }] = useEntityMutation(
    GQL.SceneCreateDocument,
  );
  const saving = updating || creating;

  // ── Scrape sources ──
  const {
    scrapers,
    stashBoxes,
    hasAny: hasAnyScrapeSource,
  } = useAvailableSceneScrapers();
  const [scrapeSearchSource, setScrapeSearchSource] =
    useState<ScrapeSource | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [mergePayload, setMergePayload] =
    useState<GQL.ScrapedSceneDataFragment | null>(null);
  const [mergeSource, setMergeSource] = useState<ScrapeSource | null>(null);
  const [mergeSnapshot, setMergeSnapshot] = useState<SceneFormValues | null>(
    null,
  );
  const [runFragmentScrape, { loading: scraping }] = useLazyQuery(
    GQL.ScrapeSingleSceneDocument,
  );

  // Date derived from the primary file's mtime, used by the "From file" button
  // next to the Date field. Null when no file is loaded yet (create mode) or
  // mtime is missing.
  const fileMTimeDate = (() => {
    const modTime = scene?.files?.[0]?.mod_time;
    if (!modTime) return null;
    const d = new Date(modTime);
    if (Number.isNaN(d.getTime())) return null;
    return dateToString(d);
  })();

  // ── Async search ──
  const [performerOptions, setPerformerOptions] = useState<EntityOption[]>([]);
  const [tagOptions, setTagOptions] = useState<EntityOption[]>([]);
  const [galleryOptions, setGalleryOptions] = useState<EntityOption[]>([]);
  const [studioOptions, setStudioOptions] = useState<EntityOption[]>([]);

  const [searchPerformers, { data: performerData, loading: performerLoading }] =
    useLazyQuery(GQL.FindPerformersDocument);
  const [searchTags, { data: tagData, loading: tagLoading }] = useLazyQuery(
    GQL.FindTagsDocument,
  );
  const [searchGalleries, { data: galleryData, loading: galleryLoading }] =
    useLazyQuery(GQL.FindGalleriesDocument);
  const [searchStudios, { data: studioData, loading: studioLoading }] =
    useLazyQuery(GQL.FindStudiosDocument);

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

  useEffect(() => {
    if (tagData) {
      setTagOptions(
        tagData.findTags.tags.map((t) => ({ id: t.id, name: t.name })),
      );
    }
  }, [tagData]);

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

  useEffect(() => {
    if (studioData) {
      setStudioOptions(
        studioData.findStudios.studios.map((s) => ({ id: s.id, name: s.name })),
      );
    }
  }, [studioData]);

  // ── Form ──
  const form = useForm({
    defaultValues: scene
      ? sceneToFormValues(scene)
      : { ...emptySceneFormValues(), ...(isCreate ? props.initialValues : {}) },
    onSubmit: async ({ value, formApi }) => {
      if (isCreate) {
        const result = await createScene({
          variables: {
            input: {
              ...formValuesToCreateInput(value),
              ...(props.createInputExtras ?? {}),
            },
          },
          update(cache) {
            evictQueries(cache, [GQL.FindScenesDocument]);
          },
        });
        const newId = result.data?.sceneCreate?.id;
        formApi.reset();
        if (newId) props.onCreated?.(newId);
      } else {
        await updateScene({
          variables: { input: formValuesToInput(props.scene.id, value) },
        });
        // Reset to the saved values so isDirty becomes false and defaultValues
        // reflect what's now on the server.
        formApi.reset(value);
        props.onSaved?.();
      }
    },
  });

  const busy = saving || scraping;

  function openMergeWith(
    scrapedScene: GQL.ScrapedSceneDataFragment,
    src: ScrapeSource,
  ) {
    setMergePayload(scrapedScene);
    setMergeSource(src);
    setMergeSnapshot(form.state.values);
    setMergeOpen(true);
  }

  async function handleScraperPick(src: ScrapeSource, action: ScrapeAction) {
    if (action === GQL.ScrapeType.Name) {
      setScrapeSearchSource(src);
      setSearchOpen(true);
      return;
    }
    // FRAGMENT scrape requires an existing scene id, so it's a no-op in
    // create mode. Surface this to the user rather than silently dropping.
    if (!scene) {
      toast.error(
        intl.formatMessage({
          id: "scrape.fragment_unavailable_create",
          defaultMessage:
            "Save the scene first — re-scrape only works on existing scenes.",
        }),
      );
      return;
    }
    try {
      const result = await runFragmentScrape({
        variables: {
          source: sourceToInput(src),
          input: { scene_id: scene.id },
        },
      });
      const hits = result.data?.scrapeSingleScene ?? [];
      if (hits.length === 0) {
        toast.error(
          intl.formatMessage({
            id: "scrape.no_match",
            defaultMessage: "No match returned by scraper.",
          }),
        );
        return;
      }
      openMergeWith(hits[0] as GQL.ScrapedSceneDataFragment, src);
    } catch (e) {
      toast.error(e);
    }
  }

  function handleSearchSelect(scrapedScene: GQL.ScrapedSceneDataFragment) {
    setSearchOpen(false);
    if (scrapeSearchSource) openMergeWith(scrapedScene, scrapeSearchSource);
  }

  function applyScrapePatch(patch: Partial<SceneFormValues>) {
    for (const [key, value] of Object.entries(patch)) {
      form.setFieldValue(
        key as keyof SceneFormValues,
        value as SceneFormValues[keyof SceneFormValues],
      );
    }
  }

  // ── Render ──
  // The form is a flex column whose middle child (the field group)
  // scrolls and whose last child (the action bar) is anchored at the
  // bottom — Save / Discard stay visible no matter how far the user
  // scrolls, without needing sticky positioning. Works inline (route
  // aside / tab pane) and inside Sheet wrappers as long as the parent
  // gives the form a defined height.
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

          {/* Groups */}
          <form.Field name="groups">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "groups",
                    defaultMessage: "Groups",
                  })}
                </FieldLabel>
                <GroupsField
                  value={field.state.value}
                  onChange={field.handleChange}
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
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
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
                  searchType="scene"
                  searchQuery={form.state.values.title}
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
      {/* ── Action bar ── flex sibling pinned at form bottom. With the
          parent providing a defined height (route aside or sheet) the
          form's `flex flex-col h-full` keeps this row anchored
          regardless of how far the user scrolls inside the field
          group. The scrape menu rides along here too — same pattern
          as `PerformerEditForm` — so the always-on save / discard
          pair is co-located with the seek-into-scrape affordance. */}
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
                disabled={busy || isSubmitting || (!isCreate && !isDirty)}
              >
                <Save />
                {intl.formatMessage(
                  isCreate
                    ? { id: "actions.create", defaultMessage: "Create" }
                    : { id: "actions.save", defaultMessage: "Save" },
                )}
              </Button>
              {!isCreate && (
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
              )}
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
      <SceneSearchDialog
        open={searchOpen}
        onOpenChange={setSearchOpen}
        source={scrapeSearchSource}
        initialQuery={form.state.values.title}
        onSelect={handleSearchSelect}
      />
      <SceneScrapeMergeDialog
        open={mergeOpen}
        onOpenChange={setMergeOpen}
        current={mergeSnapshot ?? form.state.values}
        scraped={mergePayload}
        source={mergeSource}
        onApply={applyScrapePatch}
      />
    </form>
  );
}
