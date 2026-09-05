/**
 * Offline scene list page. Reuses the streaming list chrome
 * (`EntityListPage`) but sources items from IndexedDB via a
 * `LocalDataSource` and replaces the GraphQL `FilterBuilder` sidebar
 * with `OfflineFilterSidebar` (status / studio / performers / tags
 * chips derived from the entries themselves).
 *
 * The sort dropdown is overridden to expose only sort keys the local
 * source can compute (`OFFLINE_SORT_OPTIONS`); search piggy-backs on
 * the chrome's `searchTerm` and matches title / studio / performer /
 * tag names / source path.
 */

import React, { useCallback, useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { HardDriveDownloadIcon } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { EntityListPage } from "src/components/list";
import {
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
} from "src/components/ui/context-menu";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  EmptyDescription,
} from "src/components/ui/empty";
import { SceneCard } from "src/components/cards/scene-card";
import { useBulkCardActions } from "src/components/cards/use-bulk-card-actions";
import { offlineEntryToSceneCardScene } from "./offline-scene-card-data";
import { useOfflineEntries } from "./use-offline-entries";
import { useDownloadQueue, getDownloadQueueStore } from "./use-download-queue";
import { useOfflineMetadataRefresh } from "./offline-metadata-refresh";
import { useOfflineSceneLightbox } from "./use-offline-scene-lightbox";
import { saveToFiles, FileMissingError } from "./save-to-files";
import { OfflineFilterSidebar } from "./offline-filter-sidebar";
import {
  OFFLINE_SORT_OPTIONS,
  OFFLINE_DEFAULT_SORT,
  EMPTY_OFFLINE_FILTER,
  useOfflineListSource,
  type OfflineCardItem,
  type OfflineExtraFilter,
} from "./offline-list-source";
import type { OfflineEntry } from "./offline-db";

