import { useEffect, useRef } from "react";
import type { CreatePlayerResult, VideoPlayerStore } from "@videojs/react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import {
  createPlatformMediaSession,
  type PlatformMediaOptions,
} from "./platform-media-session";
import { createScreenWakeLock } from "./screen-wake-lock";

export function PlatformMediaEffects({
  Player,
  ...options
}: PlatformMediaOptions & {
  Player: CreatePlayerResult<VideoPlayerStore>;
}) {
  const store = Player.usePlayer();
  const latest = useCommittedRef(options);
  const refreshRef = useRef<() => void>(() => {});
  useEffect(() => {
    const mediaSession = createPlatformMediaSession(
      store,
      () => latest.current,
    );
    const wakeLock = createScreenWakeLock();
    const refresh = () => {
      mediaSession.refresh();
      const state = store.state;
      wakeLock.setActive(
        !latest.current.suspended &&
          !state.paused &&
          !state.ended &&
          !state.pip &&
          state.remotePlaybackState === "disconnected",
      );
    };
    refreshRef.current = refresh;
    const unsubscribe = store.subscribe(refresh);
    refresh();
    return () => {
      refreshRef.current = () => {};
      unsubscribe();
      mediaSession.dispose();
      wakeLock.dispose();
    };
  }, [store]);
  // Publish source/clip changes even before the next media event.
  useEffect(() => {
    refreshRef.current();
  });
  return null;
}
