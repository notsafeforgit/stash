/**
 * Local data-source plumbing for the offline scene list. The list page
 * shell (`EntityListPage`) operates on a generic `TItem extends IHasID`
 * stream that comes either from a GraphQL `useQuery` or — in the
 * offline view — from this module's hooks.
 *
 * Item shape: `OfflineCardItem` wraps an `OfflineEntry` and adds
 * `id` (a duplicate of `scene_id`) so the existing `IHasID`-based
 * selection / memoisation / table machinery just works without
 * having to special-case offline rows.
 *
 * Filter / sort / paginate runs in-memory (the dataset is bounded by
 * the user's local downloads, typically a few hundred at most). Search
 * and sort piggy-back on the chrome's `ListFilterModel`; the offline-
 * specific predicates (status, studio id, performer ids, tag ids) live
 * in `OfflineExtraFilter` which the sidebar owns.
 */

import { useMemo } from "react";
import type { ListFilterModel } from "src/models/list-filter/filter";
import { SortDirectionEnum } from "src/core/generated-graphql";
import type { OfflineEntry } from "./offline-db";
import type { LocalDataSource } from "src/components/list/entity-list-page";

export interface OfflineCardItem {
  id: string;
  entry: OfflineEntry;
}

export type OfflineStatusFilter =
  | "complete"
  | "downloading"
  | "queued"
  | "error";

export interface OfflineExtraFilter {
  /** Empty set means "no status filter" (show everything). */
  statuses: Set<OfflineStatusFilter>;
  /** Studio ids the user picked. Empty set = no studio filter. */
  studioIds: Set<string>;
  /** Performer ids. Empty set = no performer filter. AND across selected ids. */
  performerIds: Set<string>;
  /** Tag ids. Empty set = no tag filter. AND across selected ids. */
  tagIds: Set<string>;
}

export const EMPTY_OFFLINE_FILTER: OfflineExtraFilter = {
  statuses: new Set(),
  studioIds: new Set(),
  performerIds: new Set(),
  tagIds: new Set(),
};

// ── Sort options ─────────────────────────────────────────────────────────────

/**
 * Sort keys exposed in the offline list toolbar. Each maps to a
 * comparator over `OfflineEntry` so the toolbar dropdown can offer
 * the same sort UX as the streaming list, but only over fields we
 * actually have locally.
 *
 * The `messageID` values match locale entries the streaming list
 * already uses (e.g. `"date"`, `"title"`) so we don't multiply
 * translation work; `"downloaded_at"` is offline-specific and gets
 * its own message key.
 */
export const OFFLINE_SORT_OPTIONS = [
  { value: "downloaded_at", messageID: "offline.sort.downloaded_at" },
  { value: "title", messageID: "title" },
  { value: "date", messageID: "date" },
  { value: "duration", messageID: "duration" },
  { value: "filesize", messageID: "filesize" },
  { value: "resolution", messageID: "resolution" },
] as const;

export const OFFLINE_DEFAULT_SORT = "downloaded_at";

// ── Comparators ──────────────────────────────────────────────────────────────

function entryTitleForCompare(e: OfflineEntry): string {
  if (e.title) return e.title.toLowerCase();
  if (e.source_file_path) {
    // Match `objectTitle`'s fallback: the path stem, lowercased.
    const base = e.source_file_path.replace(/^.*[\\/]/, "");
    const dot = base.lastIndexOf(".");
    return (dot > 0 ? base.slice(0, dot) : base).toLowerCase();
  }
  return e.scene_id;
}

function compareBy(sortBy: string, a: OfflineEntry, b: OfflineEntry): number {
  switch (sortBy) {
    case "downloaded_at":
      return a.downloaded_at - b.downloaded_at;
    case "title":
      return entryTitleForCompare(a).localeCompare(entryTitleForCompare(b));
    case "date": {
      const ad = a.date ?? "";
      const bd = b.date ?? "";
      return ad.localeCompare(bd);
    }
    case "duration":
      return (a.duration || 0) - (b.duration || 0);
    case "filesize":
      return (a.bytes || 0) - (b.bytes || 0);
    case "resolution": {
      // Compare on the smaller dimension so portrait + landscape sort
      // by the same notion of "quality tier" the streaming list uses.
      const ar = Math.min(
        a.width_actual || a.width || 0,
        a.height_actual || a.height || 0,
      );
      const br = Math.min(
        b.width_actual || b.width || 0,
        b.height_actual || b.height || 0,
      );
      return ar - br;
    }
    default:
      return 0;
  }
}