export function OfflineSceneListPage() {
  const intl = useIntl();
  const { entries, loading } = useOfflineEntries();
  useOfflineMetadataRefresh({ entries });
  const queue = useDownloadQueue();
  const {
    onCardPreviewClick: openLightboxFromClick,
    lightboxElement,
    lightboxOpen,
  } = useOfflineSceneLightbox();

  // Sidebar-managed extra filter state — kept in React state rather
  // than serialised to the URL (phase 1). The empty/default value is
  // a stable singleton so spread-onto-existing comparisons stay
  // shallow-equal until the user actually picks something.
  const [extra, setExtra] = useState<OfflineExtraFilter>(EMPTY_OFFLINE_FILTER);

  // Live "active" scene id from the queue worker — feeds into the
  // status filter so the in-flight scene reads as `downloading` even
  // before its IDB row catches up.
  const activeSceneId = queue.state.active?.sceneId ?? null;

  const localSource = useOfflineListSource({
    entries,
    loading,
    extra,
    activeSceneId,
  });

  // ── Per-card actions (closed over scene id at render time) ──
  const onSaveToFiles = useCallback(async (entry: OfflineEntry) => {
    try {
      await saveToFiles(entry);
    } catch (err) {
      if (err instanceof FileMissingError) {
        // The list view will pick up the missing file on next render.
        return;
      }
      console.error("[offline] save to files failed:", err);
    }
  }, []);

  const renderCard = useCallback(
    (
      item: OfflineCardItem,
      isMobile: boolean,
      selected: boolean,
      onSelectedChanged: (s: boolean, shift: boolean) => void,
      onPreviewClick?: () => void,
    ) => (
      <OfflineSceneCardCell
        entry={item.entry}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onPreviewClick={onPreviewClick}
        isActive={queue.state.active?.sceneId === item.entry.scene_id}
        activeBytesDownloaded={queue.state.active?.bytesDownloaded}
        activeBytesTotal={queue.state.active?.bytesTotal ?? null}
        onSaveToFiles={() => onSaveToFiles(item.entry)}
        onRedownload={() => void queue.retry(item.entry.scene_id)}
        onCancel={() => void queue.cancel(item.entry.scene_id)}
        onDelete={() => void queue.remove(item.entry.scene_id)}
      />
    ),
    [queue, onSaveToFiles],
  );

  // The chrome reads two empty cases through one prop: the "no
  // entries downloaded yet" case (cold/fresh user) and the "filters
  // narrowed everything out" case (user has chips set). The first
  // gets a help-y empty with the workflow nudge; the second falls
  // through to the default `<DefaultListEmptyState>` rendered by the
  // chrome (so wording stays in sync with the streaming list).
  const noEntriesAtAll = !loading && entries.length === 0;

  const config = useMemo(
    () => ({
      // FilterMode.Scenes so the model has scene-shaped sort options
      // available (we override which ones the toolbar shows below) and
      // saved-filter / URL serialisation infrastructure stays valid.
      filterMode: GQL.FilterMode.Scenes,
      defaultSort: OFFLINE_DEFAULT_SORT,
      source: localSource,
      sidebarContent: (
        <OfflineFilterSidebar
          entries={entries}
          filter={extra}
          onChange={setExtra}
        />
      ),
      sortOptions: [...OFFLINE_SORT_OPTIONS],
      // Only override when the user truly has nothing offline; once
      // entries exist, the default chrome empty state (search /
      // filter narrowing) is the right read.
      emptyState: noEntriesAtAll ? (
        <Empty className="border border-dashed border-border rounded-lg my-6 mx-3">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <HardDriveDownloadIcon />
            </EmptyMedia>
            <EmptyTitle>
              {intl.formatMessage({ id: "offline.empty" })}
            </EmptyTitle>
            <EmptyDescription>
              {intl.formatMessage({ id: "offline.empty_hint" })}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : undefined,
      renderCard,
      zoomable: true,
      onCardPreviewClick: (
        item: OfflineCardItem,
        allItems: OfflineCardItem[],
        index: number,
      ) => {
        if (item.entry.status === "complete")
          openLightboxFromClick(item, allItems, index);
      },
      getWallDimensions: (item: OfflineCardItem) => {
        const w = item.entry.width_actual || item.entry.width;
        const h = item.entry.height_actual || item.entry.height;
        return w && h ? { width: w, height: h } : { width: 16, height: 9 };
      },
    }),
    [
      localSource,
      entries,
      extra,
      noEntriesAtAll,
      intl,
      renderCard,
      openLightboxFromClick,
    ],
  );

  return (
    <>
      <EntityListPage
        config={config}
        keyboardShortcutsDisabled={lightboxOpen}
      />
      {lightboxElement}
    </>
  );
}

// ── Card cell ────────────────────────────────────────────────────────────────
// Wraps the streaming `<SceneCard>` with offline-specific context-menu
// items + a status overlay (downloading/queued/error/missing). The
// SceneCardScene shape comes from the existing adapter so the visual
// result is identical to the streaming card.

interface OfflineSceneCardCellProps {
  entry: OfflineEntry;
  isMobile: boolean;
  selected: boolean;
  onSelectedChanged: (s: boolean, shift: boolean) => void;
  onPreviewClick?: () => void;
  isActive: boolean;
  activeBytesDownloaded?: number;
  activeBytesTotal: number | null;
  onSaveToFiles: () => void;
  onRedownload: () => void;
  onCancel: () => void;
  onDelete: () => void;
}

const OfflineSceneCardCell = React.memo(function OfflineSceneCardCell({
  entry,
  isMobile,
  selected,
  onSelectedChanged,
  onPreviewClick,
  isActive,
  activeBytesDownloaded,
  activeBytesTotal,
  onSaveToFiles,
  onRedownload,
  onCancel,
  onDelete,
}: OfflineSceneCardCellProps) {
  const intl = useIntl();
  const cardScene = useMemo(() => offlineEntryToSceneCardScene(entry), [entry]);
  const canPreviewPlay = entry.status === "complete";
  const showStatus =
    isActive ||
    entry.status === "queued" ||
    entry.status === "error" ||
    entry.server_status === "missing";

  // Bulk-mode hook. Snapshot of the current selection is captured at
  // context-menu open time (lazy — see useBulkCardActions); when the
  // user has multiple offline scenes selected and right-clicks one of
  // them, the menu collapses to bulk actions instead of stacking the
  // single + bulk variants together. Mirrors the streaming card pattern.
  const {
    selectedItems,
    bulkCount,
    showBulkActions,
    onContextMenuOpen,
    onSelectAll,
  } = useBulkCardActions<OfflineCardItem>(entry.scene_id);

  const contextMenu = showBulkActions ? (
    <ContextMenuContent>
      <OfflineBulkContextMenuItems items={selectedItems} count={bulkCount} />
    </ContextMenuContent>
  ) : (
    <ContextMenuContent>
      {/* Select kicks the card into multi-select mode without
          requiring the user to first toggle the checkbox — same UX
          as the streaming cards. Once the first card is selected,
          right-clicking another card (or this one again) flips the
          menu into bulk mode via useBulkCardActions. */}
      <ContextMenuItem onClick={() => onSelectedChanged(true, false)}>
        {intl.formatMessage({
          id: "actions.select",
          defaultMessage: "Select",
        })}
      </ContextMenuItem>
      <ContextMenuItem onClick={onSelectAll}>
        {intl.formatMessage({
          id: "actions.select_all_on_page",
          defaultMessage: "Select all on page",
        })}
      </ContextMenuItem>
      <ContextMenuSeparator />
      {entry.status === "complete" && (
        <ContextMenuItem onClick={onSaveToFiles}>
          {intl.formatMessage({ id: "offline.actions.save_to_files" })}
        </ContextMenuItem>
      )}
      {(entry.status === "complete" || entry.status === "error") && (
        <ContextMenuItem onClick={onRedownload}>
          {intl.formatMessage({
            id:
              entry.status === "error"
                ? "offline.actions.retry_download"
                : "offline.actions.redownload",
          })}
        </ContextMenuItem>
      )}
      {(entry.status === "queued" || entry.status === "downloading") && (
        <ContextMenuItem onClick={onCancel}>
          {intl.formatMessage({ id: "offline.actions.cancel_download" })}
        </ContextMenuItem>
      )}
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onClick={onDelete}>
        {intl.formatMessage({ id: "offline.actions.delete_from_device" })}
      </ContextMenuItem>
    </ContextMenuContent>
  );

  return (
    // h-full is load-bearing for wall mode: PhotoAlbumWall renders each
    // card inside a div with explicit pixel width/height computed by
    // react-photo-album. The SceneCard's `<article>` uses `h-full` to
    // fill that pixel-height parent, but our wrapper sits between them
    // — without `h-full` here the wrapper collapses to content height
    // and the wall row appears empty. (Grid mode shows everything fine
    // because grid cells have implicit auto-row sizing.)
    <div className="relative h-full">
      <SceneCard
        scene={cardScene}
        href={`/offline/${entry.scene_id}`}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onPreviewClick={canPreviewPlay ? onPreviewClick : undefined}
        contextMenu={contextMenu}
        onContextMenuOpen={onContextMenuOpen}
      />
      {showStatus && (
        <CardStatusOverlay
          entry={entry}
          isActive={isActive}
          bytesDownloaded={activeBytesDownloaded}
          bytesTotal={activeBytesTotal}
        />
      )}
    </div>
  );
});

function CardStatusOverlay({
  entry,
  isActive,
  bytesDownloaded,
  bytesTotal,
}: {
  entry: OfflineEntry;
  isActive: boolean;
  bytesDownloaded?: number;
  bytesTotal: number | null;
}) {
  const intl = useIntl();
  let body: React.ReactNode;
  if (isActive) {
    const pct =
      bytesTotal && bytesTotal > 0 && bytesDownloaded != null
        ? Math.round((bytesDownloaded / bytesTotal) * 100)
        : null;
    body = (
      <span>
        {pct != null
          ? intl.formatMessage({ id: "offline.card.downloading_pct" }, { pct })
          : intl.formatMessage(
              { id: "offline.card.downloading_bytes" },
              { bytes: formatBytes(bytesDownloaded ?? 0) },
            )}
      </span>
    );
  } else if (entry.status === "queued") {
    body = <span>{intl.formatMessage({ id: "offline.card.queued" })}</span>;
  } else if (entry.status === "error") {
    body = (
      <span>
        {intl.formatMessage(
          { id: "offline.card.error" },
          {
            error:
              entry.error ??
              intl.formatMessage({ id: "offline.card.error_unknown" }),
          },
        )}
      </span>
    );
  } else if (entry.server_status === "missing") {
    body = (
      <span>
        {intl.formatMessage({ id: "offline.card.removed_from_server" })}
      </span>
    );
  } else {
    return null;
  }
  return (
    <div
      className="pointer-events-none absolute left-1 top-1 rounded bg-black/70 px-1.5 py-0.5 text-xs text-white"
      aria-live="polite"
    >
      {body}
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let n = bytes;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)} ${units[i]}`;
}

// ── Bulk context menu ────────────────────────────────────────────────────────
// Shown in place of the single-card menu when the user right-clicks a
// card that's part of a multi-selection. Operates on the snapshot the
// bulk hook captured at open time — going through the queue store's
// singleton (rather than the React-bound `useDownloadQueue` snapshot)
// avoids tying every cell's render to queue state, since the cell is
// React.memo'd and already gets isActive computed by the parent.

function OfflineBulkContextMenuItems({
  items,
  count,
}: {
  items: OfflineCardItem[];
  count: number;
}) {
  const intl = useIntl();

  // Partition selection by status so we only offer actions whose
  // targets are eligible. Avoids "Cancel" surfacing when nothing is
  // in flight, "Re-download" when nothing is complete/errored, etc.
  const redownloadable = items.filter(
    (i) => i.entry.status === "complete" || i.entry.status === "error",
  );
  const cancellable = items.filter(
    (i) => i.entry.status === "queued" || i.entry.status === "downloading",
  );

  const onBulkRedownload = () => {
    const store = getDownloadQueueStore();
    for (const item of redownloadable) void store.retry(item.entry.scene_id);
  };
  const onBulkCancel = () => {
    const store = getDownloadQueueStore();
    for (const item of cancellable) void store.cancel(item.entry.scene_id);
  };
  const onBulkDelete = () => {
    const store = getDownloadQueueStore();
    for (const item of items) void store.remove(item.entry.scene_id);
  };

  return (
    <>
      {redownloadable.length > 0 && (
        <ContextMenuItem onClick={onBulkRedownload}>
          {intl.formatMessage(
            {
              id: "offline.actions.bulk_redownload_count",
              defaultMessage: "Re-download {count} scenes",
            },
            { count: redownloadable.length },
          )}
        </ContextMenuItem>
      )}
      {cancellable.length > 0 && (
        <ContextMenuItem onClick={onBulkCancel}>
          {intl.formatMessage(
            {
              id: "offline.actions.bulk_cancel_count",
              defaultMessage: "Cancel {count} downloads",
            },
            { count: cancellable.length },
          )}
        </ContextMenuItem>
      )}
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onClick={onBulkDelete}>
        {intl.formatMessage(
          {
            id: "offline.actions.bulk_delete_count",
            defaultMessage: "Delete {count} from device",
          },
          { count },
        )}
      </ContextMenuItem>
    </>
  );
}
