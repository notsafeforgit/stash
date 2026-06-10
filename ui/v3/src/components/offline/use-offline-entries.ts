/**
 * Live snapshot of `OfflineEntry` rows from IDB. Re-reads on every IDB
 * mutation (via `subscribeToEntries`) so deletes / completes / status
 * flips show up instantly in the list view without waiting for a
 * route remount.
 */
import { useCallback, useEffect, useState } from "react";
import { ensureDownloadQueueInit } from "./use-download-queue";
import {
  listEntries,
  subscribeToEntries,
  type OfflineEntry,
} from "./offline-db";

export interface UseOfflineEntries {
  entries: OfflineEntry[];
  loading: boolean;
  refresh: () => void;
}

export function useOfflineEntries(): UseOfflineEntries {
  const [entries, setEntries] = useState<OfflineEntry[]>([]);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(() => {
    void (async () => {
      await ensureDownloadQueueInit();
      const rows = await listEntries();
      setEntries(rows);
      setLoading(false);
    })();
  }, []);

  // Initial load + re-fetch whenever the IDB pub-sub fires. Every
  // mutating helper (`putEntry`, `patchEntry`, `deleteEntry`,
  // `clearAll`) calls `notifyChange` after the transaction commits, so
  // the list reflects deletes / progress writes / status transitions
  // without having to also subscribe to the download queue snapshot.
  useEffect(() => {
    refresh();
    return subscribeToEntries(refresh);
  }, [refresh]);

  return { entries, loading, refresh };
}
