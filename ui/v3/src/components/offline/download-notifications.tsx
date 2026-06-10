/**
 * Toast notifier for offline-download lifecycle events. Mounted once
 * at the app shell so users see download outcome feedback regardless
 * of which route they're on.
 *
 * Lifecycle: in-progress download visibility lives in the header's
 * `<DownloadTray>` (a popover with per-row progress bars). Toasts
 * here only fire on terminal transitions:
 *   - `complete` ⇒ a brief success toast
 *   - `error` ⇒ an error toast (silent for user-initiated cancels)
 *
 * No "downloading…" loading toast — the tray handles that. Stacked
 * loading toasts during multi-select bulk downloads were the wrong
 * surface for monitoring queue state.
 */

import { useEffect, useRef } from "react";
import { useIntl } from "react-intl";
import { toast } from "sonner";
import { fileStemFromPath } from "src/utils/file";
import { useDownloadQueue } from "./use-download-queue";
import { getEntry, type OfflineEntry } from "./offline-db";

function toastId(sceneId: string): string {
  return `offline-download-${sceneId}`;
}

function entryDisplayTitle(entry: OfflineEntry): string {
  if (entry.title) return entry.title;
  if (entry.source_file_path) return fileStemFromPath(entry.source_file_path);
  return entry.scene_id;
}

export function DownloadNotifications() {
  const intl = useIntl();
  const queue = useDownloadQueue();
  const previousActiveRef = useRef<string | null>(null);

  useEffect(() => {
    const prev = previousActiveRef.current;
    const curr = queue.state.active?.sceneId ?? null;
    previousActiveRef.current = curr;

    // Transition: something → nothing. Just finished. Inspect the IDB
    // row to decide outcome (complete / error / cancelled). Bulk
    // downloads cycle through the active slot serially, so this fires
    // once per scene as it finishes.
    if (prev && !curr) {
      void getEntry(prev).then((entry) => {
        if (!entry) return;
        const title = entryDisplayTitle(entry);
        if (entry.status === "complete") {
          toast.success(
            intl.formatMessage(
              { id: "offline.notifications.downloaded_title" },
              { title },
            ),
            { id: toastId(prev) },
          );
        } else if (entry.status === "error") {
          if (entry.error === "Cancelled") {
            // User-initiated cancel — silent: they just clicked
            // cancel, surfacing it as a red toast is noise.
            return;
          }
          toast.error(
            intl.formatMessage(
              { id: "offline.notifications.download_failed" },
              {
                error:
                  entry.error ??
                  intl.formatMessage({ id: "offline.card.error_unknown" }),
              },
            ),
            { id: toastId(prev) },
          );
        }
        // Other statuses (re-queued for retry, removed) → no toast.
      });
    }
  }, [intl, queue.state.active]);

  return null;
}
