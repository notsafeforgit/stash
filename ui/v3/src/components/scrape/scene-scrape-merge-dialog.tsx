import type React from "react";
import { useEffect, useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { useToast } from "src/hooks/toast";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import type { EntityOption } from "src/components/forms/async-entity-select";
import type { SceneFormValues } from "src/components/detail/scene-edit-form";
import type { GroupEntry } from "src/components/forms/groups-field";
import type { ScrapeSource } from "./use-available-scrapers";
import {
  type MergeMode,
  RowShell,
  SectionHeader,
  emptyOrText,
} from "./scrape-merge-shared";
import {
  type ScrapedItemResolution,
  defaultItemResolution,
} from "./scraped-item-row";
import { ScrapedTagRow } from "./scraped-tag-row";
import { ScrapedPerformerRow } from "./scraped-performer-row";
import { ScrapedStudioRow } from "./scraped-studio-row";
import { ScrapedGroupRow } from "./scraped-group-row";

type ScrapedScene = GQL.ScrapedSceneDataFragment;

// ── Field-row plumbing ───────────────────────────────────────────────────────

type RowRenderArgs = {
  accepted: boolean;
  setAccepted: (v: boolean) => void;
  mergeMode: MergeMode;
  setMergeMode: (m: MergeMode) => void;
};
type PreparedRow = {
  key: string;
  render: (args: RowRenderArgs) => React.ReactNode;
  apply: (patch: Partial<SceneFormValues>, mergeMode: MergeMode) => void;
};

interface SimpleStringFieldDef<K extends keyof SceneFormValues> {
  field: K;
  label: string;
  scrapedValue: string | null | undefined;
}

// ── Component ────────────────────────────────────────────────────────────────

interface SceneScrapeMergeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  current: SceneFormValues;
  scraped: ScrapedScene | null;
  /** Source the scrape came from. When this is a stash-box and the scraped
   *  payload includes a remote_site_id, the dialog offers a stash-id row. */
  source?: ScrapeSource | null;
  onApply: (patch: Partial<SceneFormValues>) => void;
}

