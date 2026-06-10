/**
 * Drop-in replacement for `@videojs/react`'s `<HlsVideo>` that keeps the
 * `HlsMedia` attach ref stable across re-renders.
 *
 * The packaged component composes its callback ref via
 * `useComposedRefs(attachMediaElement(mediaApi), ref)` — `attachMediaElement`
 * returns a fresh closure on every call, so `useComposedRefs`'s `useCallback`
 * produces a new callback on every render. React then fires the old
 * ref(null) cleanup (`media.detach()`) followed by the new ref(element)
 * (`media.attach(element)`) on every re-render of `<HlsVideo>`. Each
 * detach/attach round-trip tears the underlying engine off the video
 * element; in-flight segment appends from hls.js can land on a now-detached
 * SourceBuffer and surface as
 *
 *   InvalidStateError: This SourceBuffer has been removed from the
 *   parent media source
 *
 * Same shape of bug that's been flagged for `<NativeHlsVideo>` and
 * `<SimpleHlsVideo>` — different engines under the hood (hls.js for MSE
 * browsers, AVFoundation for Safari) but the same React-side ref
 * instability.
 *
 * The fix is one line: `useCallback` the attach callback so its identity
 * is stable while `mediaApi` is stable. `mediaApi` is itself stable
 * (`useState(() => new HlsMedia())`), so the memo is keyed on a single
 * stable value.
 *
 * `HlsMedia` from `@videojs/core` is a smart wrapper that auto-selects
 * between hls.js (MSE-capable browsers, including Safari) and the
 * browser's native HLS via the `preferPlayback` setting. We let it
 * default to `'mse'` so hls.js drives playback uniformly across Chrome,
 * Firefox, Edge, and Safari — single client to satisfy, no AVFoundation
 * src-reassign teardown to work around.
 */
import { mediaProps, useComposedRefs, useMediaInstance } from "@videojs/react";
import { HlsMedia } from "@videojs/core/dom/media/hls";
import {
  forwardRef,
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
} from "react";
import type { ReactNode, VideoHTMLAttributes } from "react";
import { parseStartPosition } from "./hls";

type StableHlsVideoProps = VideoHTMLAttributes<HTMLVideoElement> & {
  children?: ReactNode;
};

export const StableHlsVideo = forwardRef<HTMLVideoElement, StableHlsVideoProps>(
  function StableHlsVideo({ children, muted, ...props }, ref) {
    const mediaApi = useMediaInstance(HlsMedia);
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
    // microtask that `HlsMedia.src = url` scheduled via `#requestLoad`
    // hasn't fired yet, so when its `load()` reads `this.config` to
    // construct the HlsJsMedia delegate, it picks up our updated
    // startPosition. Setting `config` here also calls `#requestLoad`
    // again, but that's a no-op because a load is already scheduled —
    // so the engine is constructed exactly once, with the right
    // startPosition.
    const startPosition = useMemo(
      () => parseStartPosition(props.src as string | undefined),
      [props.src],
    );
    useLayoutEffect(() => {
      const cfg: Record<string, unknown> = { startPosition };
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
        cfg.maxBufferLength = 60;
        cfg.maxMaxBufferLength = 120;
        cfg.maxBufferSize = 240 * 1024 * 1024;
      }
      mediaApi.config = cfg;
    }, [mediaApi, startPosition]);
    // Inline + memoised attach callback — see the file header comment.
    // Going through the upstream `attachMediaElement(mediaApi)` factory
    // produces a fresh closure on every render and the React 19 cleanup
    // detaches the engine on each one; calling `media.attach`/`detach`
    // directly inside a `useCallback` keyed on the stable mediaApi
    // gives a single mount-time attach and a single unmount-time detach.
    const attachRef = useCallback(
      (element: HTMLVideoElement | null) => {
        if (element) {
          mediaApi.attach(element);
          // `mediaProps` strips every prop that has a settable accessor
          // on `HlsMedia`'s prototype chain — `muted`, `playbackRate`,
          // etc. — and tries to apply them to `mediaApi` instead. But
          // `mediaApi.target` is `null` during the render that calls
          // `mediaProps`, so the setter (which checks `if (this.target)`)
          // silently drops the value, and `attach` does NOT replay
          // queued props onto the new target. For `muted` specifically
          // that breaks iOS Safari's muted-autoplay path: the new
          // `<video>` ends up with `autoplay` but no `muted`, iOS
          // refuses to autoplay, the source-change spinner stalls, and
          // the user has to tap play. We sidestep this entirely by
          // omitting `muted` from the props handed to `mediaProps` (see
          // below) and applying it imperatively here, before iOS makes
          // its autoplay decision (which it makes after the first
          // metadata load, well after this attach fires). Reactive
          // updates after mount are handled by the `muted` prop passed
          // to `<video>` directly, which React syncs to the IDL
          // property as it does for any controlled video attribute.
          const m = mutedRef.current;
          if (typeof m === "boolean") element.muted = m;
        } else {
          mediaApi.detach();
        }
      },
      [mediaApi],
    );
    // Pass `props` (without `muted`) to `mediaProps` so it can't drop
    // the muted value into a null target. We own `muted` via `attachRef`
    // for mount and via React's reconciliation for subsequent updates.
    const restProps = mediaProps(mediaApi, HlsMedia, props);
    return (
      <video ref={useComposedRefs(attachRef, ref)} muted={muted} {...restProps}>
        {children}
      </video>
    );
  },
);
