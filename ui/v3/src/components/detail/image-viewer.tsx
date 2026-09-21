import { useMemo, useRef, useState } from "react";
import YARLightbox, { type ZoomRef } from "yet-another-react-lightbox";
import Inline from "yet-another-react-lightbox/plugins/inline";
import Zoom from "yet-another-react-lightbox/plugins/zoom";
import "yet-another-react-lightbox/styles.css";
import type * as GQL from "@/core/generated-graphql";
import { imageTitle } from "@/core/files";
import { useMediaQuery } from "@/utils/screen";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  LIGHTBOX_ZOOM_TUNING,
  OriginalSizeButton,
  LightboxImageActionsButton,
  useAtOriginalSize,
} from "@/components/lightbox/lightbox";
import { lightboxIconRenders } from "@/components/lightbox/lightbox-icons";

type ImageData = NonNullable<GQL.FindImageQuery["findImage"]>;

export function ImageViewer({ image }: { image: ImageData }) {
  const src = image.paths.image ?? image.paths.preview ?? undefined;
  const file = image.visual_files[0];
  const [open, setOpen] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const inlineZoomRef = useRef<ZoomRef>(null);
  const modalZoomRef = useRef<ZoomRef>(null);
  const [inlineAtOriginal, inlineZoomCallbacks] =
    useAtOriginalSize(inlineZoomRef);
  const [modalAtOriginal, modalZoomCallbacks] = useAtOriginalSize(modalZoomRef);
  // Matches the `lg:` breakpoint used by `MediaDetailLayout` to switch from a
  // single-column scrolling page (mobile) to the side-by-side desktop
  // layout where the image gets a fixed-height column it can fill.
  const isDesktop = useMediaQuery("(min-width: 1024px)");

  // Reset the loaded flag when the underlying image src changes
  // (navigation between images via the router reuses this component).
  // Render-time state adjustment rather than an effect so the stale
  // image never paints as "loaded".
  const [loadedSrc, setLoadedSrc] = useState(src);
  if (loadedSrc !== src) {
    setLoadedSrc(src);
    setLoaded(false);
  }

  const slides = useMemo(
    () =>
      src
        ? [
            {
              src,
              alt: imageTitle(image) || undefined,
              width: file?.width ?? undefined,
              height: file?.height ?? undefined,
              filePaths: image.visual_files.map((f) => f.path),
            },
          ]
        : [],
    [src, image, file?.width, file?.height],
  );

  if (!src) {
    return (
      <div className="flex items-center justify-center w-full h-full bg-black text-muted-foreground text-sm">
        No image available
      </div>
    );
  }

  // Desktop: the lightbox renders inline so pan / zoom is available
  // immediately without a tap-to-open round trip. The Inline plugin
  // suppresses the close button and pins the lightbox to its container,
  // which we size to fill the detail-page primary column.
  //
  // Mobile is left on the tap-to-open modal flow — an inline lightbox
  // there fights page scroll once the image is zoomed (the controller
  // captures vertical pans), and the surrounding page is the scroll
  // container under `mobilePageScroll`.
  if (isDesktop) {
    return (
      <YARLightbox
        open
        close={() => {}}
        slides={slides}
        index={0}
        plugins={[Inline, Zoom]}
        inline={{ className: "w-full h-full bg-black" }}
        controller={{ disableSwipeNavigation: true }}
        carousel={{ finite: true }}
        animation={{ zoom: 250 }}
        zoom={{
          ...LIGHTBOX_ZOOM_TUNING,
          scrollToZoom: true,
          ref: inlineZoomRef,
        }}
        on={inlineZoomCallbacks}
        toolbar={{
          buttons: [
            <LightboxImageActionsButton key="image-actions" />,
            "zoom",
            <OriginalSizeButton
              key="original-size"
              zoomRef={inlineZoomRef}
              atOriginal={inlineAtOriginal}
            />,
          ],
        }}
        render={{
          ...lightboxIconRenders,
          buttonPrev: () => null,
          buttonNext: () => null,
        }}
        className="image-lightbox lightbox-mobile-toolbar-bottom"
      />
    );
  }

  // Mobile tap-to-open: the inline view is a plain `<img>` rendered at
  // its natural aspect ratio at full width so the user scrolls past it.
  // Tapping opens the modal yarl below, which has full-screen real
  // estate to actually pinch-zoom into.
  //
  // The img's `width`/`height` HTML attributes (not CSS) give browsers
  // the intrinsic aspect ratio so they reserve the correct vertical
  // space before the bytes arrive — no jump on slow connections. A
  // Skeleton overlays the reserved area until `onLoad` fires.
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        onClick={() => setOpen(true)}
        aria-label="Open image"
        className="relative block h-auto w-full bg-black p-0 cursor-zoom-in rounded-none border-0 overflow-hidden hover:bg-black"
      >
        {!loaded && (
          <Skeleton className="absolute inset-0 rounded-none bg-muted/40" />
        )}
        <img
          src={src}
          alt={imageTitle(image)}
          width={file?.width ?? undefined}
          height={file?.height ?? undefined}
          onLoad={() => setLoaded(true)}
          className="block w-full h-auto"
        />
      </Button>

      <YARLightbox
        open={open}
        close={() => setOpen(false)}
        slides={slides}
        index={0}
        plugins={[Zoom]}
        controller={{ disableSwipeNavigation: true }}
        carousel={{ finite: true }}
        animation={{ zoom: 250 }}
        zoom={{
          ...LIGHTBOX_ZOOM_TUNING,
          scrollToZoom: true,
          ref: modalZoomRef,
        }}
        on={modalZoomCallbacks}
        toolbar={{
          buttons: [
            <LightboxImageActionsButton key="image-actions" />,
            "zoom",
            <OriginalSizeButton
              key="original-size"
              zoomRef={modalZoomRef}
              atOriginal={modalAtOriginal}
            />,
            "close",
          ],
        }}
        render={{
          ...lightboxIconRenders,
          buttonPrev: () => null,
          buttonNext: () => null,
        }}
        className="image-lightbox lightbox-mobile-toolbar-bottom"
      />
    </>
  );
}
