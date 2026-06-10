/**
 * Header tray exposing the live download queue. A button (cloud-
 * download icon + count badge) opens a Popover listing the active
 * download with a determinate-or-indeterminate progress bar, plus
 * any queued and recently-errored entries with cancel / retry
 * controls. Replaces the per-download loading toast: progress is now
 * monitorable from any non-lightbox view via this single surface.
 *
 * Source of truth:
 *   - `useDownloadQueue` for live `active` + `queued` order (the queue
 *     worker is single-threaded; only one scene downloads at a time).
 *   - `useOfflineEntries` for the IDB-backed metadata (title, status,
 *     error text). Combined here because the queue snapshot only
 *     carries scene ids + bytes, not user-facing labels.
 *
 * The button itself is hidden when there's nothing to show (no active,
 * no queued, no errors) — keeps the header clean for users who never
 * download anything.
 */

import type React from "react";
import { useMemo } from "react";
import { useIntl } from "react-intl";
import { Link } from "@tanstack/react-router";
import {
  CloudDownloadIcon,
  XIcon,
  RotateCcwIcon,
  Trash2Icon,
} from "lucide-react";
import { Badge } from "src/components/ui/badge";
import { Button } from "src/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "src/components/ui/popover";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  EmptyDescription,
} from "src/components/ui/empty";
import {
  Progress,
  ProgressTrack,
  ProgressIndicator,
} from "src/components/ui/progress";
import { cn } from "src/lib/utils";
import { useDownloadQueue } from "./use-download-queue";
import { useOfflineEntries } from "./use-offline-entries";
import { entryDisplayTitle, type OfflineEntry } from "./offline-db";

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

