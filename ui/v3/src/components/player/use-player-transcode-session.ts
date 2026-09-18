import { useEffect } from "react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { isHlsPlaylist } from "./hls";
import { PlayerTranscodeSession } from "./player-transcode-session";

/** Ordinary players own their lease. A TV window can retain it after a swipe. */
export function usePlayerTranscodeSession(
  sceneId: string,
  finalSrc: string | undefined,
  owner?: PlayerTranscodeSession,
) {
  const currentSource = useCommittedRef({ sceneId, src: finalSrc });
  useEffect(() => {
    if (owner) {
      // Suspension detaches the visible player without releasing a prepared
      // item. Only its window owner decides when that session is evicted.
      if (finalSrc) owner.selectSource(finalSrc);
      return;
    }
    if (!finalSrc || !isHlsPlaylist(finalSrc)) return;
    const session = new PlayerTranscodeSession(sceneId);
    session.selectSource(finalSrc);
    return () => {
      const next = currentSource.current;
      session.dispose(
        next.sceneId === sceneId &&
          next.src !== finalSrc &&
          next.src &&
          isHlsPlaylist(next.src)
          ? next.src
          : undefined,
      );
    };
  }, [sceneId, finalSrc, owner]);
}