// ── Search ──────────────────────────────────────────────────────────────────

function entryMatchesSearch(e: OfflineEntry, query: string): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  if (e.title?.toLowerCase().includes(q)) return true;
  if (e.studio_name?.toLowerCase().includes(q)) return true;
  if (e.performers.some((p) => p.name.toLowerCase().includes(q))) return true;
  if (e.tags.some((t) => t.name.toLowerCase().includes(q))) return true;
  if (e.source_file_path?.toLowerCase().includes(q)) return true;
  return false;
}

// ── Filter predicate ────────────────────────────────────────────────────────

function entryMatchesExtra(
  e: OfflineEntry,
  extra: OfflineExtraFilter,
  isActive: boolean,
): boolean {
  if (extra.statuses.size > 0) {
    // The active scene reads as "downloading" regardless of its IDB
    // status (which lags behind the in-memory queue worker). Mirror
    // that into the filter so the chip works during a live download.
    const effectiveStatus = isActive ? "downloading" : e.status;
    if (!extra.statuses.has(effectiveStatus as OfflineStatusFilter))
      return false;
  }
  if (extra.studioIds.size > 0) {
    if (!e.studio_id || !extra.studioIds.has(e.studio_id)) return false;
  }
  if (extra.performerIds.size > 0) {
    const have = new Set(e.performers.map((p) => p.id));
    for (const id of extra.performerIds) if (!have.has(id)) return false;
  }
  if (extra.tagIds.size > 0) {
    const have = new Set(e.tags.map((t) => t.id));
    for (const id of extra.tagIds) if (!have.has(id)) return false;
  }
  return true;
}

// ── Main: build the LocalDataSource ──────────────────────────────────────────

/**
 * Build the `LocalDataSource` the chrome consumes, parameterised on
 * the OfflineExtraFilter the sidebar manages and the live "active
 * scene id" from the queue (so the in-flight scene shows as
 * `downloading` in status filters even before the IDB row catches up).
 *
 * Returns a memoised value so spread changes that don't actually
 * affect filter inputs don't re-trigger the chrome's recompute path.
 */
export function useOfflineListSource(args: {
  entries: OfflineEntry[];
  loading: boolean;
  extra: OfflineExtraFilter;
  activeSceneId: string | null;
}): LocalDataSource<OfflineCardItem> {
  const { entries, loading, extra, activeSceneId } = args;

  return useMemo<LocalDataSource<OfflineCardItem>>(
    () => ({
      kind: "local",
      // The chrome treats `items` as the raw input; `filter` projects
      // it to the page slice. We pass the entries straight through
      // and do all the work in `filter` so the dependency graph is
      // exact (no item reshuffling on UI-only renders).
      items: entries.map((e) => ({ id: e.scene_id, entry: e })),
      loading,
      filter: (rawItems, filterModel) => {
        const filtered = filterByExtraSearchSort(
          rawItems,
          filterModel,
          extra,
          activeSceneId,
        );
        const startIdx =
          (filterModel.currentPage - 1) * filterModel.itemsPerPage;
        const pageItems = filtered.slice(
          startIdx,
          startIdx + filterModel.itemsPerPage,
        );
        return { count: filtered.length, items: pageItems };
      },
    }),
    [entries, loading, extra, activeSceneId],
  );
}

function filterByExtraSearchSort(
  items: OfflineCardItem[],
  filter: ListFilterModel,
  extra: OfflineExtraFilter,
  activeSceneId: string | null,
): OfflineCardItem[] {
  const q = filter.searchTerm;
  const dir = filter.sortDirection === SortDirectionEnum.Asc ? 1 : -1;
  const sortBy = filter.sortBy ?? OFFLINE_DEFAULT_SORT;

  const matched = items.filter(
    ({ entry }) =>
      entryMatchesExtra(entry, extra, entry.scene_id === activeSceneId) &&
      entryMatchesSearch(entry, q),
  );
  matched.sort((a, b) => dir * compareBy(sortBy, a.entry, b.entry));
  return matched;
}