export function DownloadTray() {
  const intl = useIntl();
  const queue = useDownloadQueue();
  const { entries } = useOfflineEntries();

  // Index entries by id for the lookups below — entries is bounded by
  // the user's offline library, so a per-render Map is cheap.
  const entryById = useMemo(() => {
    const m = new Map<string, OfflineEntry>();
    for (const e of entries) m.set(e.scene_id, e);
    return m;
  }, [entries]);

  const activeEntry = queue.state.active
    ? entryById.get(queue.state.active.sceneId)
    : undefined;
  const queuedEntries = queue.state.queued
    .map((id) => entryById.get(id))
    .filter((e): e is OfflineEntry => !!e);
  const errored = entries.filter((e) => e.status === "error");

  const totalActivity =
    (queue.state.active ? 1 : 0) + queuedEntries.length + errored.length;

  // Hidden when nothing is happening — the header stays uncluttered for
  // users who never download anything.
  if (totalActivity === 0) return null;

  // Badge count: in-flight + queued (errors are shown separately and
  // don't read as "in progress" so they're excluded from the count).
  const inProgressCount = (queue.state.active ? 1 : 0) + queuedEntries.length;

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            className="relative"
            aria-label={intl.formatMessage({
              id: "offline.tray.label",
              defaultMessage: "Downloads",
            })}
          />
        }
      >
        <CloudDownloadIcon />
        {inProgressCount > 0 && (
          <Badge
            // Override Badge's default size to a notification-pip
            // overlay (~16px). The default `h-5 w-fit` is sized for
            // inline labels, not as an icon-corner notification dot.
            className="absolute -right-0.5 -top-0.5 size-4 min-w-0 px-1 text-[0.625rem] leading-none"
          >
            {inProgressCount > 9 ? "9+" : inProgressCount}
          </Badge>
        )}
        {inProgressCount === 0 && errored.length > 0 && (
          <Badge
            variant="destructive"
            className="absolute -right-0.5 -top-0.5 size-4 min-w-0 px-1 text-[0.625rem] leading-none"
          >
            {errored.length > 9 ? "9+" : errored.length}
          </Badge>
        )}
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className="w-80 max-h-[28rem] overflow-y-auto p-0 gap-0"
      >
        <PopoverHeader className="flex flex-row items-center justify-between gap-2 border-b border-border px-3 py-2">
          <PopoverTitle>
            {intl.formatMessage({
              id: "offline.tray.title",
              defaultMessage: "Downloads",
            })}
          </PopoverTitle>
          <Link
            to="/offline"
            className="text-xs text-muted-foreground hover:text-foreground hover:underline"
          >
            {intl.formatMessage({
              id: "offline.tray.view_all",
              defaultMessage: "View all",
            })}
          </Link>
        </PopoverHeader>

        <div className="flex flex-col">
          {queue.state.active && activeEntry && (
            <ActiveRow
              entry={activeEntry}
              bytesDownloaded={queue.state.active.bytesDownloaded}
              bytesTotal={queue.state.active.bytesTotal}
              onCancel={() => void queue.cancel(queue.state.active!.sceneId)}
            />
          )}

          {queuedEntries.length > 0 && (
            <SectionLabel>
              {intl.formatMessage(
                {
                  id: "offline.tray.queued_count",
                  defaultMessage:
                    "{count, plural, one {# queued} other {# queued}}",
                },
                { count: queuedEntries.length },
              )}
            </SectionLabel>
          )}
          {queuedEntries.map((entry) => (
            <QueuedRow
              key={entry.scene_id}
              entry={entry}
              onCancel={() => void queue.cancel(entry.scene_id)}
            />
          ))}

          {errored.length > 0 && (
            <SectionLabel destructive>
              {intl.formatMessage(
                {
                  id: "offline.tray.errored_count",
                  defaultMessage:
                    "{count, plural, one {# failed} other {# failed}}",
                },
                { count: errored.length },
              )}
            </SectionLabel>
          )}
          {errored.map((entry) => (
            <ErrorRow
              key={entry.scene_id}
              entry={entry}
              onRetry={() => void queue.retry(entry.scene_id)}
              onDismiss={() => void queue.remove(entry.scene_id)}
            />
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}

// ── Rows ─────────────────────────────────────────────────────────────────────

function ActiveRow({
  entry,
  bytesDownloaded,
  bytesTotal,
  onCancel,
}: {
  entry: OfflineEntry;
  bytesDownloaded: number;
  bytesTotal: number | null;
  onCancel: () => void;
}) {
  const intl = useIntl();
  const determinate = bytesTotal != null && bytesTotal > 0;
  const pct = determinate
    ? Math.min(100, Math.round((bytesDownloaded / bytesTotal!) * 100))
    : 0;

  return (
    <div className="flex flex-col gap-1.5 border-b border-border px-3 py-2.5">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">
            {entryDisplayTitle(entry)}
          </div>
          <div className="text-xs text-muted-foreground tabular-nums">
            {determinate
              ? `${formatBytes(bytesDownloaded)} / ${formatBytes(bytesTotal!)} · ${pct}%`
              : formatBytes(bytesDownloaded)}
          </div>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onCancel}
          aria-label={intl.formatMessage({
            id: "offline.actions.cancel_download",
          })}
          title={intl.formatMessage({ id: "offline.actions.cancel_download" })}
        >
          <XIcon />
        </Button>
      </div>
      {/* Base UI Progress: pass the live percentage as `value`, or
          `null` to flip into indeterminate mode (Base UI then drops
          the inline width on the indicator and our CSS keyframe
          sweep takes over). Composed via Track + Indicator so the
          indeterminate variant can apply the sweep class to the
          indicator only — width=auto on the Track. */}
      <Progress value={determinate ? pct : null} className="block w-full">
        <ProgressTrack className="relative block h-1 w-full overflow-hidden rounded-full bg-primary/10">
          <ProgressIndicator
            className={cn(
              "h-full bg-primary",
              !determinate && "download-progress-indeterminate",
            )}
          />
        </ProgressTrack>
      </Progress>
    </div>
  );
}

function QueuedRow({
  entry,
  onCancel,
}: {
  entry: OfflineEntry;
  onCancel: () => void;
}) {
  const intl = useIntl();
  return (
    <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-2 text-sm">
      <div className="min-w-0 flex-1">
        <div className="truncate">{entryDisplayTitle(entry)}</div>
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={onCancel}
        aria-label={intl.formatMessage({
          id: "offline.actions.cancel_download",
        })}
        title={intl.formatMessage({ id: "offline.actions.cancel_download" })}
      >
        <XIcon />
      </Button>
    </div>
  );
}

function ErrorRow({
  entry,
  onRetry,
  onDismiss,
}: {
  entry: OfflineEntry;
  onRetry: () => void;
  onDismiss: () => void;
}) {
  const intl = useIntl();
  return (
    <div className="flex items-start justify-between gap-2 border-b border-border px-3 py-2 text-sm">
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{entryDisplayTitle(entry)}</div>
        {entry.error && (
          <div className="truncate text-xs text-destructive">{entry.error}</div>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-0.5">
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onRetry}
          aria-label={intl.formatMessage({
            id: "offline.actions.retry_download",
          })}
          title={intl.formatMessage({ id: "offline.actions.retry_download" })}
        >
          <RotateCcwIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onDismiss}
          aria-label={intl.formatMessage({
            id: "offline.actions.delete_from_device",
          })}
          title={intl.formatMessage({
            id: "offline.actions.delete_from_device",
          })}
        >
          <Trash2Icon />
        </Button>
      </div>
    </div>
  );
}

// ── Section label ───────────────────────────────────────────────────────────

function SectionLabel({
  children,
  destructive,
}: {
  children: React.ReactNode;
  destructive?: boolean;
}) {
  return (
    <div
      className={cn(
        "px-3 py-1.5 text-[0.6875rem] font-semibold uppercase tracking-wide",
        destructive ? "text-destructive" : "text-muted-foreground",
      )}
    >
      {children}
    </div>
  );
}

// ── Empty state (exported for tests / alt mounts; not used directly) ─────────

export function DownloadTrayEmpty() {
  const intl = useIntl();
  return (
    <Empty>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <CloudDownloadIcon />
        </EmptyMedia>
        <EmptyTitle>
          {intl.formatMessage({
            id: "offline.tray.empty_title",
            defaultMessage: "No downloads",
          })}
        </EmptyTitle>
        <EmptyDescription>
          {intl.formatMessage({
            id: "offline.tray.empty_description",
            defaultMessage:
              "Downloads in progress and queued downloads will appear here.",
          })}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}
