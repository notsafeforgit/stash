/**
 * Custom `HlsJsMedia` bridge for the scene player.
 *
 * Video.js beta.25 fixed the packaged component's unstable attach ref, but
 * this wrapper still owns two scene-specific requirements that
 * `<HlsJsVideo>` cannot express:
 *
 * - inject the requested start position and ManagedMediaSource buffer limits
 *   before the source's deferred engine load begins;
 * - apply `muted` to the element synchronously at attach time so iOS can make
 *   its autoplay decision from the correct initial state.
 *
 * `HlsJsMedia` auto-selects hls.js on MSE-capable browsers and native HLS
 * otherwise. Its beta.25 configuration nests hls.js options under `hlsJs`.
 */
import { useComposedRefs, useMediaInstance } from "@videojs/react";
import {
  HlsJsMedia,
  type HlsMediaConfig,
  type HlsMediaProps,
} from "@videojs/core/dom/media/hls-js";
import {
  forwardRef,
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
} from "react";
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

    // Mirrors beta.25's internal `useSyncProps`: media-owned properties must
    // be assigned to HlsJsMedia rather than spread onto the <video>. Setting
    // `src` schedules a microtask load; the layout effect below runs first and
    // installs the hls.js configuration used to construct that engine.
    if (mediaApi.src !== src) mediaApi.src = src;
    if (mediaApi.preload !== preload) mediaApi.preload = preload;

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
    // Wired via `useLayoutEffect`: the effect runs synchronously in
    // commit (after refs are assigned, before microtasks flush). The
    // microtask that `HlsJsMedia.src = url` scheduled via `#requestLoad`
    // hasn't fired yet, so when its `load()` reads `this.config` to
    // construct the HlsJsMedia delegate, it picks up our updated
    // startPosition. Setting `config` here also calls `#requestLoad`
    // again, but that's a no-op because a load is already scheduled —
    // so the engine is constructed exactly once, with the right
    // startPosition.
    const startPosition = useMemo(() => parseStartPosition(src), [src]);
    useLayoutEffect(() => {
      const hlsJs: NonNullable<HlsMediaConfig["hlsJs"]> = { startPosition };
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
      if ("ManagedMediaSource" in window) {
        hlsJs.maxBufferLength = 60;
        hlsJs.maxMaxBufferLength = 120;
        hlsJs.maxBufferSize = 240 * 1024 * 1024;
      }
      mediaApi.config = {
        ...mediaApi.config,
        hlsJs,
      };
    }, [mediaApi, startPosition]);
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
