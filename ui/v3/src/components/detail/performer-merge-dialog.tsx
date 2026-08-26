import { useEffect, useMemo, useRef, useState, useCallback } from "react";
import { useForm } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import { useNavigate } from "@tanstack/react-router";
import { useApolloClient, useMutation, useQuery } from "@apollo/client/react";
import { removeEntitiesFromCache } from "src/core/client";
import { z } from "zod";
import { GitMerge, User } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
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
import { PERFORMER_MERGE_FIELDS } from "src/components/detail/merge/performer-merge-fields";
import {
  useMergeResolution,
  type SourceRef,
} from "src/components/detail/merge/use-merge-resolution";
import { MergeResolutionPanel } from "src/components/detail/merge/merge-resolution-panel";
import type { MergeChoice } from "src/components/detail/merge/merge-types";

export interface PerformerMergeSource {
  id: string;
  name: string;
  disambiguation?: string | null;
  image_path?: string | null;
  birthdate?: string | null;
  death_date?: string | null;
}

interface PerformerMergeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * One or more source performers — their data is merged into the chosen
   * destination performer and the source performers are deleted.
   */
  sources: PerformerMergeSource[];
}

interface PerformerOption {
  id: string;
  label: string;
  thumbnail?: string | null;
  disambiguation?: string | null;
  birthdate?: string | null;
  death_date?: string | null;
}

function performerLabel(name: string, disambiguation?: string | null) {
  return disambiguation ? `${name} (${disambiguation})` : name;
}

