import type React from "react";
import {
  ChevronLeft,
  ChevronRight,
  Images,
  Maximize,
  Minimize,
  Pause,
  Play,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";

// YARL's `.yarl__icon` rule sets `width / height: var(--yarl__icon_size,
// 32px)` and the surrounding `.yarl__button` provides colour + hover
// state via `currentColor`. Reusing the same class on a Lucide SVG keeps
// our overrides pixel-identical to YARL's defaults — no separate sizing
// or theming needed.
const Icon = ({
  Component,
}: {
  Component: React.ComponentType<{ className?: string }>;
}) => <Component className="yarl__icon" />;

/**
 * Render-prop overrides that swap YARL's bundled Material-style SVGs for
 * Lucide equivalents. Spread into a `<YARLightbox render={...} />` block.
 * Covers every icon slot YARL exposes for the plugins this app uses
 * (core + Zoom + Fullscreen + Slideshow + Thumbnails); slots from
 * unused plugins (Captions, Share, Download) are intentionally omitted.
 */
export const lightboxIconRenders = {
  iconPrev: () => <Icon Component={ChevronLeft} />,
  iconNext: () => <Icon Component={ChevronRight} />,
  iconClose: () => <Icon Component={X} />,
  iconZoomIn: () => <Icon Component={ZoomIn} />,
  iconZoomOut: () => <Icon Component={ZoomOut} />,
  iconEnterFullscreen: () => <Icon Component={Maximize} />,
  iconExitFullscreen: () => <Icon Component={Minimize} />,
  iconSlideshowPlay: () => <Icon Component={Play} />,
  iconSlideshowPause: () => <Icon Component={Pause} />,
  iconThumbnailsVisible: () => <Icon Component={Images} />,
  iconThumbnailsHidden: () => <Icon Component={Images} />,
} as const;
