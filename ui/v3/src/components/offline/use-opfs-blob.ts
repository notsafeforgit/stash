/**
 * Resolves an OPFS scene file → `blob:` URL for the lifetime of the
 * caller component, and revokes the URL on unmount or scene change.
 *
 * Used by both the offline detail route and the offline lightbox slide,
 * which would otherwise repeat the same 20-line `useEffect` (read OPFS,
 * createObjectURL, set state, cancel on unmount, revoke).
 *
 * Return shape:
 *   - `url: string`    — ready to feed into `<video src>` or
 *                        `URL.createObjectURL(blob)`-keyed components
 *   - `url: null`      — initial / in-flight resolution
 *   - `missing: true`  — OPFS file doesn't exist (browser eviction or
 *                        partial download). Caller renders a re-
 *                        download CTA.
 *   - `error: string`  — non-NotFound failure (filesystem error, etc.)
 *                        when the caller wants to surface the message.
 *
 * `missing` is split out from `error` because callers usually want
 * different UX for the two: a missing file routes to "Re-download",
 * whereas an arbitrary error gets a "Failed to load" message with the
 * raw error string.
 */

import { useEffect, useState } from "react";
import { readScene } from "./opfs-storage";

export interface OpfsBlobResult {
  url: string | null;
  missing: boolean;
  error: string | null;
}

export function useOpfsBlobUrl(
  sceneId: string | null | undefined,
): OpfsBlobResult {
  const [url, setUrl] = useState<string | null>(null);
  const [missing, setMissing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setUrl(null);
    setMissing(false);
    setError(null);
    if (!sceneId) return;

    let created: string | null = null;
    let cancelled = false;
    void (async () => {
      try {
        const file = await readScene(sceneId);
        if (cancelled) return;
        if (!file) {
          setMissing(true);
          return;
        }
        created = URL.createObjectURL(file);
        setUrl(created);
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
      }
    })();
    return () => {
      cancelled = true;
      if (created) URL.revokeObjectURL(created);
    };
  }, [sceneId]);

  return { url, missing, error };
}