export function PerformerMergeDialog({
  open,
  onOpenChange,
  sources,
}: PerformerMergeDialogProps) {
  const sourceIds = useMemo(() => sources.map((s) => s.id), [sources]);
  const sourceIdSet = useMemo(() => new Set(sourceIds), [sourceIds]);
  const isBulk = sources.length > 1;
  const intl = useIntl();
  const toast = useToast();
  const navigate = useNavigate();
  const client = useApolloClient();
  // A merge destroys the source performer, so the global entity-mutation
  // policy of refetching every active query is counterproductive here: it
  // waits on detail/list queries that still target the deleted source before
  // this dialog can close. The merge response plus the explicit cache cleanup
  // below is sufficient; the destination route then fetches its evicted row.
  const [mergePerformer] = useMutation(GQL.PerformerMergeDocument);

  const [serverQuery, setServerQuery] = useState("");
  const debouncedSetServerQuery = useDebounce(setServerQuery, 250);

  const { data, loading } = useQuery(GQL.FindPerformersForSelectDocument, {
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

  const options: PerformerOption[] = useMemo(() => {
    const performers = data?.findPerformers.performers ?? [];
    return performers
      .filter((p) => !sourceIdSet.has(p.id))
      .map((p) => ({
        id: p.id,
        label: performerLabel(p.name, p.disambiguation),
        thumbnail: p.image_path ?? null,
        disambiguation: p.disambiguation ?? null,
        birthdate: p.birthdate ?? null,
        death_date: p.death_date ?? null,
      }));
  }, [data, sourceIdSet]);

  const sourceOptions: PerformerOption[] = useMemo(
    () =>
      sources.map((s) => ({
        id: s.id,
        label: performerLabel(s.name, s.disambiguation),
        thumbnail: s.image_path ?? null,
        disambiguation: s.disambiguation ?? null,
        birthdate: s.birthdate ?? null,
        death_date: s.death_date ?? null,
      })),
    [sources],
  );

  const knownLabelsRef = useRef<Map<string, string>>(new Map());
  for (const opt of options) knownLabelsRef.current.set(opt.id, opt.label);
  for (const opt of sourceOptions)
    knownLabelsRef.current.set(opt.id, opt.label);
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

  const formSchema = useMemo(() => {
    const destinationField = z.string().min(
      1,
      intl.formatMessage({
        id: "dialogs.merge_performer.destination_required",
        defaultMessage: "Pick a destination performer",
      }),
    );
    const validated = isBulk
      ? destinationField
      : destinationField.refine((id) => !sourceIdSet.has(id), {
          message: intl.formatMessage({
            id: "dialogs.merge_performer.destination_self",
            defaultMessage: "Pick a different performer",
          }),
        });
    return z.object({ destinationId: validated });
  }, [intl, sourceIdSet, isBulk]);

  // ── Per-field resolution state ─────────────────────────────────────────────
  // See scene-merge-dialog.tsx for the rationale — choices live in
  // local state (not the form) because they're always valid by
  // construction.
  const [choices, setChoices] = useState<Record<string, MergeChoice>>({});
  const [destinationId, setDestinationId] = useState("");
  const fullDataIds = useMemo(() => {
    if (!destinationId) return [] as number[];
    const ids = new Set<string>([destinationId, ...sourceIds]);
    return Array.from(ids)
      .map((id) => parseInt(id, 10))
      .filter((n) => Number.isFinite(n));
  }, [destinationId, sourceIds]);
  const { data: fullData, loading: fullDataLoading } = useQuery(
    GQL.FindPerformersDocument,
    {
      variables: { performer_ids: fullDataIds },
      skip: fullDataIds.length === 0,
      fetchPolicy: "cache-and-network",
    },
  );
  const fullPerformersById = useMemo(() => {
    const m = new Map<string, GQL.PerformerDataFragment>();
    for (const p of fullData?.findPerformers.performers ?? []) m.set(p.id, p);
    return m;
  }, [fullData]);
  const destinationPerformer = fullPerformersById.get(destinationId) ?? null;
  const sourceRefs = useMemo<SourceRef<GQL.PerformerDataFragment>[]>(() => {
    return sourceIds
      .filter((id) => id !== destinationId)
      .map((id) => fullPerformersById.get(id))
      .filter((p): p is GQL.PerformerDataFragment => !!p)
      .map((p) => ({
        id: p.id,
        entity: p,
        label: performerLabel(p.name, p.disambiguation),
      }));
  }, [fullPerformersById, sourceIds, destinationId]);
  const { rows, applyResolutions } = useMergeResolution({
    fields: PERFORMER_MERGE_FIELDS,
    destination: destinationPerformer,
    sources: sourceRefs,
    projectKeepValues: true,
  });
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
    defaultValues: { destinationId: "" },
    validators: { onChange: formSchema, onSubmit: formSchema },
    onSubmit: async ({ value }) => {
      try {
        const finalSourceIds = sourceIds.filter(
          (id) => id !== value.destinationId,
        );
        const updateValues: GQL.PerformerUpdateInput = {
          id: value.destinationId,
        };
        applyResolutions(updateValues, choices);
        // Always fold every merged performer's name (destination + each
        // source) into the destination's aliases, except the chosen
        // final name. Without this the source performers' names — the
        // very thing the merge is collapsing — would be silently lost.
        if (destinationPerformer) {
          const finalNameLC = (updateValues.name ?? destinationPerformer.name)
            .trim()
            .toLowerCase();
          const namePolicies = new Map<string, boolean>();
          for (const performer of [
            destinationPerformer,
            ...sourceRefs.map((source) => source.entity),
          ]) {
            const canonicalKey = performer.name.trim().toLowerCase();
            namePolicies.set(
              canonicalKey,
              (namePolicies.get(canonicalKey) ?? false) ||
                performer.ignore_primary_name_auto_tag,
            );
            for (const alias of performer.aliases ?? []) {
              const key = alias.alias.trim().toLowerCase();
              namePolicies.set(
                key,
                (namePolicies.get(key) ?? false) || alias.ignore_auto_tag,
              );
            }
          }
          const baseAliases =
            updateValues.aliases ??
            destinationPerformer.aliases?.map((a) => ({
              alias: a.alias,
              ignore_auto_tag: a.ignore_auto_tag,
            })) ??
            [];
          const seen = new Set<string>();
          const merged: { alias: string; ignore_auto_tag: boolean }[] = [];
          for (const a of baseAliases) {
            const k = a.alias.trim().toLowerCase();
            if (!k || k === finalNameLC || seen.has(k)) continue;
            seen.add(k);
            merged.push({
              alias: a.alias,
              ignore_auto_tag:
                a.ignore_auto_tag || (namePolicies.get(k) ?? false),
            });
          }
          for (const [name, ignoreAutoTag] of [
            [
              destinationPerformer.name,
              destinationPerformer.ignore_primary_name_auto_tag,
            ] as const,
            ...sourceRefs.map(
              (source) =>
                [
                  source.entity.name,
                  source.entity.ignore_primary_name_auto_tag,
                ] as const,
            ),
          ]) {
            const trimmed = name.trim();
            const k = trimmed.toLowerCase();
            if (!trimmed || k === finalNameLC || seen.has(k)) continue;
            seen.add(k);
            merged.push({ alias: trimmed, ignore_auto_tag: ignoreAutoTag });
          }
          updateValues.ignore_primary_name_auto_tag =
            namePolicies.get(finalNameLC) ?? false;
          updateValues.aliases = merged;
        }
        const result = await mergePerformer({
          variables: {
            input: {
              destination: value.destinationId,
              source: finalSourceIds,
              values: updateValues,
              require_resolved_values: true,
            },
          },
        });
        const mergedId = result.data?.performerMerge?.id;
        if (!mergedId) throw new Error("Merge failed");

        // Drop the destroyed source performers from cached
        // findPerformers results so list views update immediately.
        // Also evict the merged destination so its consolidated data
        // refetches.
        removeEntitiesFromCache({
          cache: client.cache,
          typename: "Performer",
          listFieldName: "findPerformers",
          itemsField: "performers",
          ids: finalSourceIds,
        });
        client.cache.evict({
          id: client.cache.identify({ __typename: "Performer", id: mergedId }),
        });
        client.cache.gc();

        toast.success(
          intl.formatMessage({
            id: "toast.merged_performers",
            defaultMessage: "Merged performers",
          }),
        );
        onOpenChange(false);
        navigate({
          to: "/performers/$performerId",
          params: { performerId: mergedId },
          replace: true,
        });
      } catch (e) {
        toast.error(e);
      }
    },
  });

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
        // Wider than the default `sm:max-w-sm` so the per-field
        // resolution rows have room for side-by-side previews.
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
                  const keepingFromSelection = sourceIdSet.has(destinationId);
                  if (keepingFromSelection) {
                    return intl.formatMessage(
                      {
                        id: "dialogs.merge_performer.description_bulk_keep",
                        defaultMessage:
                          "{count, plural, one {The other performer} other {The other # performers}} will be merged into the chosen performer and deleted.",
                      },
                      { count: sourceIds.length - 1 },
                    );
                  }
                  return intl.formatMessage(
                    {
                      id: "dialogs.merge_performer.description_bulk_external",
                      defaultMessage:
                        "All {count} selected performers will be merged into the destination and deleted.",
                    },
                    { count: sourceIds.length },
                  );
                }}
              </form.Subscribe>
            ) : (
              intl.formatMessage({
                id: "dialogs.merge_performer.description",
                defaultMessage:
                  "Merge this performer into another. The source performer will be deleted and its data added to the destination.",
              })
            )}
          </DialogDescription>
        </DialogHeader>

        <form
          id="performer-merge-form"
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
                            id: "dialogs.merge_performer.destination_bulk",
                            defaultMessage: "Performer to keep",
                          })
                        : intl.formatMessage({
                            id: "dialogs.merge_performer.destination",
                            defaultMessage: "Destination performer",
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
                                    <PerformerOptionItem option={opt} />
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
                                  <PerformerOptionItem option={opt} />
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
                                      id: "dialogs.merge_performer.type_to_search_more",
                                      defaultMessage:
                                        "Type to search other performers",
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
                        jumps when the user picks / clears a performer). */}
                    <div className="min-h-5 text-sm">
                      {isInvalid && (
                        <FieldError errors={field.state.meta.errors} />
                      )}
                    </div>
                  </Field>
                );
              }}
            </form.Field>

            {destinationId && destinationPerformer && (
              <>
                <Separator />
                {/* Destination summary — always visible once a
                    destination is picked, regardless of whether any
                    individual field becomes a conflict row. The image
                    in particular never makes it into the resolution
                    panel when the source(s) don't supply an image
                    (a common case), so without this card the user has
                    no place at all to see what they're merging into. */}
                <DestinationPerformerSummary performer={destinationPerformer} />
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
                  form="performer-merge-form"
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

// Destination summary card shown above the resolution panel once a
// destination has been picked. Mirrors `PerformerOptionItem`'s layout
// (image + name + birthdate-range) but at a larger size so the user
// can confirm visually what they're merging into. The image preview
// here is the canonical "current image" for the merge UX — the
// per-field resolution panel only renders an image row when at least
// one source contributes one, and many merges don't, so this card is
// the only place the destination image is guaranteed to surface.
function DestinationPerformerSummary({
  performer,
}: {
  performer: GQL.PerformerDataFragment;
}) {
  const intl = useIntl();
  const meta = [performer.birthdate, performer.death_date]
    .filter(Boolean)
    .join(" – ");
  return (
    <div className="flex items-start gap-3 min-w-0 w-full rounded-md border border-border/60 bg-muted/30 p-3">
      <div className="shrink-0 w-16 h-24 rounded bg-muted overflow-hidden flex items-center justify-center text-muted-foreground">
        {performer.image_path ? (
          <img
            src={performer.image_path}
            alt=""
            className="w-full h-full object-cover object-top"
          />
        ) : (
          <User className="size-6" />
        )}
      </div>
      <div className="flex flex-col min-w-0 leading-tight gap-1">
        <span className="text-xs uppercase tracking-wide text-muted-foreground font-semibold">
          {intl.formatMessage({
            id: "dialogs.merge_performer.destination",
            defaultMessage: "Destination performer",
          })}
        </span>
        <span className="truncate font-medium">
          {performerLabel(performer.name, performer.disambiguation)}
        </span>
        {meta && (
          <span className="truncate text-xs text-muted-foreground">{meta}</span>
        )}
      </div>
    </div>
  );
}

function PerformerOptionItem({ option }: { option: PerformerOption }) {
  const meta = [option.birthdate, option.death_date]
    .filter(Boolean)
    .join(" – ");
  return (
    <div className="flex items-center gap-3 min-w-0 w-full">
      <div className="shrink-0 w-9 h-12 rounded bg-muted overflow-hidden flex items-center justify-center text-muted-foreground">
        {option.thumbnail ? (
          <img
            src={option.thumbnail}
            alt=""
            loading="lazy"
            className="w-full h-full object-cover object-top"
          />
        ) : (
          <User className="size-4" />
        )}
      </div>
      <div className="flex flex-col min-w-0 leading-tight">
        <span className="truncate font-medium">{option.label}</span>
        {meta && (
          <span className="truncate text-xs text-muted-foreground">{meta}</span>
        )}
      </div>
    </div>
  );
}
