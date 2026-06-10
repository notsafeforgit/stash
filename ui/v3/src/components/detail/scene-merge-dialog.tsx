import { useEffect, useMemo, useRef, useState, useCallback } from "react";
import { useForm } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import { useNavigate } from "@tanstack/react-router";
import { useApolloClient, useQuery } from "@apollo/client/react";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import { z } from "zod";
import { GitMerge } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import { Button } from "src/components/ui/button";
import { Checkbox } from "src/components/ui/checkbox";
import { Spinner } from "src/components/ui/spinner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "src/components/ui/field";
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxGroup,
  ComboboxInput,
  ComboboxItem,
  ComboboxLabel,
  ComboboxList,
  ComboboxSeparator,
} from "src/components/ui/combobox";
import { Separator } from "src/components/ui/separator";
import { useDebounce } from "src/hooks/debounce";
import { useToast } from "src/hooks/toast";
import { SCENE_MERGE_FIELDS } from "src/components/detail/merge/scene-merge-fields";
import {
  useMergeResolution,
  type SourceRef,
} from "src/components/detail/merge/use-merge-resolution";
import { MergeResolutionPanel } from "src/components/detail/merge/merge-resolution-panel";
import type { MergeChoice } from "src/components/detail/merge/merge-types";

interface SceneMergeSource {
  id: string;
  title?: string | null;
  date?: string | null;
  paths?: { screenshot?: string | null } | null;
  studio?: { name: string } | null;
  performers?: Array<{ name: string }>;
}

interface SceneMergeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * One or more source scenes — their data is merged into the chosen
   * destination and the source scenes are deleted by the backend.
   */
  sources: SceneMergeSource[];
}

interface SceneOption {
  id: string;
  label: string;
  thumbnail?: string | null;
  studioName?: string | null;
  performerNames: string[];
  date?: string | null;
}

