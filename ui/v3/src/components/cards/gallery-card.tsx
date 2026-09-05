import type React from "react";
import { useState, useCallback, useEffect, useRef } from "react";
import { useApolloClient } from "@apollo/client/react";
import { useNavigate } from "@tanstack/react-router";
import { Search } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { EntityCard } from "./entity-card";
import { Button } from "src/components/ui/button";
import { useCardAspect } from "src/components/list/card-aspect-context";
import { useCardLayout } from "src/components/list/card-layout-context";
import { cn } from "src/lib/utils";
import { galleryLabel } from "src/lib/gallery-utils";
import { HoverScrubber } from "./hover-scrubber";
import { useGalleryContextMenu } from "./use-gallery-context-menu";

type GalleryCardGallery = Pick<
  GQL.SlimGalleryDataFragment,
  | "id"
  | "title"
  | "date"
  | "rating100"
  | "image_count"
  | "organized"
  | "paths"
  | "studio"
  | "tags"
  | "files"
  | "folder"
  | "performers"
>;

interface GalleryCardProps {
  gallery: GalleryCardGallery;
  isMobile?: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
  onPreview?: (index: number) => void;
}

// ── Gallery wall overlay ──────────────────────────────────────────────────────

function formatWallPerformers(
  performers: Array<{ name: string }> | null | undefined,
): string {
  if (!performers || performers.length === 0) return "";
  const names = performers.map((p) => p.name);
  if (names.length === 1) return names[0];
  const head = names.slice(0, -2);
  const tail = names.slice(-2).join(" & ");
  return [...head, tail].join(", ");
}

function GalleryWallOverlay({
  title,
  performers,
  date,
}: {
  title: string;
  performers?: Array<{ name: string }> | null;
  date?: string | null;
}) {
  const performersStr = formatWallPerformers(performers);
  if (!title && !performersStr && !date) return null;
  return (
    <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/80 via-black/40 to-transparent pt-6 pb-1.5 px-2 pointer-events-none">
      {title && (
        <p className="text-white text-[0.7rem] font-semibold leading-tight truncate">
          {title}
        </p>
      )}
      {performersStr && (
        <p
          className={cn(
            "text-white/80 text-[0.65rem] leading-snug",
            title && "mt-0.5",
          )}
        >
          {performersStr}
        </p>
      )}
      {date && (
        <p className="text-white/70 text-[0.6rem] leading-tight mt-0.5">
          {date}
        </p>
      )}
    </div>
  );
}

