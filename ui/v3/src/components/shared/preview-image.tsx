import { useState, type ComponentProps } from "react";
import type {
  PreviewImageDataFragment,
  PreviewImageRenditionDataFragment,
} from "@/core/generated-graphql";

export type PreviewImageData = PreviewImageDataFragment;

export type PreviewImageProps = ComponentProps<"img"> & {
  preview?: PreviewImageData | null;
  /** Prefer stored card renditions, falling back to existing full-size covers. */
  thumbnail?: boolean;
  alt: string;
};

/** One rendering policy for covers, markers and player posters. Adaptive AVIF
 * contains its SDR rendering; plain HDR is selected only on HDR displays. The
 * browser handles format support, display changes and colour management. */
export function PreviewImage({
  thumbnail,
  preview,
  ...props
}: PreviewImageProps) {
  const rendition = (thumbnail && preview?.thumbnail) || preview;
  return (
    <PreviewImageContent
      key={`${rendition?.fallback ?? ""}\n${props.src ?? ""}`}
      {...props}
      preview={rendition}
    />
  );
}

function PreviewImageContent({
  preview,
  src,
  onError,
  alt,
  ...props
}: Omit<PreviewImageProps, "preview" | "thumbnail"> & {
  preview?: PreviewImageRenditionDataFragment | null;
}) {
  const [fallbackLevel, setFallbackLevel] = useState(0);
  const fallback = fallbackLevel < 2 ? (preview?.fallback ?? src) : src;
  return (
    <picture className="contents">
      {fallbackLevel === 0 &&
        preview?.sources.map((source) => (
          <source
            key={source.url}
            srcSet={source.url}
            type={source.mime_type}
            media={
              source.dynamic_range === "HDR"
                ? "(dynamic-range: high)"
                : undefined
            }
            width={source.width}
            height={source.height}
          />
        ))}
      <img
        {...props}
        src={fallback || undefined}
        alt={alt}
        onError={(event) => {
          if (preview && fallbackLevel < 2) {
            const image = event.currentTarget;
            const failedURL = image.currentSrc || image.src;
            const fallbackURL = new URL(
              preview.fallback,
              image.ownerDocument.baseURI,
            ).href;
            // If the browser already chose JPEG (unsupported AVIF or SDR
            // display), changing only <source> won't retry that same URL.
            setFallbackLevel(failedURL === fallbackURL ? 2 : fallbackLevel + 1);
          } else {
            onError?.(event);
          }
        }}
      />
    </picture>
  );
}