export function SceneScrapeMergeDialog({
  open,
  onOpenChange,
  current,
  scraped,
  source,
  onApply,
}: SceneScrapeMergeDialogProps) {
  const intl = useIntl();
  const toast = useToast();

  // ── Simple string rows (title / code / director / details / date) ──
  const stringRows = useMemo<PreparedRow[]>(() => {
    if (!scraped) return [];
    const defs: SimpleStringFieldDef<keyof SceneFormValues>[] = [
      { field: "title", label: "Title", scrapedValue: scraped.title },
      { field: "code", label: "Studio code", scrapedValue: scraped.code },
      {
        field: "director",
        label: "Director",
        scrapedValue: scraped.director,
      },
      { field: "date", label: "Date", scrapedValue: scraped.date },
      { field: "details", label: "Details", scrapedValue: scraped.details },
    ];

    return defs.flatMap((def) => {
      const newValue = (def.scrapedValue ??
        "") as SceneFormValues[typeof def.field];
      const currentValue = current[def.field];
      const newDisplay = String(newValue ?? "");
      const currentDisplay = String(currentValue ?? "");
      // Skip when scraped is empty or already matches.
      if (!newDisplay) return [];
      if (newDisplay === currentDisplay) return [];

      return [
        {
          key: def.field,
          render: ({ accepted, setAccepted }) => (
            <RowShell
              key={def.field}
              label={def.label}
              accepted={accepted}
              onAcceptedChange={setAccepted}
              current={emptyOrText(currentDisplay)}
              scraped={newDisplay}
            />
          ),
          apply: (patch) => {
            (patch as Record<string, unknown>)[def.field] = newValue;
          },
        } satisfies PreparedRow,
      ];
    });
  }, [scraped, current]);

  // ── URLs (multi-value, merge or overwrite) ──
  const urlRow = useMemo<PreparedRow | null>(() => {
    const incoming = scraped?.urls ?? [];
    if (incoming.length === 0) return null;
    const existing = new Set(current.urls);
    const incomingSet = new Set(incoming);
    const additions = incoming.filter((u) => !existing.has(u));
    const sameSet =
      additions.length === 0 &&
      current.urls.length === incomingSet.size &&
      current.urls.every((u) => incomingSet.has(u));
    if (sameSet) return null;
    return {
      key: "urls",
      render: ({ accepted, setAccepted, mergeMode, setMergeMode }) => (
        <RowShell
          key="urls"
          label="URLs"
          accepted={accepted}
          onAcceptedChange={setAccepted}
          mergeMode={current.urls.length > 0 ? mergeMode : undefined}
          onMergeModeChange={current.urls.length > 0 ? setMergeMode : undefined}
          current={
            current.urls.length === 0 ? (
              emptyOrText(null)
            ) : (
              <ul className="flex flex-col gap-0.5">
                {current.urls.map((u) => (
                  <li key={u} className="break-all text-xs">
                    {u}
                  </li>
                ))}
              </ul>
            )
          }
          scraped={
            mergeMode === "overwrite" || current.urls.length === 0 ? (
              <ul className="flex flex-col gap-0.5">
                {incoming.map((u) => (
                  <li key={u} className="break-all text-xs">
                    {u}
                  </li>
                ))}
              </ul>
            ) : additions.length === 0 ? (
              <span className="italic text-muted-foreground text-xs">
                {intl.formatMessage({
                  id: "scrape.no_additions",
                  defaultMessage:
                    "Nothing to add — switch to Overwrite to drop the others.",
                })}
              </span>
            ) : (
              <ul className="flex flex-col gap-0.5">
                {additions.map((u) => (
                  <li key={u} className="break-all text-xs">
                    <span className="text-emerald-500 mr-1 select-none">+</span>
                    {u}
                  </li>
                ))}
              </ul>
            )
          }
        />
      ),
      apply: (patch, mergeMode) => {
        if (mergeMode === "overwrite") {
          patch.urls = incoming;
        } else {
          patch.urls = [...current.urls, ...additions];
        }
      },
    };
  }, [scraped, current.urls, intl]);

  // ── Stash ID row (only for stash-box sources) ──
  const stashIdRow = useMemo<PreparedRow | null>(() => {
    if (!scraped) return null;
    if (source?.kind !== "stashBox") return null;
    if (!scraped.remote_site_id) return null;
    const endpoint = source.endpoint;
    const newId = scraped.remote_site_id;
    const existing = current.stash_ids.find((s) => s.endpoint === endpoint);
    if (existing && existing.stash_id === newId) return null;

    return {
      key: "stash_ids",
      render: ({ accepted, setAccepted }) => (
        <RowShell
          key="stash_ids"
          label="Stash ID"
          accepted={accepted}
          onAcceptedChange={setAccepted}
          current={
            existing ? (
              <span className="break-all text-xs">{existing.stash_id}</span>
            ) : (
              emptyOrText(null)
            )
          }
          scraped={<span className="break-all text-xs">{newId}</span>}
        />
      ),
      apply: (patch) => {
        const next = [...current.stash_ids];
        const idx = next.findIndex((s) => s.endpoint === endpoint);
        if (idx >= 0) next[idx] = { endpoint, stash_id: newId };
        else next.push({ endpoint, stash_id: newId });
        patch.stash_ids = next;
      },
    };
  }, [scraped, source, current.stash_ids]);

  // (Scenes have no `image` field on the form — covers come from the file
  // itself. So no scraped-image row or lightbox is shown.)

  // ── Section state: per-row accept toggle, per-section merge mode ──
  const [accepted, setAccepted] = useState<Record<string, boolean>>({});
  function isAccepted(key: string): boolean {
    return accepted[key] ?? true;
  }
  function setAcceptedFor(key: string, value: boolean) {
    setAccepted((curr) => ({ ...curr, [key]: value }));
  }

  const [mergeModes, setMergeModes] = useState<Record<string, MergeMode>>({});
  function getMergeMode(key: string): MergeMode {
    return mergeModes[key] ?? "merge";
  }
  function setMergeModeFor(key: string, mode: MergeMode) {
    setMergeModes((curr) => ({ ...curr, [key]: mode }));
  }

  // ── Studio (single value) ──
  const scrapedStudio = scraped?.studio ?? null;
  // Don't surface when scraper returned no usable studio or it matches the
  // form's current studio.
  const showStudioRow =
    scrapedStudio &&
    !(
      scrapedStudio.stored_id &&
      current.studio &&
      scrapedStudio.stored_id === current.studio.id
    );
  const [studioRes, setStudioRes] = useState<ScrapedItemResolution | null>(
    null,
  );

  // ── Per-item resolutions for nested entity lists ──
  const scrapedPerformers = scraped?.performers ?? [];
  const scrapedTags = scraped?.tags ?? [];
  const scrapedGroups = scraped?.groups ?? [];
  const [performerRes, setPerformerRes] = useState<
    Record<number, ScrapedItemResolution>
  >({});
  const [tagRes, setTagRes] = useState<Record<number, ScrapedItemResolution>>(
    {},
  );
  const [groupRes, setGroupRes] = useState<
    Record<number, ScrapedItemResolution>
  >({});
  function getRes(
    map: Record<number, ScrapedItemResolution>,
    i: number,
    seed: { stored_id?: string | null; name?: string | null },
  ): ScrapedItemResolution {
    return map[i] ?? defaultItemResolution(seed);
  }

  // Reset every time the dialog reopens with a fresh payload.
  useEffect(() => {
    if (open) {
      setAccepted({});
      setMergeModes({});
      const s = scraped?.studio ?? null;
      setStudioRes(s ? defaultItemResolution(s) : null);
      setPerformerRes({});
      setTagRes({});
      setGroupRes({});
    }
  }, [open, scraped]);

  const simpleRows = useMemo(
    () =>
      [...stringRows, urlRow, stashIdRow].filter(
        (r): r is PreparedRow => r !== null,
      ),
    [stringRows, urlRow, stashIdRow],
  );

  // ── Mutations for "create new" resolutions ──
  const [createTag] = useMutation(GQL.TagCreateDocument);
  const [createPerformer] = useMutation(GQL.PerformerCreateDocument);
  const [createStudio] = useMutation(GQL.StudioCreateDocument);
  const [createGroup] = useMutation(GQL.GroupCreateDocument);
  const [applying, setApplying] = useState(false);

  /**
   * Resolve a list of scraped entities into a set of {id, name} additions.
   * Runs all "create" mutations in parallel and folds the resulting ids.
   * Returns null on mutation failure (caller should bail).
   */
  async function resolveList<
    T extends { stored_id?: string | null; name?: string | null },
  >(
    items: T[],
    resMap: Record<number, ScrapedItemResolution>,
    create: (name: string) => Promise<EntityOption | null>,
  ): Promise<Map<string, EntityOption> | null> {
    const additions = new Map<string, EntityOption>();
    const promises: Promise<void>[] = [];
    for (let i = 0; i < items.length; i++) {
      const r = getRes(resMap, i, items[i]);
      if (r.kind === "skip") continue;
      if (r.kind === "existing") {
        additions.set(r.option.id, r.option);
        continue;
      }
      // create
      promises.push(
        create(r.name).then((opt) => {
          if (opt) additions.set(opt.id, opt);
        }),
      );
    }
    try {
      await Promise.all(promises);
    } catch (e) {
      toast.error(e);
      return null;
    }
    return additions;
  }

  async function handleApply() {
    if (!scraped) return;
    setApplying(true);
    try {
      const patch: Partial<SceneFormValues> = {};

      for (const row of simpleRows) {
        if (isAccepted(row.key)) row.apply(patch, getMergeMode(row.key));
      }

      // Studio (single)
      if (showStudioRow && studioRes && isAccepted("studio")) {
        if (studioRes.kind === "existing") {
          patch.studio = studioRes.option;
        } else if (studioRes.kind === "create") {
          try {
            const result = await createStudio({
              variables: { input: { name: studioRes.name } },
            });
            const created = result.data?.studioCreate;
            if (created) {
              patch.studio = { id: created.id, name: created.name };
            }
          } catch (e) {
            toast.error(e);
            setApplying(false);
            return;
          }
        }
        // skip → do nothing
      }

      // Performers (list, with merge mode)
      if (scrapedPerformers.length > 0) {
        const additions = await resolveList(
          scrapedPerformers,
          performerRes,
          async (name) => {
            const result = await createPerformer({
              variables: { input: { name } },
            });
            const c = result.data?.performerCreate;
            return c ? { id: c.id, name: c.name } : null;
          },
        );
        if (!additions) {
          setApplying(false);
          return;
        }
        const mode = getMergeMode("performers");
        if (mode === "overwrite") {
          patch.performers = [...additions.values()];
        } else {
          const existingIds = new Set(current.performers.map((p) => p.id));
          const newOnes = [...additions.values()].filter(
            (p) => !existingIds.has(p.id),
          );
          if (newOnes.length > 0) {
            patch.performers = [...current.performers, ...newOnes];
          }
        }
      }

      // Tags (list, with merge mode)
      if (scrapedTags.length > 0) {
        const additions = await resolveList(
          scrapedTags,
          tagRes,
          async (name) => {
            const result = await createTag({
              variables: { input: { name } },
            });
            const c = result.data?.tagCreate;
            return c ? { id: c.id, name: c.name } : null;
          },
        );
        if (!additions) {
          setApplying(false);
          return;
        }
        const mode = getMergeMode("tags");
        if (mode === "overwrite") {
          patch.tags = [...additions.values()];
        } else {
          const existingIds = new Set(current.tags.map((t) => t.id));
          const newOnes = [...additions.values()].filter(
            (t) => !existingIds.has(t.id),
          );
          if (newOnes.length > 0) {
            patch.tags = [...current.tags, ...newOnes];
          }
        }
      }

      // Groups (list, with merge mode). Note groups carry a scene_index in
      // the form value but the scraper doesn't supply it, so new groups land
      // with scene_index=null.
      if (scrapedGroups.length > 0) {
        const additions = await resolveList(
          scrapedGroups,
          groupRes,
          async (name) => {
            const result = await createGroup({
              variables: { input: { name } },
            });
            const c = result.data?.groupCreate;
            return c ? { id: c.id, name: c.name } : null;
          },
        );
        if (!additions) {
          setApplying(false);
          return;
        }
        const mode = getMergeMode("groups");
        const newEntries: GroupEntry[] = [...additions.values()].map((g) => ({
          group_id: g.id,
          group_name: g.name,
          scene_index: null,
        }));
        if (mode === "overwrite") {
          patch.groups = newEntries;
        } else {
          const existingIds = new Set(current.groups.map((g) => g.group_id));
          const onlyNew = newEntries.filter(
            (g) => !existingIds.has(g.group_id),
          );
          if (onlyNew.length > 0) {
            patch.groups = [...current.groups, ...onlyNew];
          }
        }
      }

      onApply(patch);
      onOpenChange(false);
    } finally {
      setApplying(false);
    }
  }

  if (!scraped) return null;

  const hasAnyContent =
    simpleRows.length > 0 ||
    !!showStudioRow ||
    scrapedPerformers.length > 0 ||
    scrapedTags.length > 0 ||
    scrapedGroups.length > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: "scrape.review_results",
              defaultMessage: "Review scraped results",
            })}
          </DialogTitle>
        </DialogHeader>

        <div className="max-h-[60vh] overflow-y-auto -mx-6 px-6">
          {!hasAnyContent && (
            <p className="text-sm text-muted-foreground py-4 text-center">
              {intl.formatMessage({
                id: "scrape.no_changes",
                defaultMessage:
                  "Nothing new from this scrape — every field already matches the form.",
              })}
            </p>
          )}

          {simpleRows.length > 0 && (
            <div className="grid grid-cols-[24px_1fr_1fr] gap-3 sticky top-0 bg-background pt-2 pb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              <div />
              <div>
                {intl.formatMessage({
                  id: "scrape.column_current",
                  defaultMessage: "Current",
                })}
              </div>
              <div>
                {intl.formatMessage({
                  id: "scrape.column_scraped",
                  defaultMessage: "Scraped",
                })}
              </div>
            </div>
          )}

          {simpleRows.map((row) =>
            row.render({
              accepted: isAccepted(row.key),
              setAccepted: (v) => setAcceptedFor(row.key, v),
              mergeMode: getMergeMode(row.key),
              setMergeMode: (m) => setMergeModeFor(row.key, m),
            }),
          )}

          {showStudioRow && scrapedStudio && studioRes && (
            <div className="mt-4">
              <SectionHeader
                label={intl.formatMessage({
                  id: "studio",
                  defaultMessage: "Studio",
                })}
              />
              <ScrapedStudioRow
                scraped={scrapedStudio}
                value={studioRes}
                onChange={setStudioRes}
              />
            </div>
          )}

          {scrapedPerformers.length > 0 && (
            <div className="mt-4">
              <SectionHeader
                label={intl.formatMessage({
                  id: "performers",
                  defaultMessage: "Performers",
                })}
                mergeMode={
                  current.performers.length > 0
                    ? getMergeMode("performers")
                    : undefined
                }
                onMergeModeChange={
                  current.performers.length > 0
                    ? (m) => setMergeModeFor("performers", m)
                    : undefined
                }
              />
              {scrapedPerformers.map((p, i) => (
                <ScrapedPerformerRow
                  key={`${p.name ?? "performer"}-${i}`}
                  scraped={p}
                  endpoint={
                    source?.kind === "stashBox" ? source.endpoint : undefined
                  }
                  value={getRes(performerRes, i, p)}
                  onChange={(next) =>
                    setPerformerRes((curr) => ({ ...curr, [i]: next }))
                  }
                />
              ))}
            </div>
          )}

          {scrapedTags.length > 0 && (
            <div className="mt-4">
              <SectionHeader
                label={intl.formatMessage({
                  id: "tags",
                  defaultMessage: "Tags",
                })}
                mergeMode={
                  current.tags.length > 0 ? getMergeMode("tags") : undefined
                }
                onMergeModeChange={
                  current.tags.length > 0
                    ? (m) => setMergeModeFor("tags", m)
                    : undefined
                }
              />
              {scrapedTags.map((t, i) => (
                <ScrapedTagRow
                  key={`${t.name}-${i}`}
                  scraped={t}
                  value={getRes(tagRes, i, t)}
                  onChange={(next) =>
                    setTagRes((curr) => ({ ...curr, [i]: next }))
                  }
                />
              ))}
            </div>
          )}

          {scrapedGroups.length > 0 && (
            <div className="mt-4">
              <SectionHeader
                label={intl.formatMessage({
                  id: "groups",
                  defaultMessage: "Groups",
                })}
                mergeMode={
                  current.groups.length > 0 ? getMergeMode("groups") : undefined
                }
                onMergeModeChange={
                  current.groups.length > 0
                    ? (m) => setMergeModeFor("groups", m)
                    : undefined
                }
              />
              {scrapedGroups.map((g, i) => (
                <ScrapedGroupRow
                  key={`${g.name ?? "group"}-${i}`}
                  scraped={g}
                  value={getRes(groupRes, i, g)}
                  onChange={(next) =>
                    setGroupRes((curr) => ({ ...curr, [i]: next }))
                  }
                />
              ))}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={applying}
          >
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button
            type="button"
            onClick={handleApply}
            disabled={applying || !hasAnyContent}
          >
            {applying && <Spinner className="size-4" />}
            {intl.formatMessage({
              id: "actions.apply",
              defaultMessage: "Apply",
            })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