export function SceneMergeDialog({
  open,
  onOpenChange,
  sources,
}: SceneMergeDialogProps) {
  const sourceIds = useMemo(() => sources.map((s) => s.id), [sources]);
  const sourceIdSet = useMemo(() => new Set(sourceIds), [sourceIds]);
  const isBulk = sources.length > 1;
  const intl = useIntl();
  const toast = useToast();
  const navigate = useNavigate();
  const client = useApolloClient();
  const [mergeScene] = useEntityMutation(GQL.SceneMergeDocument);

  // ── Search UI state (transient — not part of form data) ────────────────────
  // We let Base UI Combobox manage `inputValue` itself (via `itemToStringLabel`
  // for the selected value, plus the user's typing). We only react to
  // `onInputValueChange` to drive the server query for the async search.
  const [serverQuery, setServerQuery] = useState("");
  const debouncedSetServerQuery = useDebounce(setServerQuery, 250);

  const { data, loading } = useQuery(GQL.FindScenesDocument, {
    variables: {
      filter: {
        q: serverQuery,
        page: 1,
        per_page: 20,
      },
    },
    skip: !open || serverQuery.length === 0,
    fetchPolicy: "cache-and-network",
  });

  const options: SceneOption[] = useMemo(() => {
    const scenes = data?.findScenes.scenes ?? [];
    return scenes
      .filter((s) => !sourceIdSet.has(s.id))
      .map((s) => ({
        id: s.id,
        label:
          objectTitle(s) ||
          intl.formatMessage(
            { id: "scene_n", defaultMessage: "Scene #{id}" },
            { id: s.id },
          ),
        thumbnail: s.paths?.screenshot ?? null,
        studioName: s.studio?.name ?? null,
        performerNames: s.performers?.map((p) => p.name) ?? [],
        date: s.date ?? null,
      }));
  }, [data, sourceIdSet, intl]);

  // Source scenes as quick-pick options (bulk mode only). The user can pick
  // one of the selected scenes as the destination; the others get merged
  // into it. Falls back to the source's id when no title is available.
  const sourceOptions: SceneOption[] = useMemo(
    () =>
      sources.map((s) => ({
        id: s.id,
        label:
          s.title ||
          intl.formatMessage(
            { id: "scene_n", defaultMessage: "Scene #{id}" },
            { id: s.id },
          ),
        thumbnail: s.paths?.screenshot ?? null,
        studioName: s.studio?.name ?? null,
        performerNames: s.performers?.map((p) => p.name) ?? [],
        date: s.date ?? null,
      })),
    [sources, intl],
  );

  // Cached id→label map so the selected scene's label survives even after
  // the search results refetch. `itemToStringLabel` reads from this so
  // Base UI can render the picked scene's title in the input even when
  // it's no longer in the current results array.
  const knownLabelsRef = useRef<Map<string, string>>(new Map());
  for (const opt of options) {
    knownLabelsRef.current.set(opt.id, opt.label);
  }
  for (const opt of sourceOptions) {
    knownLabelsRef.current.set(opt.id, opt.label);
  }
  const itemToStringLabel = useCallback(
    (id: string) => knownLabelsRef.current.get(id) ?? id,
    [],
  );

  // Pass the visible item ids to the root so Base UI can compute
  // `filteredItems` and only render <ComboboxEmpty> when the list really is
  // empty. We disable the internal filter (`filter={null}`) because the
  // results are already filtered server-side via `serverQuery`.
  const allItemIds = useMemo(
    () => [...sourceOptions.map((o) => o.id), ...options.map((o) => o.id)],
    [sourceOptions, options],
  );

  // ── Form ───────────────────────────────────────────────────────────────────
  const formSchema = useMemo(() => {
    const destinationField = z.string().min(
      1,
      intl.formatMessage({
        id: "dialogs.merge_scene.destination_required",
        defaultMessage: "Pick a destination scene",
      }),
    );
    // Single source: destination can't be that scene (merging into itself
    // is a no-op). Bulk: any source can be the destination — the others
    // get merged into it, which is the most common bulk-merge intent.
    const validated = isBulk
      ? destinationField
      : destinationField.refine((id) => !sourceIdSet.has(id), {
          message: intl.formatMessage({
            id: "dialogs.merge_scene.destination_self",
            defaultMessage: "Pick a different scene",
          }),
        });
    return z.object({
      destinationId: validated,
      includePlayHistory: z.boolean(),
      includeOHistory: z.boolean(),
    });
  }, [intl, sourceIdSet, isBulk]);

  // ── Per-field resolution state ─────────────────────────────────────────────
  // Choices live outside the form because they're always valid by
  // construction (each is one of "keep" / "source:<id>" / "combine"
  // produced by the row UI itself). Reset when the destination
  // changes — different destination → different conflict set.
  const [choices, setChoices] = useState<Record<string, MergeChoice>>({});

  // Full data for destination + sources is needed to compute the
  // conflict rows. The list-card props only carry a thin slice; pull
  // the rest via a single FindScenes call once a destination is
  // picked. Skip until then so we don't fire a query for the typical
  // "open dialog, immediately cancel" case.
  const [destinationId, setDestinationId] = useState("");
  const fullDataIds = useMemo(() => {
    if (!destinationId) return [] as number[];
    const ids = new Set<string>([destinationId, ...sourceIds]);
    return Array.from(ids)
      .map((id) => parseInt(id, 10))
      .filter((n) => Number.isFinite(n));
  }, [destinationId, sourceIds]);
  const { data: fullData, loading: fullDataLoading } = useQuery(
    GQL.FindScenesDocument,
    {
      variables: { scene_ids: fullDataIds },
      skip: fullDataIds.length === 0,
      fetchPolicy: "cache-and-network",
    },
  );
  const fullScenesById = useMemo(() => {
    const m = new Map<string, GQL.SlimSceneDataFragment>();
    for (const s of fullData?.findScenes.scenes ?? []) m.set(s.id, s);
    return m;
  }, [fullData]);
  const destinationScene = fullScenesById.get(destinationId) ?? null;
  const sourceRefs = useMemo<SourceRef<GQL.SlimSceneDataFragment>[]>(() => {
    return sourceIds
      .filter((id) => id !== destinationId)
      .map((id) => fullScenesById.get(id))
      .filter((s): s is GQL.SlimSceneDataFragment => !!s)
      .map((s) => ({
        id: s.id,
        entity: s,
        label:
          objectTitle(s) ||
          intl.formatMessage(
            { id: "scene_n", defaultMessage: "Scene #{id}" },
            { id: s.id },
          ),
      }));
  }, [fullScenesById, sourceIds, destinationId, intl]);
  const { rows, applyResolutions } = useMergeResolution({
    fields: SCENE_MERGE_FIELDS,
    destination: destinationScene,
    sources: sourceRefs,
  });
  // Re-seed choices whenever the row set changes (destination pick,
  // bulk-source set change). Leaves explicit overrides in place
  // across unrelated edits.
  useEffect(() => {
    setChoices((prev) => {
      const next: Record<string, MergeChoice> = {};
      for (const row of rows) {
        next[row.field.key] = prev[row.field.key] ?? row.defaultChoice;
      }
      return next;
    });
  }, [rows]);

  const form = useForm({
    defaultValues: {
      destinationId: "",
      includePlayHistory: false,
      includeOHistory: false,
    },
    validators: { onChange: formSchema, onSubmit: formSchema },
    onSubmit: async ({ value }) => {
      try {
        // If the destination is one of the sources (bulk "merge into this
        // one" flow), exclude it from the source list — otherwise the
        // backend would try to merge it into itself.
        const finalSourceIds = sourceIds.filter(
          (id) => id !== value.destinationId,
        );
        // Project per-field choices into a SceneUpdateInput. The
        // backend treats this as an override layer: anything we set
        // overwrites the destination, anything we leave alone keeps
        // the destination's existing value.
        const updateValues: GQL.SceneUpdateInput = { id: value.destinationId };
        applyResolutions(updateValues, choices);
        const result = await mergeScene({
          variables: {
            input: {
              destination: value.destinationId,
              source: finalSourceIds,
              play_history: value.includePlayHistory,
              o_history: value.includeOHistory,
              values: updateValues,
            },
          },
        });
        const mergedId = result.data?.sceneMerge?.id;
        if (!mergedId) throw new Error("Merge failed");

        // Source scenes (excluding any destination picked from the
        // selection) are destroyed by the backend — drop them from
        // cached findScenes results so list views update without a
        // refetch. Also evict the destination so its merged data
        // refetches on the next read.
        removeEntitiesFromCache({
          cache: client.cache,
          typename: "Scene",
          listFieldName: "findScenes",
          itemsField: "scenes",
          ids: finalSourceIds,
        });
        client.cache.evict({
          id: client.cache.identify({ __typename: "Scene", id: mergedId }),
        });
        client.cache.gc();

        toast.success(
          intl.formatMessage({
            id: "toast.merged_scenes",
            defaultMessage: "Merged scenes",
          }),
        );
        onOpenChange(false);
        navigate({
          to: "/scenes/$sceneId",
          params: { sceneId: mergedId },
          replace: true,
        });
      } catch (e) {
        toast.error(e);
      }
    },
  });

  // Reset whenever the dialog opens
  useEffect(() => {
    if (open) {
      setServerQuery("");
      setDestinationId("");
      setChoices({});
      form.reset();
    }
  }, [open, form]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        // Override the default `sm:max-w-sm` — the per-field
        // resolution panel needs room for side-by-side previews on
        // multi-source merges. Height is capped with `overflow-auto`
        // on the form so the toolbar + footer stay visible while the
        // conflict rows scroll.
        className="sm:max-w-2xl"
      >
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: "actions.merge",
              defaultMessage: "Merge",
            })}
          </DialogTitle>
          <DialogDescription>
            {isBulk ? (
              <form.Subscribe selector={(s) => s.values.destinationId}>
                {(destinationId) => {
                  // When the picked destination is one of the sources, the
                  // backend keeps it and merges the other (N-1) into it.
                  // When the destination is something else (search result),
                  // all N sources get merged into it.
                  const keepingFromSelection = sourceIdSet.has(destinationId);
                  if (keepingFromSelection) {
                    return intl.formatMessage(
                      {
                        id: "dialogs.merge_scene.description_bulk_keep",
                        defaultMessage:
                          "{count, plural, one {The other scene} other {The other # scenes}} will be merged into the chosen scene and deleted.",
                      },
                      { count: sourceIds.length - 1 },
                    );
                  }
                  return intl.formatMessage(
                    {
                      id: "dialogs.merge_scene.description_bulk_external",
                      defaultMessage:
                        "All {count} selected scenes will be merged into the destination and deleted.",
                    },
                    { count: sourceIds.length },
                  );
                }}
              </form.Subscribe>
            ) : (
              intl.formatMessage({
                id: "dialogs.merge_scene.description",
                defaultMessage:
                  "Merge this scene into another. The source scene will be deleted and its data added to the destination.",
              })
            )}
          </DialogDescription>
        </DialogHeader>

        <form
          id="scene-merge-form"
          onSubmit={(e) => {
            e.preventDefault();
            form.handleSubmit();
          }}
          className="overflow-y-auto max-h-[calc(100vh-12rem)]"
        >
          <FieldGroup>
            <form.Field name="destinationId">
              {(field) => {
                const isInvalid =
                  field.state.meta.isTouched && !field.state.meta.isValid;
                return (
                  <Field data-invalid={isInvalid}>
                    <FieldLabel htmlFor={field.name}>
                      {isBulk
                        ? intl.formatMessage({
                            id: "dialogs.merge_scene.destination_bulk",
                            defaultMessage: "Scene to keep",
                          })
                        : intl.formatMessage({
                            id: "dialogs.merge_scene.destination",
                            defaultMessage: "Destination scene",
                          })}
                    </FieldLabel>
                    <Combobox<string>
                      value={field.state.value || null}
                      onValueChange={(id) => {
                        if (!id) {
                          field.handleChange("");
                          setDestinationId("");
                          setChoices({});
                          setServerQuery("");
                          return;
                        }
                        const label =
                          options.find((o) => o.id === id)?.label ??
                          knownLabelsRef.current.get(id) ??
                          id;
                        knownLabelsRef.current.set(id, label);
                        field.handleChange(id);
                        setDestinationId(id);
                      }}
                      items={allItemIds}
                      filter={null}
                      itemToStringLabel={itemToStringLabel}
                      onInputValueChange={(v) => {
                        debouncedSetServerQuery(v);
                      }}
                      onOpenChange={(o) => {
                        if (o && !serverQuery) debouncedSetServerQuery("");
                      }}
                    >
                      <ComboboxInput
                        id={field.name}
                        placeholder={intl.formatMessage({
                          id: "actions.search",
                          defaultMessage: "Search…",
                        })}
                        showClear={!!field.state.value}
                        onBlur={field.handleBlur}
                        aria-invalid={isInvalid}
                      />
                      <ComboboxContent>
                        <ComboboxList>
                          {isBulk && (
                            <>
                              <ComboboxGroup>
                                <ComboboxLabel>
                                  {intl.formatMessage({
                                    id: "dialogs.merge_scene.from_selection",
                                    defaultMessage: "From selection",
                                  })}
                                </ComboboxLabel>
                                {sourceOptions.map((opt) => (
                                  <ComboboxItem key={opt.id} value={opt.id}>
                                    <SceneOptionItem option={opt} />
                                  </ComboboxItem>
                                ))}
                              </ComboboxGroup>
                              {options.length > 0 && <ComboboxSeparator />}
                            </>
                          )}
                          {options.length > 0 && (
                            <ComboboxGroup>
                              {isBulk && (
                                <ComboboxLabel>
                                  {intl.formatMessage({
                                    id: "dialogs.merge_scene.search_results",
                                    defaultMessage: "Search results",
                                  })}
                                </ComboboxLabel>
                              )}
                              {options.map((opt) => (
                                <ComboboxItem key={opt.id} value={opt.id}>
                                  <SceneOptionItem option={opt} />
                                </ComboboxItem>
                              ))}
                            </ComboboxGroup>
                          )}
                          <ComboboxEmpty>
                            {loading
                              ? intl.formatMessage({
                                  id: "searching",
                                  defaultMessage: "Searching…",
                                })
                              : serverQuery.length === 0
                                ? isBulk
                                  ? intl.formatMessage({
                                      id: "dialogs.merge_scene.type_to_search_more",
                                      defaultMessage:
                                        "Type to search other scenes",
                                    })
                                  : intl.formatMessage({
                                      id: "type_to_search",
                                      defaultMessage: "Type to search",
                                    })
                                : intl.formatMessage({
                                    id: "no_results_found",
                                    defaultMessage: "No results found",
                                  })}
                          </ComboboxEmpty>
                        </ComboboxList>
                      </ComboboxContent>
                    </Combobox>
                    {/* Reserve a constant-height slot for the validation
                        error so toggling it on/off doesn't change the
                        dialog's height (Base UI re-centers vertically on
                        height changes — without this the dialog visibly
                        jumps when the user picks / clears a scene). */}
                    <div className="min-h-5 text-sm">
                      {isInvalid && (
                        <FieldError errors={field.state.meta.errors} />
                      )}
                    </div>
                  </Field>
                );
              }}
            </form.Field>

            {destinationId && destinationScene && (
              <>
                <Separator />
                {fullDataLoading && rows.length === 0 ? (
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Spinner className="size-4" />
                    {intl.formatMessage({
                      id: "loading.generic",
                      defaultMessage: "Loading…",
                    })}
                  </div>
                ) : (
                  <MergeResolutionPanel
                    rows={rows}
                    sources={sourceRefs}
                    choices={choices}
                    onChoiceChange={(key, next) =>
                      setChoices((prev) => ({ ...prev, [key]: next }))
                    }
                  />
                )}
              </>
            )}

            <FieldGroup data-slot="checkbox-group">
              <form.Field name="includePlayHistory">
                {(field) => (
                  <Field orientation="horizontal">
                    <Checkbox
                      id={field.name}
                      checked={field.state.value}
                      onCheckedChange={(c) => field.handleChange(c === true)}
                    />
                    <FieldLabel htmlFor={field.name}>
                      {intl.formatMessage({
                        id: "dialogs.merge_scene.include_play_history",
                        defaultMessage: "Include play history",
                      })}
                    </FieldLabel>
                  </Field>
                )}
              </form.Field>
              <form.Field name="includeOHistory">
                {(field) => (
                  <Field orientation="horizontal">
                    <Checkbox
                      id={field.name}
                      checked={field.state.value}
                      onCheckedChange={(c) => field.handleChange(c === true)}
                    />
                    <FieldLabel htmlFor={field.name}>
                      {intl.formatMessage({
                        id: "dialogs.merge_scene.include_o_history",
                        defaultMessage: "Include O history",
                      })}
                    </FieldLabel>
                  </Field>
                )}
              </form.Field>
              <FieldDescription>
                {intl.formatMessage({
                  id: "dialogs.merge_scene.history_desc",
                  defaultMessage:
                    "Carry the source scene's play / O timestamps onto the destination.",
                })}
              </FieldDescription>
            </FieldGroup>
          </FieldGroup>
        </form>

        <DialogFooter>
          <form.Subscribe
            selector={(s) => ({
              canSubmit: s.canSubmit,
              isSubmitting: s.isSubmitting,
            })}
          >
            {({ canSubmit, isSubmitting }) => (
              <>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={isSubmitting}
                  onClick={() => onOpenChange(false)}
                >
                  {intl.formatMessage({
                    id: "actions.cancel",
                    defaultMessage: "Cancel",
                  })}
                </Button>
                <Button
                  type="submit"
                  form="scene-merge-form"
                  size="sm"
                  disabled={!canSubmit || isSubmitting}
                >
                  {isSubmitting ? <Spinner className="size-4" /> : <GitMerge />}
                  {intl.formatMessage({
                    id: "actions.merge",
                    defaultMessage: "Merge",
                  })}
                </Button>
              </>
            )}
          </form.Subscribe>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SceneOptionItem({ option }: { option: SceneOption }) {
  const meta = [option.studioName, option.date].filter(Boolean).join(" · ");
  const performers = option.performerNames.slice(0, 4).join(", ");
  const overflow =
    option.performerNames.length > 4
      ? ` +${option.performerNames.length - 4}`
      : "";
  return (
    <div className="flex items-center gap-3 min-w-0 w-full">
      <div className="shrink-0 w-16 h-9 rounded bg-muted overflow-hidden">
        {option.thumbnail ? (
          <img
            src={option.thumbnail}
            alt=""
            loading="lazy"
            className="w-full h-full object-cover"
          />
        ) : null}
      </div>
      <div className="flex flex-col min-w-0 leading-tight">
        <span className="truncate font-medium">{option.label}</span>
        {meta && (
          <span className="truncate text-xs text-muted-foreground">{meta}</span>
        )}
        {performers && (
          <span className="truncate text-xs text-muted-foreground">
            {performers}
            {overflow}
          </span>
        )}
      </div>
    </div>
  );
}
