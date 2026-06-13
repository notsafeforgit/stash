import "yet-another-react-lightbox/styles.css";

import React, { useMemo, useRef, useState } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useSmartBack } from "src/hooks/use-smart-back";
import { useQuery, useMutation } from "@apollo/client/react";
import { zodValidator } from "@tanstack/zod-adapter";
import { z } from "zod";
import { useIntl } from "react-intl";
import YARLightbox, { type ZoomRef } from "yet-another-react-lightbox";
import Zoom from "yet-another-react-lightbox/plugins/zoom";
import Inline from "yet-another-react-lightbox/plugins/inline";
import { Spinner } from "src/components/ui/spinner";
import { Skeleton } from "src/components/ui/skeleton";
import { Button } from "src/components/ui/button";
import { cn } from "src/lib/utils";
import { useMediaQuery } from "src/utils/screen";
import {
  Star,
  CheckCircle2Icon,
  Droplets,
  Minus,
  RotateCcw,
  Pencil,
  ChevronLeft,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { imageTitle } from "src/core/files";
import {
  MediaDetailLayout,
  type DetailTab,
} from "src/components/detail/media-detail-layout";
import {
  ImageDetailsTab,
  ImageFileInfoTab,
} from "src/components/detail/image-detail-tabs";
import { ImageEditForm } from "src/components/detail/image-edit-form";
import { ImageActionsMenu } from "src/components/detail/image-actions-menu";
import { DetailEditTransition } from "src/components/detail/detail-edit-transition";
import {
  LIGHTBOX_ZOOM_TUNING,
  OriginalSizeButton,
  lightboxIconRenders,
  useAtOriginalSize,
} from "src/components/lightbox";
import { useImageOCounter } from "src/hooks/use-image-o-counter";
import { useDocumentTitle } from "src/hooks/title";

// ── Route search params ────────────────────────────────────────────────────────

const searchSchema = z.object({
  tab: z.string().optional(),
});

// ── Image viewer ──────────────────────────────────────────────────────────────

type ImageData = NonNullable<GQL.FindImageQuery["findImage"]>;

function ImageViewer({ image }: { image: ImageData }) {
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

// ── Image toolbar ─────────────────────────────────────────────────────────────

interface ImageToolbarProps {
  image: ImageData;
  onAddO: () => void;
  onSubO: () => void;
  onResetO: () => void;
  onToggleOrganized: () => void;
  onDeleted?: () => void;
}

function ImageToolbar({
  image,
  onAddO,
  onSubO,
  onResetO,
  onToggleOrganized,
  onDeleted,
}: ImageToolbarProps) {
  const intl = useIntl();
  const oCounter = image.o_counter ?? 0;

  return (
    <div className="flex items-center gap-3 py-1.5 flex-wrap">
      <div className="flex items-center flex-wrap gap-1">
        {image.rating100 != null && (
          <span
            className="inline-flex items-center bg-transparent border border-border rounded-md text-muted-foreground text-[0.8125rem] gap-1 px-2 py-1 mr-1"
            title={intl.formatMessage({
              id: "rating",
              defaultMessage: "Rating",
            })}
          >
            <Star size={14} />
            {image.rating100}
          </span>
        )}
        {/* O-counter: images don't track an o_history, so decrement and
            reset live alongside the +1 increment in the toolbar instead
            of behind the edit form. */}
        <Button
          variant="outline"
          className="h-auto bg-transparent px-2 py-1 text-[0.8125rem] gap-1 text-muted-foreground hover:text-foreground"
          onClick={onAddO}
          title={intl.formatMessage({
            id: "o_counter",
            defaultMessage: "Add O",
          })}
        >
          <Droplets size={14} />
          {oCounter}
        </Button>
        <Button
          variant="outline"
          size="icon-sm"
          className="bg-transparent text-muted-foreground hover:text-foreground"
          onClick={onSubO}
          disabled={oCounter <= 0}
          title={intl.formatMessage({
            id: "actions.decrement_o",
            defaultMessage: "Decrement O",
          })}
        >
          <Minus size={13} />
        </Button>
        {oCounter > 0 && (
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground hover:text-foreground"
            onClick={onResetO}
            title={intl.formatMessage({
              id: "actions.reset_o",
              defaultMessage: "Reset O",
            })}
          >
            <RotateCcw size={13} />
          </Button>
        )}
      </div>

      <Button
        variant="outline"
        size="sm"
        onClick={onToggleOrganized}
        className={cn(
          image.organized &&
            "text-green-600 border-green-500/60 hover:text-green-500",
        )}
        title={intl.formatMessage({
          id: "organized",
          defaultMessage: "Organized",
        })}
      >
        <CheckCircle2Icon
          size={13}
          className={image.organized ? "fill-green-600/20" : ""}
        />
        {intl.formatMessage({ id: "organized", defaultMessage: "Organized" })}
      </Button>

      <div className="ml-auto">
        <ImageActionsMenu image={image} onDeleted={onDeleted} />
      </div>
    </div>
  );
}

// ── Image detail page ─────────────────────────────────────────────────────────

function ImageDetailPage() {
  const { imageId } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const goBack = useSmartBack("/images");
  const intl = useIntl();

  const { data, loading, error } = useQuery(GQL.FindImageDocument, {
    variables: { id: imageId },
    fetchPolicy: "cache-first",
  });

  const [updateImage] = useMutation(GQL.ImageUpdateDocument);
  function handleToggleOrganized() {
    if (!image) return;
    updateImage({
      variables: { input: { id: imageId, organized: !image.organized } },
      optimisticResponse: {
        imageUpdate: { ...image, organized: !image.organized },
      },
    });
  }

  const { incrementO, decrementO, resetO } = useImageOCounter(imageId);

  const activeTab = tab ?? "details";
  function setActiveTab(id: string) {
    navigate({ search: (prev) => ({ ...prev, tab: id }), replace: true });
  }

  // Inline edit transition on the Details tab — replaces the previous
  // top-level "Edit" tab. See `routes/scenes/$sceneId.tsx` for the
  // full rationale; reset to false whenever the user navigates away
  // from Details so re-entering the tab lands on the read-only view.
  const [editingDetails, setEditingDetails] = useState(false);
  React.useEffect(() => {
    if (activeTab !== "details") setEditingDetails(false);
  }, [activeTab]);

  const image = data?.findImage;
  useDocumentTitle(image ? imageTitle(image) || undefined : undefined);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-48 text-muted-foreground">
        <Spinner className="size-10" />
      </div>
    );
  }

  if (error || !image) {
    return (
      <div className="p-4 text-destructive">
        {error?.message ??
          intl.formatMessage({
            id: "image_not_found",
            defaultMessage: "Image not found",
          })}
      </div>
    );
  }

  const tabs: DetailTab[] = [
    {
      id: "details",
      label: intl.formatMessage({ id: "details", defaultMessage: "Details" }),
      shortcut: "a",
      content: (
        <DetailEditTransition
          editing={editingDetails}
          detail={
            <div className="flex flex-col gap-3">
              <div className="flex justify-end">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setEditingDetails(true)}
                >
                  <Pencil size={13} />
                  {intl.formatMessage({
                    id: "actions.edit",
                    defaultMessage: "Edit",
                  })}
                </Button>
              </div>
              <ImageDetailsTab image={image} />
            </div>
          }
          editForm={
            <div className="flex flex-col h-full">
              {/* Header sized to match `DetailSidebarBack`. */}
              <div className="flex shrink-0 items-center gap-1 px-1 py-1 border-b border-border">
                <Button
                  variant="ghost"
                  size="sm"
                  className="px-2 shrink-0"
                  onClick={() => setEditingDetails(false)}
                  title={intl.formatMessage({
                    id: "actions.back",
                    defaultMessage: "Back",
                  })}
                >
                  <ChevronLeft size={18} />
                </Button>
                <h2 className="text-base font-semibold leading-tight truncate min-w-0">
                  {intl.formatMessage(
                    {
                      id: "actions.edit_entity",
                      defaultMessage: "Edit {entityType}",
                    },
                    {
                      entityType: intl
                        .formatMessage({
                          id: "image",
                          defaultMessage: "Image",
                        })
                        .toLocaleLowerCase(),
                    },
                  )}
                </h2>
              </div>
              {/* The form owns its own scroll body + anchored action
                  bar via flex-col layout, so we just give it the
                  remaining height of the parent. */}
              <div className="flex-1 min-h-0">
                <ImageEditForm
                  image={image}
                  onSaved={() => setEditingDetails(false)}
                />
              </div>
            </div>
          }
        />
      ),
    },
    {
      id: "fileinfo",
      label: intl.formatMessage({
        id: "file_info",
        defaultMessage: "File info",
      }),
      shortcut: "i",
      content: <ImageFileInfoTab image={image} />,
    },
  ];

  return (
    <MediaDetailLayout
      title={imageTitle(image) || undefined}
      primaryContent={<ImageViewer image={image} />}
      headerContent={
        <ImageToolbar
          image={image}
          onAddO={() => incrementO()}
          onSubO={() => decrementO()}
          onResetO={() => resetO()}
          onToggleOrganized={handleToggleOrganized}
          onDeleted={goBack}
        />
      }
      tabs={tabs}
      activeTab={activeTab}
      onTabChange={setActiveTab}
      onBack={goBack}
      mobilePageScroll
    />
  );
}

// ── Route ─────────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/images/$imageId")({
  validateSearch: zodValidator(searchSchema),
  component: ImageDetailPage,
});
