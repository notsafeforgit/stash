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
  const source = useMemo<HlsSource | null>(() => {
    if (!src) return null;
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
          // MMS rate-limits segment fetches. These existing ceilings leave
          // enough buffered video to cover its quota cycles on iPhones.
          ...(typeof window !== "undefined" &&
            "ManagedMediaSource" in window && {
              maxBufferLength: 60,
              maxMaxBufferLength: 120,
              maxBufferSize: 240 * 1024 * 1024,
            }),
        },
      },
    };
  }, [src, sourceType, startPosition]);

  return <HlsJsVideo source={source} {...props} />;
});