export const GalleryCard: React.FC<GalleryCardProps> = ({
  gallery,
  isMobile = false,
  selected,
  onSelectedChanged,
  onEdit,
  onPreview,
}) => {
  const cardAspect = useCardAspect();
  const cardLayout = useCardLayout();
  const isPortrait = cardAspect === "portrait";

  // Only commit a scrub thumbnail once it has finished loading. Mid-scrub
  // the new src would otherwise replace the previous src on the visible
  // <img> while still un-decoded, and FadeInImage's opacity-0 would expose
  // the grey bg-muted underneath. Preloading off-DOM and committing on load
  // means the displayed image is always one the browser has decoded — the
  // last successful scrub image stays put until the next one is ready.
  const [scrubImage, setScrubImage] = useState<string | null>(null);
  // Pairs 1:1 with `scrubImage` — committed in lockstep when the preload
  // finishes, so the click target always matches what's on screen.
  const [scrubIndex, setScrubIndex] = useState<number | null>(null);
  const pendingScrubRef = useRef<HTMLImageElement | null>(null);

  const previewBase = !isMobile ? gallery.paths.preview : null;
  const canScrub = !!previewBase && gallery.image_count > 1;

  const handleScrubIndex = useCallback(
    (index: number) => {
      if (!previewBase) return;
      const url = `${previewBase}/${index}`;
      if (url === scrubImage) return;
      const img = new Image();
      pendingScrubRef.current = img;
      const commit = () => {
        if (pendingScrubRef.current === img) {
          setScrubImage(url);
          setScrubIndex(index);
        }
      };
      img.onload = commit;
      img.src = url;
      // Cache hit: <img>.complete is true synchronously after src assign.
      if (img.complete && img.naturalWidth > 0) commit();
    },
    [previewBase, scrubImage],
  );

  // Reset scrub image when no longer relevant
  useEffect(() => {
    if (!canScrub) {
      setScrubImage(null);
      setScrubIndex(null);
      pendingScrubRef.current = null;
    }
  }, [canScrub]);

  const { menuContent, dialogs, onContextMenuOpen } = useGalleryContextMenu({
    gallery,
    onSelectedChanged,
    onEdit,
  });
  const contextMenu = menuContent;

  const preview = (
    <EntityCard.Preview
      image={scrubImage ?? gallery.paths.cover}
      isPortrait={isPortrait}
      organized={gallery.organized}
    >
      {cardLayout === "wall" ? (
        <GalleryWallOverlay
          title={galleryLabel(gallery)}
          performers={gallery.performers}
          date={gallery.date}
        />
      ) : (
        gallery.image_count > 0 && (
          <span className="entity-card-badge entity-card-badge-count">
            {gallery.image_count}
          </span>
        )
      )}
      {onPreview && gallery.image_count > 0 && (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="absolute bottom-1.5 right-1.5 z-20 bg-black/60 text-white/85 hover:bg-black/75 hover:text-white"
          title="Open slideshow"
          aria-label="Open slideshow"
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onPreview(scrubIndex ?? 0);
          }}
        >
          <Search className="size-4" />
        </Button>
      )}
    </EntityCard.Preview>
  );

  const client = useApolloClient();
  const navigate = useNavigate();
  const prefetch = useCallback(() => {
    void client.query({
      query: GQL.FindGalleryDocument,
      variables: { id: gallery.id },
      fetchPolicy: "cache-first",
    });
  }, [client, gallery.id]);

  // While scrubbed, clicking the preview opens the displayed image's
  // detail page. Only attached when an index is committed: otherwise we
  // want the default stretched-anchor behaviour (navigate to the gallery,
  // with a normal pointer cursor — onPreviewClick forces `cursor-zoom-in`).
  // Right-click / middle-click still hit the stretched anchor and open the
  // gallery — overriding that would require resolving the imageId
  // synchronously, which we can't do without a round-trip.
  const handlePreviewClick = useCallback(() => {
    if (scrubIndex == null) return;
    void (async () => {
      try {
        const { data } = await client.query({
          query: GQL.FindGalleryImageIdDocument,
          variables: { id: gallery.id, index: scrubIndex },
          fetchPolicy: "cache-first",
        });
        const imageId = data?.findGallery?.image?.id;
        if (imageId) {
          // Thread `returnTo` through state so the image detail's
          // useSmartBack lands on the parent gallery, not the galleries
          // list — the user never visited the gallery detail, but that's
          // the natural up-context from a scrubbed thumbnail.
          navigate({
            to: "/images/$imageId",
            params: { imageId },
            state: { returnTo: `/galleries/${gallery.id}` },
            viewTransition: true,
          });
          return;
        }
      } catch {
        // Fall through to gallery navigation on failure.
      }
      navigate({
        to: "/galleries/$galleryId",
        params: { galleryId: gallery.id },
        viewTransition: true,
      });
    })();
  }, [client, gallery.id, navigate, scrubIndex]);

  return (
    <>
      <EntityCard
        label={galleryLabel(gallery)}
        id={gallery.id}
        href={`/galleries/${gallery.id}`}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onPreviewClick={scrubIndex != null ? handlePreviewClick : undefined}
        contextMenu={contextMenu}
        onContextMenuOpen={onContextMenuOpen}
        prefetch={prefetch}
        className="gallery-card"
      >
        <EntityCard.SelectCheckbox />
        {canScrub ? (
          <HoverScrubber
            count={gallery.image_count}
            onIndex={handleScrubIndex}
            className="gallery-card-scrubber"
          >
            {preview}
          </HoverScrubber>
        ) : (
          preview
        )}
        <EntityCard.Body>
          <EntityCard.Title>{galleryLabel(gallery)}</EntityCard.Title>
          {(gallery.date || gallery.studio) && (
            <EntityCard.Subtitle noTooltip>
              {[gallery.date, gallery.studio?.name].filter(Boolean).join(" · ")}
            </EntityCard.Subtitle>
          )}
          <EntityCard.Tags tags={gallery.tags} />
          <EntityCard.Rating rating100={gallery.rating100} />
        </EntityCard.Body>
      </EntityCard>
      {dialogs}
    </>
  );
};
