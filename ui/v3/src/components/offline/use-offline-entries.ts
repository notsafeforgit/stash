import { useCallback, useEffect, useState } from "react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { ensureDownloadQueueInit } from "./use-download-queue";
import {
  getEntry,
  listEntries,
  subscribeToEntries,
  type OfflineEntry,
} from "./offline-db";

/** Generation belongs to the subscription, so old reads cannot overwrite a
 * newer database notification or update a newly mounted scene. */
function useOfflineQuery<T>(key: string, read: () => Promise<T>, initial: T) {
  const reader = useCommittedRef(read);
  const [revision, setRevision] = useState(0);
  const [snapshot, setSnapshot] = useState<{
    key: string;
    data: T;
    loading: boolean;
    error?: Error;
  }>({ key, data: initial, loading: true });
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    setSnapshot((previous) => ({ ...previous, loading: true }));
    // A manual refresh creates a new subscription generation too.
    let generation = revision;
    let disposed = false;
    async function reload() {
      const current = ++generation;
      try {
        await ensureDownloadQueueInit();
        const data = await reader.current();
        if (!disposed && current === generation)
          setSnapshot({ key, data, loading: false });
      } catch (error) {
        if (!disposed && current === generation)
          setSnapshot((previous) => ({
            key,
            data: previous.key === key ? previous.data : initial,
            loading: false,
            error: error instanceof Error ? error : new Error(String(error)),
          }));
      }
    }
    const unsubscribe = subscribeToEntries(() => void reload());
    void reload();
    return () => {
      disposed = true;
      unsubscribe();
    };
  }, [key, revision, initial]);
  const current =
    snapshot.key === key ? snapshot : { key, data: initial, loading: true };
  return { ...current, refresh };
}
const EMPTY_ENTRIES: OfflineEntry[] = [];
export function useOfflineEntries() {
  const { data: entries, ...state } = useOfflineQuery(
    "all",
    listEntries,
    EMPTY_ENTRIES,
  );
  return { entries, ...state };
}
export function useOfflineEntry(sceneId: string) {
  const { data: entry, ...state } = useOfflineQuery(
    sceneId,
    () => getEntry(sceneId),
    undefined,
  );
  return { entry, ...state };
}
