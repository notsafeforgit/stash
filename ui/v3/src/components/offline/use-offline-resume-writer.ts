/**
 * Polls the player's playhead and writes `last_position_seconds` to
 * the offline IDB row. Same shape as the streaming detail page's
 * server-side `SaveActivity` mutation, but local-only: the offline
 * surface is intentionally not connected to the server's resume
 * tracking (see `docs/offline.md` § "Settled decisions").
 *
 * Returned `sendGetCurrentTime` plugs straight into `<ScenePlayer
 * sendGetCurrentTime={…}>` — that prop hands us a getter for the
 * live playhead, which we capture in a ref and read on every poll
 * tick + on unmount + on `beforeunload`.
 *
 * Threshold: writes only when the playhead has moved at least 0.5 s
 * since the last write. Keeps IDB transactions cheap on long pauses
 * and avoids redundant writes when the user is scrubbing back and
 * forth around the same position.
 *
 * 5 s poll cadence mirrors the in-tab streaming player's resume-time
 * write rhythm.
 */

import { useCallback, useLayoutEffect, useRef } from "react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { patchEntry } from "./offline-db";

const POLL_INTERVAL_MS = 5000;
const MIN_POSITION_DELTA_S = 0.5;

export interface OfflineResumeWriter {
  /** Wire as `<ScenePlayer sendGetCurrentTime={…}>`. */
  sendGetCurrentTime: (getter: () => number | undefined) => void;
}

export function useOfflineResumeWriter(
  sceneId: string | null | undefined,
  initialPositionSeconds: number | null | undefined,
): OfflineResumeWriter {
  const getCurrentTimeRef = useRef<(() => number | undefined) | null>(null);
  const sendGetCurrentTime = useCallback((getter: () => number | undefined) => {
    getCurrentTimeRef.current = getter;
  }, []);

  const initialPositionRef = useCommittedRef(initialPositionSeconds ?? 0);

  // Flush before the shared video's source changes in passive media effects.
  // Reading it in passive cleanup can attribute the new playhead to the old row.
  useLayoutEffect(() => {
    if (!sceneId) return;
    let lastWritten = initialPositionRef.current;
    const flush = () => {
      const t = getCurrentTimeRef.current?.();
      if (t == null || t <= 0) return;
      if (Math.abs(t - lastWritten) < MIN_POSITION_DELTA_S) return;
      lastWritten = t;
      void patchEntry(sceneId, { last_position_seconds: t });
    };

    const timer = setInterval(flush, POLL_INTERVAL_MS);
    const onBeforeUnload = () => flush();
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      clearInterval(timer);
      window.removeEventListener("beforeunload", onBeforeUnload);
      // Final write on the route-navigation / lightbox-close path.
      flush();
    };
  }, [sceneId]);

  return { sendGetCurrentTime };
}
