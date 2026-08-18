/**
 * Custom `HlsJsMedia` bridge for the scene player.
 *
 * Video.js beta.27 fixed the packaged component's unstable attach ref, but
 * this wrapper still owns two scene-specific requirements that
 * `<HlsJsVideo>` cannot express:
 *
 * - inject the requested start position and ManagedMediaSource buffer limits
 *   before the source's deferred engine load begins;
 * - apply `muted` to the element synchronously at attach time so iOS can make
 *   its autoplay decision from the correct initial state.
 *
 * `HlsJsMedia` auto-selects hls.js on MSE-capable browsers and native HLS
 * otherwise. Its structured source nests hls.js options under `engine.hlsJs`.
 */
import { useComposedRefs, useMediaInstance } from "@videojs/react";
import {
  HlsJsMedia,
  type HlsEngineConfig,
  type HlsMediaProps,
  type HlsSource,
} from "@videojs/media/dom/hls-js";
import { forwardRef, useCallback, useMemo, useRef } from "react";
import type { ReactNode, VideoHTMLAttributes } from "react";
import { parseStartPosition } from "./hls";

type StableHlsVideoProps = Omit<
  VideoHTMLAttributes<HTMLVideoElement>,
  "preload" | "src"
> & {
  children?: ReactNode;
  preload?: HlsMediaProps["preload"];
  src?: string;
};

export const StableHlsVideo = forwardRef<HTMLVideoElement, StableHlsVideoProps>(
  function StableHlsVideo(
    { children, muted, preload = "metadata", src = "", ...videoProps },
    ref,
  ) {
    const mediaApi = useMediaInstance(HlsJsMedia);

    // Live `muted` mirror so `attachRef` can read the latest value at
    // mount time without taking `muted` as a dep (which would cause a
    // detach/reattach on every mute toggle, defeating the whole point
    // of this wrapper).
    const mutedRef = useRef(muted);
    mutedRef.current = muted;

    // hls.js's `config.startPosition` decides which fragment is loaded
    // first when the manifest is parsed. Default (-1) makes hls.js
    // start at fragment 0 for VOD — fine for a freshly loaded scene,
    // but wasteful when the user has a resume_time or deep-link `?t=`
    // (the server would transcode segment 0 only to be told to seek to
    // segment N seconds later, costing one ffmpeg restart). Setting
    // `startPosition` from the URL's `?start=` parses cleanly and
    // means the FIRST segment request hls.js makes is the right one.
    //
    // beta.27's structured source applies the URL and engine options in one
    // assignment, so the deferred load always sees the requested start
    // position and constructs the engine exactly once.
    const startPosition = useMemo(() => parseStartPosition(src), [src]);
    const source = useMemo<HlsSource>(() => {
      const hlsJs: NonNullable<HlsEngineConfig["hlsJs"]> = { startPosition };
      // iOS Safari (iOS 17+) exposes ManagedMediaSource instead of MSE
      // and rate-limits hls.js's segment fetches via quota signals.
      // hls.js's default `maxBufferSize` (60 MB) caps the byte buffer
      // well below the time-based `maxBufferLength` (30 s) on a 4K
      // ~50 Mbps stream — even when the server is producing well over
      // realtime, MMS releases bytes in pulses and the player ends up
      // with a thin ~10 s buffer that drains visibly on any hiccup.
      // Raising the ceilings on the MMS path lets hls.js queue more
      // ahead whenever MMS allows; MMS still controls actual quota so
      // we can't force it past its memory budget. No effect on desktop
      // MSE because that path hits the time cap first.
      if (typeof window !== "undefined" && "ManagedMediaSource" in window) {
        hlsJs.maxBufferLength = 60;
        hlsJs.maxMaxBufferLength = 120;
        hlsJs.maxBufferSize = 240 * 1024 * 1024;
      }
      return {
        src,
        engine: { hlsJs },
      };
    }, [src, startPosition]);

    // Mirrors beta.27's internal `useSyncProps`: media-owned properties must
    // be assigned to HlsJsMedia rather than spread onto the <video>.
    if (mediaApi.source !== source) mediaApi.source = source;
    if (mediaApi.preload !== preload) mediaApi.preload = preload;

    // Inline attach callback so muted is installed before the engine's source
    // load. Its stable identity also guarantees one mount-time attach and one
    // unmount-time detach across ordinary React re-renders.
    const attachRef = useCallback(
      (element: HTMLVideoElement | null) => {
        if (element) {
          mediaApi.attach(element);
          // Apply the initial value imperatively before iOS evaluates muted
          // autoplay. Reactive updates after mount remain owned by React via
          // the controlled `muted` prop below.
          const m = mutedRef.current;
          if (typeof m === "boolean") element.muted = m;
        } else {
          mediaApi.detach();
        }
      },
      [mediaApi],
    );
    return (
      <video
        ref={useComposedRefs(attachRef, ref)}
        muted={muted}
        {...videoProps}
      >
        {children}
      </video>
    );
  },
);
