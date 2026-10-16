import { memo, useMemo, type ComponentPropsWithRef } from "react";
import { HlsJsVideo } from "@videojs/react/media/hlsjs-video";
import { ContentTypes, type HlsSource } from "@videojs/hlsjs-video";
import { isHlsPlaylist, parseStartPosition } from "./hls";

type SceneVideoProps = Omit<
  ComponentPropsWithRef<typeof HlsJsVideo>,
  "source"
> & {
  /** Direct endpoints and offline blob URLs need an explicit MIME type. */
  sourceType?: string;
  /** Requested position in playlist time, separate from a clip's fixed bounds. */
  startPosition?: number;
};

/**
 * One native video element for both direct files and HLS. Video.js chooses
 * the playback adapter from the structured source's type, rebuilding the
 * engine when necessary while retaining the element and its audio state.
 */
export const SceneVideo = memo(function SceneVideo({
  src,
  sourceType,
  startPosition,
  ...props
}: SceneVideoProps) {
  const source = useMemo<HlsSource>(() => {
    // A null source is inferred as HLS and attaches an empty MediaSource.
    // Explicit native idle releases the old engine without replacing the video.
    if (!src) return { src: "", type: ContentTypes.MP4 };
    if (!isHlsPlaylist(src)) {
      return { src, type: sourceType ?? ContentTypes.MP4 };
    }

    return {
      src,
      type: ContentTypes.M3U8,
      engine: {
        hlsJs: {
          // A resumed clip must load at its playhead, while its URL keeps the
          // entire clip seekable. Standalone sources can still use URL hints.
          startPosition: startPosition ?? parseStartPosition(src),
          // hls.js otherwise retains the entire played portion of a VOD.
          // At 4K this can accumulate hundreds of MB before Safari evicts it.
          // Keep a small rewind window and enough ahead for MMS quota cycles.
          backBufferLength: 10,
          maxBufferLength: 30,
          maxMaxBufferLength: 60,
          // This is a buffering target, not a hard memory ceiling: hls.js
          // uses the larger of this byte target and maxBufferLength seconds.
          maxBufferSize: 60 * 1024 * 1024,
        },
      },
    };
  }, [src, sourceType, startPosition]);

  return <HlsJsVideo source={source} {...props} />;
});
