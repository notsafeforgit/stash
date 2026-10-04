import { NativeArchiveError } from "./client";

// getRandomValues also works on self-hosted LAN HTTP, where randomUUID may not.
export function requestUUID(): string {
  const hex = Array.from(
    crypto.getRandomValues(new Uint8Array(16)),
    (byte, index) => {
      const value =
        index === 6 ? (byte & 15) | 64 : index === 8 ? (byte & 63) | 128 : byte;
      return value.toString(16).padStart(2, "0");
    },
  ).join("");
  return [
    hex.slice(0, 8),
    hex.slice(8, 12),
    hex.slice(12, 16),
    hex.slice(16, 20),
    hex.slice(20),
  ].join("-");
}

/** Serialize competing tabs and wait for durable completion before sending.
 * Each review protocol validates its saved record and controls recovery. */
export async function withReviewRecord<T>(
  databaseName: string,
  key: string,
  mode: IDBTransactionMode,
  action: (value: unknown, store: IDBObjectStore) => T,
): Promise<T> {
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(databaseName, 1);
    let blocked = false;
    request.onupgradeneeded = () =>
      request.result.createObjectStore("requests");
    request.onerror = () => reject(request.error);
    request.onblocked = () => {
      blocked = true;
      reject(new NativeArchiveError(0, "storage_blocked"));
    };
    request.onsuccess = () => {
      if (blocked) request.result.close();
      else resolve(request.result);
    };
  });
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = database.transaction("requests", mode, {
        durability: "strict",
      });
      let result: T;
      let failure: unknown;
      const store = tx.objectStore("requests");
      const request = store.get(key);
      request.onsuccess = () => {
        try {
          result = action(request.result, store);
        } catch (error) {
          failure = error;
          tx.abort();
        }
      };
      tx.oncomplete = () => resolve(result);
      tx.onabort = () =>
        reject(
          failure ?? tx.error ?? new NativeArchiveError(0, "storage_aborted"),
        );
      tx.onerror = () => reject(tx.error);
    });
  } finally {
    database.close();
  }
}
