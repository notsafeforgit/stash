export interface PlatformPlayback {
  paused: boolean;
  ended: boolean;
  currentTime: number;
  duration: number;
  playbackRate: number;
}

export interface PlatformMediaOptions {
  metadata: MediaMetadataInit;
  offsetStart: number;
  duration?: number;
  suspended: boolean;
  seek(time: number): void;
  /** Share explicit pause intent and frame preservation with the app controls. */
  pause?: () => void;
  next?: () => void;
  previous?: () => void;
}

let owner: symbol | undefined;
const actions = [
  "play",
  "pause",
  "stop",
  "seekto",
  "seekbackward",
  "seekforward",
  "nexttrack",
  "previoustrack",
] as const satisfies readonly MediaSessionAction[];

/** The most recently started player owns OS controls. A paused preview cannot
 * replace it, and an old player's cleanup cannot clear a newer session. */
export function createPlatformMediaSession(
  playback: { state: PlatformPlayback; play(): unknown; pause(): unknown },
  options: () => PlatformMediaOptions,
) {
  const token = Symbol("media-session");
  const session = navigator.mediaSession;
  let wasPlaying = false;
  let previousMetadata: MediaMetadataInit | undefined;
  const handle = (
    action: MediaSessionAction,
    handler: MediaSessionActionHandler | null,
  ) => {
    // Safari exposes MediaSession without supporting every action.
    try {
      session?.setActionHandler(action, handler);
    } catch {}
  };
  const position = () => {
    const config = options();
    const duration = config.duration ?? playback.state.duration;
    return {
      duration,
      position: Math.max(
        0,
        Math.min(duration, config.offsetStart + playback.state.currentTime),
      ),
    };
  };
  const seek = (time: number) => {
    const { duration } = position();
    if (Number.isFinite(time) && Number.isFinite(duration) && duration > 0)
      options().seek(Math.max(0, Math.min(duration, time)));
  };
  const pause = () => {
    const handler = options().pause;
    if (handler) handler();
    else playback.pause();
  };
  const clear = () => {
    if (!session || owner !== token) return;
    owner = undefined;
    for (const action of actions) handle(action, null);
    session.metadata = null;
    session.playbackState = "none";
    try {
      session.setPositionState?.();
    } catch {}
    previousMetadata = undefined;
  };
  const refresh = () => {
    if (!session) return;
    const config = options();
    const playing =
      !config.suspended && !playback.state.paused && !playback.state.ended;
    if (playing && !wasPlaying) {
      owner = token;
      previousMetadata = undefined;
    }
    wasPlaying = playing;
    if (owner !== token) return;
    if (config.suspended) {
      clear();
      return;
    }
    if (
      previousMetadata !== config.metadata &&
      typeof MediaMetadata !== "undefined"
    ) {
      session.metadata = new MediaMetadata(config.metadata);
      previousMetadata = config.metadata;
    }
    session.playbackState = playing ? "playing" : "paused";
    handle("play", () => {
      void Promise.resolve(playback.play()).catch(() => {});
    });
    handle("pause", pause);
    handle("stop", () => {
      pause();
      clear();
    });
    handle("seekto", (event) => {
      if (event.seekTime !== undefined) seek(event.seekTime);
    });
    handle("seekbackward", (event) =>
      seek(position().position - (event.seekOffset ?? 10)),
    );
    handle("seekforward", (event) =>
      seek(position().position + (event.seekOffset ?? 10)),
    );
    handle("nexttrack", config.next ?? null);
    handle("previoustrack", config.previous ?? null);
    const current = position();
    try {
      session.setPositionState?.(
        Number.isFinite(current.duration) &&
          current.duration > 0 &&
          Number.isFinite(current.position)
          ? {
              ...current,
              playbackRate:
                playback.state.playbackRate > 0
                  ? playback.state.playbackRate
                  : 1,
            }
          : undefined,
      );
    } catch {}
  };
  return { refresh, dispose: clear };
}
