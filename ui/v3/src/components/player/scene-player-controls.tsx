import {
  createContext,
  useContext,
  useMemo,
  useLayoutEffect,
  useState,
  useSyncExternalStore,
  type ReactNode,
  type RefObject,
} from "react";
import type { CreatePlayerResult, VideoPlayerStore } from "@videojs/react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import type { PlayerSource } from "./player-utils";
import type { PlaybackRange } from "@/core/marker-range";

export interface ScenePlayerState {
  paused: boolean;
  muted: boolean;
  volume: number;
  rate: number;
  position: number;
  ready: boolean;
  zoomed: boolean;
  error: string | null;
  canPip: boolean;
}
export interface ScenePlayerCommands {
  read: () => Readonly<ScenePlayerState>;
  play: () => void;
  pause: () => void;
  togglePaused: () => void;
  seek: (sceneTime: number) => void;
  setVolume: (volume: number) => void;
  toggleMuted: () => void;
  setRate: (rate: number) => void;
  setCaption: (index: number | null) => void;
  toggleCaptions: () => void;
  selectSource: (src: string) => void;
  resetQuality: () => void;
  retry: () => void;
  resetZoom: () => void;
  togglePip: () => void;
}
type ControlContext = {
  commands: ScenePlayerCommands;
  subscribe: (listener: () => void) => () => void;
};
const Context = createContext<ControlContext | null>(null);
const SourcesContext = createContext<{
  sources: readonly PlayerSource[];
  activeSource: PlayerSource | null;
} | null>(null);

export function useScenePlayerControls() {
  const value = useContext(Context);
  if (!value) throw new Error("Scene player controls require a ScenePlayer");
  return value.commands;
}
export function useScenePlayerValue<K extends keyof ScenePlayerState>(
  key: K,
): ScenePlayerState[K] {
  const value = useContext(Context);
  if (!value) throw new Error("Scene player state requires a ScenePlayer");
  return useSyncExternalStore(
    value.subscribe,
    () => value.commands.read()[key],
  );
}
export function useScenePlayerSourcesMenu() {
  const value = useContext(SourcesContext);
  if (!value) throw new Error("Scene player sources require a ScenePlayer");
  return value;
}

/** Library adaptation lives here. TV receives only semantic commands and
 * scalar subscriptions; it cannot reach adapters, native fullscreen or stores. */
export function ScenePlayerControlsProvider({
  Player,
  children,
  ...options
}: {
  Player: CreatePlayerResult<VideoPlayerStore>;
  children: ReactNode;
  offsetStart: number;
  duration: number;
  range?: PlaybackRange;
  ready: boolean;
  zoomed: boolean;
  rootRef: RefObject<HTMLDivElement | null>;
  sources: PlayerSource[];
  activeSource: PlayerSource | null;
  seek: (position: number) => void;
  togglePaused: () => void;
  selectSource: (source: PlayerSource) => void;
  resetQuality: () => void;
  retry: () => void;
  resetZoom: () => void;
}) {
  const store = Player.usePlayer();
  const [listeners] = useState(() => new Set<() => void>());
  const latest = useCommittedRef(options);
  // Presentation options commit independently of media events. Scalar
  // subscribers compare their selected value before scheduling a render.
  useLayoutEffect(() => {
    for (const notify of listeners) notify();
  });
  const value = useMemo<ControlContext>(() => {
    const tracks = () =>
      latest.current.rootRef.current?.querySelector("video")?.textTracks;
    const setCaption = (index: number | null) => {
      const list = tracks();
      if (!list) return;
      for (let i = 0; i < list.length; i++) {
        const track = list[i];
        if (track) track.mode = i === index ? "showing" : "disabled";
      }
    };
    return {
      subscribe: (listener) => {
        listeners.add(listener);
        const unsubscribe = store.subscribe(listener);
        return () => {
          listeners.delete(listener);
          unsubscribe();
        };
      },
      commands: {
        read: () => ({
          paused: store.state.paused,
          muted: store.state.muted,
          volume: store.state.volume,
          rate: store.state.playbackRate,
          position: latest.current.offsetStart + store.state.currentTime,
          ready: latest.current.ready,
          zoomed: latest.current.zoomed,
          error: store.state.error?.message ?? null,
          canPip: store.state.pipAvailability === "available",
        }),
        play: () => {
          if (store.state.paused) latest.current.togglePaused();
        },
        pause: () => store.pause(),
        togglePaused: () => latest.current.togglePaused(),
        seek: (position) => {
          if (!Number.isFinite(position)) return;
          const { range, duration, seek } = latest.current;
          seek(
            Math.max(
              range?.start ?? 0,
              Math.min(position, range?.end ?? duration),
            ),
          );
        },
        setVolume: (volume) => {
          if (Number.isFinite(volume))
            store.setVolume(Math.max(0, Math.min(1, volume)));
        },
        toggleMuted: () => store.toggleMuted(),
        setRate: (rate) => {
          if (Number.isFinite(rate) && rate >= 0.25 && rate <= 16)
            store.setPlaybackRate(rate);
        },
        setCaption,
        toggleCaptions: () => {
          const list = tracks();
          setCaption(
            list && Array.from(list).some((track) => track.mode === "showing")
              ? null
              : 0,
          );
        },
        selectSource: (src) => {
          const source = latest.current.sources.find(
            (item) => item.src === src,
          );
          if (source) latest.current.selectSource(source);
        },
        resetQuality: () => latest.current.resetQuality(),
        retry: () => latest.current.retry(),
        resetZoom: () => latest.current.resetZoom(),
        togglePip: () => {
          if (store.state.pipAvailability === "available")
            void store.togglePictureInPicture();
        },
      },
    };
  }, [store, listeners]);
  const sources = useMemo(
    () => ({ sources: options.sources, activeSource: options.activeSource }),
    [options.sources, options.activeSource],
  );
  return (
    <Context.Provider value={value}>
      <SourcesContext.Provider value={sources}>
        {children}
      </SourcesContext.Provider>
    </Context.Provider>
  );
}
