import { useSyncExternalStore, type SetStateAction } from "react";
import type { z } from "zod";

function isUpdater<T>(value: SetStateAction<T>): value is (previous: T) => T {
  return typeof value === "function";
}

/** One store per persisted key. Storage events synchronize other tabs; writes
 * within this page share a current snapshot. Cross-tab conflicts are last-write
 * wins, matching localStorage; this is not a transactional database. */
export function createStoredState<T>(
  key: string,
  schema: z.ZodType<T>,
  fallback: T,
) {
  let snapshot = fallback;
  let initialized = false;
  let storedRaw: string | null | undefined;
  const listeners = new Set<() => void>();

  function read(): T {
    try {
      const raw = localStorage.getItem(key);
      if (raw === storedRaw) return snapshot;
      storedRaw = raw;
      return raw === null ? fallback : schema.parse(JSON.parse(raw));
    } catch {
      return snapshot;
    }
  }

  function publish(next: T) {
    if (JSON.stringify(next) === JSON.stringify(snapshot)) return;
    snapshot = next;
    for (const notify of listeners) notify();
  }

  function getSnapshot() {
    if (!initialized) {
      snapshot = read();
      initialized = true;
    }
    return snapshot;
  }

  function onStorage(event: StorageEvent) {
    if (event.key === key || event.key === null) publish(read());
  }

  function subscribe(notify: () => void) {
    if (listeners.size === 0) window.addEventListener("storage", onStorage);
    listeners.add(notify);
    publish(read());
    return () => {
      listeners.delete(notify);
      if (listeners.size === 0)
        window.removeEventListener("storage", onStorage);
    };
  }

  function set(update: SetStateAction<T>) {
    getSnapshot();
    const current = read();
    const next = schema.parse(isUpdater(update) ? update(current) : update);
    try {
      const raw = JSON.stringify(next);
      localStorage.setItem(key, raw);
      storedRaw = raw;
    } catch {
      // Private browsing or quota restrictions must not disable in-page edits.
    }
    publish(next);
  }

  function useStoredState(): [T, typeof set] {
    return [useSyncExternalStore(subscribe, getSnapshot, () => fallback), set];
  }

  return { getSnapshot, set, subscribe, useStoredState };
}
