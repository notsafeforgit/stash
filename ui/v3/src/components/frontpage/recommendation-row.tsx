/**
 * RecommendationRow — a horizontally scrollable carousel row for the FrontPage.
 *
 * EntityCarouselRow handles the per-entity-type query and renders cards inside
 * a scroll-snap carousel. SavedFilterCarouselRow loads a saved filter by ID
 * first, then delegates to EntityCarouselRow.
 */

import React, {
  createContext,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "src/lib/utils";
import { ListFilterModel } from "src/models/list-filter/filter";
import type { ICustomFilter, ISavedFilterRow } from "src/core/config";
import {
  SceneCard,
  type SceneCardScene,
  StudioCard,
  PerformerCard,
  GroupCard,
  GalleryCard,
  ImageCard,
  TagCard,
  MarkerCard,
} from "src/components/cards";
import { CardAspectContext } from "src/components/list/card-aspect-context";
import {
  Lightbox,
  SceneLightbox,
  type LightboxSlide,
  type SceneSlide,
} from "src/components/lightbox";
import { imageToSlide } from "src/components/list/entity-list-configs";
import { objectTitle } from "src/core/files";
import { useFrontPageRowState } from "./front-page-state";
import { QueryError } from "@/components/query-error";
import { DeferredMount } from "@/components/shared/deferred-mount";

// ── Carousel-row lightbox helpers ──────────────────────────────────────────────

/**
 * Returns refs + a scroll helper for the per-card snap container. The
 * homepage carousels are single-page (CAROUSEL_PAGE_SIZE items, no
 * paging) so the lightbox is finite — but when the user pages forward /
 * backward inside the lightbox to a card that's currently off-screen in
 * the strip, we need the strip itself to scroll along so closing the
 * lightbox lands them on a visible card.
 */
function useCardScrollRefs(count: number) {
  const refs = useRef<(HTMLDivElement | null)[]>([]);
  useEffect(() => {
    refs.current.length = count;
  }, [count]);
  const setRefAt = (i: number) => (el: HTMLDivElement | null) => {
    refs.current[i] = el;
  };
  const scrollToIndex = (i: number) => {
    const el = refs.current[i];
    if (!el) return;
    // `inline: "nearest"` no-ops when already in view, otherwise aligns
    // the card with the nearest scroll edge. `block: "nearest"` keeps
    // page-level vertical scroll untouched (the homepage scroller would
    // otherwise jump when the lightbox advances).
    el.scrollIntoView({
      behavior: "smooth",
      inline: "nearest",
      block: "nearest",
    });
  };
  return { setRefAt, scrollToIndex };
}

function sceneToCarouselSlide(scene: SceneCardScene): SceneSlide {
  return {
    type: "scene",
    sceneId: scene.id,
    title: objectTitle(scene) || undefined,
    posterSrc: scene.paths.screenshot ?? undefined,
    posterImage: scene.preview_image,
  };
}

function markerTitleStr(m: GQL.SceneMarkerDataFragment): string {
  if (m.title) return m.title;
  const sceneTitle = objectTitle(m.scene);
  return sceneTitle
    ? `${sceneTitle} — ${m.primary_tag.name}`
    : m.primary_tag.name;
}

function markerToCarouselSlide(m: GQL.SceneMarkerDataFragment): SceneSlide {
  // Marker slides carry the scene id (the player streams the parent
  // scene) plus a `marker` payload that scene-slide-content uses to
  // (a) seek the player to `marker.seconds` via `initialTimestamp`,
  // (b) clamp the timeline to the marker's clip range, and
  // (c) show the marker overlay (title / primary tag / tags).
  return {
    type: "scene",
    sceneId: m.scene.id,
    title: markerTitleStr(m),
    posterSrc: m.screenshot ?? undefined,
    posterImage: m.preview_image,
    marker: {
      id: m.id,
      title: markerTitleStr(m),
      seconds: m.seconds,
      primaryTag: { id: m.primary_tag.id, name: m.primary_tag.name },
      tags: m.tags.map((t) => ({ id: t.id, name: t.name })),
    },
  };
}

// ── Carousel shell ─────────────────────────────────────────────────────────────

interface CarouselProps {
  heading: string;
  viewAllHref?: string;
  children: React.ReactNode;
  loading?: boolean;
  mode?: GQL.FilterMode;
}

const CarouselContext = createContext<{
  scrollRef: React.RefObject<HTMLDivElement | null>;
  initialOffset: number;
  viewportWidth: number;
  rem: number;
} | null>(null);

export function RecommendationRow({
  heading,
  viewAllHref,
  children,
  loading,
  mode,
}: CarouselProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const rowState = useFrontPageRowState();
  const kind = mode ? MODE_KINDS[mode] : undefined;
  const carousel = useMemo(
    () => ({
      scrollRef,
      initialOffset: rowState?.scrollLeft ?? 0,
      viewportWidth: window.innerWidth,
      rem: Number.parseFloat(
        getComputedStyle(document.documentElement).fontSize,
      ),
    }),
    [rowState],
  );
  useLayoutEffect(() => {
    if (!loading && scrollRef.current && rowState) {
      scrollRef.current.scrollLeft = rowState.scrollLeft;
    }
  }, [loading, rowState]);
  useEffect(() => {
    const element = scrollRef.current;
    if (loading || !element || !rowState) return;
    const observer = new ResizeObserver(() => {
      rowState.carouselHeight = element.getBoundingClientRect().height;
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [loading, rowState]);

  function scrollBy(delta: number) {
    scrollRef.current?.scrollBy({ left: delta, behavior: "smooth" });
  }

  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center gap-2 px-4">
        <h2 className="text-base font-semibold flex-1 truncate">{heading}</h2>
        {viewAllHref && (
          <Link
            to={viewAllHref}
            className="text-xs text-muted-foreground hover:text-foreground shrink-0"
          >
            View all
          </Link>
        )}
        <Button
          variant="ghost"
          size="icon-sm"
          className="rounded-full shrink-0 text-muted-foreground hover:text-foreground"
          onClick={() => scrollBy(-480)}
          aria-label="Scroll left"
        >
          <ChevronLeft size={18} />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          className="rounded-full shrink-0 text-muted-foreground hover:text-foreground"
          onClick={() => scrollBy(480)}
          aria-label="Scroll right"
        >
          <ChevronRight size={18} />
        </Button>
      </div>

      <div
        ref={scrollRef}
        style={{ minHeight: rowState?.carouselHeight }}
        onScroll={(event) => {
          if (rowState) rowState.scrollLeft = event.currentTarget.scrollLeft;
        }}
        className={cn(
          "flex gap-3 overflow-x-auto px-4 pb-2",
          "[scrollbar-width:none] [&::-webkit-scrollbar]:hidden",
          "scroll-snap-type-x-mandatory [scroll-snap-type:x_mandatory]",
        )}
      >
        {loading ? (
          Array.from({ length: 8 }).map((_, i) => (
            <div
              key={i}
              className={cn(
                "shrink-0 [scroll-snap-align:start]",
                SNAP_WIDTHS[kind ?? "studio"],
              )}
            >
              <Skeleton
                className={kind === "gallery" ? "aspect-video" : "aspect-[2/3]"}
              />
              <div
                className={cn(
                  "flex flex-col gap-2 px-3 py-2.5",
                  BODY_HEIGHTS[kind ?? "studio"],
                )}
              >
                <Skeleton className="h-4 w-4/5" />
                <Skeleton className="h-3 w-2/3" />
              </div>
            </div>
          ))
        ) : (
          <CarouselContext value={carousel}>{children}</CarouselContext>
        )}
      </div>
    </section>
  );
}

// ── Snap-card wrapper (ensures consistent scroll-snap on each card) ────────────

// Per-card-type widths: cards have no intrinsic width and would otherwise
// grow to fit their content (i.e. fill the row). Landscape-preview cards
// (scenes, galleries, markers, images) are wider; portrait/square ones
// (performers, groups, studios, tags) are narrower.
type SnapCardKind =
  | "scene"
  | "gallery"
  | "marker"
  | "image"
  | "studio"
  | "performer"
  | "group"
  | "tag";

const SNAP_WIDTHS: Record<SnapCardKind, string> = {
  scene: "w-64",
  gallery: "w-64",
  marker: "w-64",
  image: "w-56",
  studio: "w-56",
  performer: "w-44",
  group: "w-44",
  tag: "w-40",
};

const SNAP_WIDTH_REM: Record<SnapCardKind, number> = {
  scene: 16,
  gallery: 16,
  marker: 16,
  image: 14,
  studio: 14,
  performer: 11,
  group: 11,
  tag: 10,
};

// Skeletons reserve the same cover width/aspect and typical metadata space as
// their cards. A generic portrait rectangle made the next row jump by ~170px.
const BODY_HEIGHTS: Record<SnapCardKind, string> = {
  scene: "h-30",
  gallery: "h-24",
  marker: "h-24",
  image: "h-24",
  studio: "h-12",
  performer: "h-24",
  group: "h-20",
  tag: "h-16",
};
const MODE_KINDS: Partial<Record<GQL.FilterMode, SnapCardKind>> = {
  [GQL.FilterMode.Scenes]: "scene",
  [GQL.FilterMode.Galleries]: "gallery",
  [GQL.FilterMode.SceneMarkers]: "marker",
  [GQL.FilterMode.Images]: "image",
  [GQL.FilterMode.Studios]: "studio",
  [GQL.FilterMode.Performers]: "performer",
  [GQL.FilterMode.Groups]: "group",
  [GQL.FilterMode.Tags]: "tag",
};

const SnapCard = React.forwardRef<
  HTMLDivElement,
  {
    kind: SnapCardKind;
    index: number;
    children: React.ReactNode;
  }
>(function SnapCard({ kind, index, children }, ref) {
  const carousel = useContext(CarouselContext);
  // Keep all native snap targets and the strip's width, but only build cards
  // near the visible range. Returning to Home eagerly restores that range;
  // it doesn't synchronously rebuild every card in every visited row.
  const step = (SNAP_WIDTH_REM[kind] + 0.75) * (carousel?.rem ?? 16);
  const start = index * step;
  const eager =
    !carousel ||
    (start + step >= carousel.initialOffset - step &&
      start <= carousel.initialOffset + carousel.viewportWidth + step);
  return (
    <div
      ref={ref}
      className={cn("shrink-0 [scroll-snap-align:start]", SNAP_WIDTHS[kind])}
    >
      <DeferredMount
        releaseDistantMedia
        eager={eager}
        scrollRoot={carousel?.scrollRef}
        rootMargin="0px 320px"
        className="h-full"
        fallback={null}
      >
        {children}
      </DeferredMount>
    </div>
  );
});

// ── Per-entity carousel rows ───────────────────────────────────────────────────

const CAROUSEL_PAGE_SIZE = 25;

function buildFilter(
  mode: GQL.FilterMode,
  sortBy: string,
  direction: GQL.SortDirectionEnum,
): ListFilterModel {
  const f = new ListFilterModel(mode, undefined, {
    defaultSortBy: sortBy,
    defaultSortDir: direction,
  });
  f.itemsPerPage = CAROUSEL_PAGE_SIZE;
  return f;
}

function useCarouselFilter(
  mode: GQL.FilterMode,
  sortBy: string,
  direction: GQL.SortDirectionEnum,
  filterProp?: ListFilterModel,
) {
  const rowState = useFrontPageRowState();
  useLayoutEffect(() => {
    if (rowState) rowState.mounted = true;
  }, [rowState]);
  return useMemo(() => {
    const filter = filterProp?.clone() ?? buildFilter(mode, sortBy, direction);
    if (rowState && filter.sortBy === "random")
      filter.randomSeed = rowState.randomSeed;
    return filter;
  }, [mode, sortBy, direction, filterProp, rowState]);
}

// ── Scenes ─────────────────────────────────────────────────────────────────────

interface SceneRowProps {
  heading: string;
  sortBy: string;
  direction: GQL.SortDirectionEnum;
  filter?: ListFilterModel;
}

export function SceneCarouselRow({
  heading,
  sortBy,
  direction,
  filter: filterProp,
}: SceneRowProps) {
  const filter = useCarouselFilter(
    GQL.FilterMode.Scenes,
    sortBy,
    direction,
    filterProp,
  );
  const { data, loading } = useQuery(GQL.FindScenesDocument, {
    variables: {
      filter: filter.makeFindFilter(),
      scene_filter_ast: filter.makeFilterAST(),
    },
  });

  // Stable reference for the array — the `?? []` fallback would
  // otherwise produce a fresh empty array on every loading render and
  // invalidate downstream memoised slides.
  const scenes = useMemo<SceneCardScene[]>(
    () => (data?.findScenes.scenes ?? []) as SceneCardScene[],
    [data],
  );

  // Lightbox over the row only — the carousel is single-page (no
  // sentinels / page nav), so the user can only flick between the
  // currently-loaded items in this row. If they advance to a card that
  // is off-screen in the strip, scroll the strip so it tracks the
  // lightbox.
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const { setRefAt, scrollToIndex } = useCardScrollRefs(scenes.length);
  const slides = useMemo(() => scenes.map(sceneToCarouselSlide), [scenes]);

  return (
    <RecommendationRow
      heading={heading}
      viewAllHref="/scenes"
      mode={GQL.FilterMode.Scenes}
      loading={loading && !data}
    >
      {scenes.map((scene, i) => (
        <SnapCard key={scene.id} kind="scene" index={i} ref={setRefAt(i)}>
          <SceneCard
            scene={scene}
            onPreviewClick={() => {
              setIndex(i);
              setOpen(true);
            }}
          />
        </SnapCard>
      ))}
      {open && (
        <SceneLightbox
          open
          onClose={() => setOpen(false)}
          slides={slides}
          index={index}
          onView={(i) => {
            setIndex(i);
            scrollToIndex(i);
          }}
          finite
        />
      )}
    </RecommendationRow>
  );
}

// ── Studios ────────────────────────────────────────────────────────────────────

interface StudioRowProps {
  heading: string;
  sortBy: string;
  direction: GQL.SortDirectionEnum;
  filter?: ListFilterModel;
}

export function StudioCarouselRow({
  heading,
  sortBy,
  direction,
  filter: filterProp,
}: StudioRowProps) {
  const filter = useCarouselFilter(
    GQL.FilterMode.Studios,
    sortBy,
    direction,
    filterProp,
  );
  const { data, loading } = useQuery(GQL.FindStudiosDocument, {
    variables: {
      filter: filter.makeFindFilter(),
      studio_filter_ast: filter.makeFilterAST(),
    },
  });

  const studios = data?.findStudios.studios ?? [];

  return (
    <RecommendationRow
      heading={heading}
      viewAllHref="/studios"
      mode={GQL.FilterMode.Studios}
      loading={loading && !data}
    >
      {studios.map((studio, i) => (
        <SnapCard key={studio.id} kind="studio" index={i}>
          <StudioCard studio={studio} />
        </SnapCard>
      ))}
    </RecommendationRow>
  );
}

// ── Performers ─────────────────────────────────────────────────────────────────

interface PerformerRowProps {
  heading: string;
  sortBy: string;
  direction: GQL.SortDirectionEnum;
  filter?: ListFilterModel;
}

export function PerformerCarouselRow({
  heading,
  sortBy,
  direction,
  filter: filterProp,
}: PerformerRowProps) {
  const filter = useCarouselFilter(
    GQL.FilterMode.Performers,
    sortBy,
    direction,
    filterProp,
  );
  const { data, loading } = useQuery(GQL.FindPerformersDocument, {
    variables: {
      filter: filter.makeFindFilter(),
      performer_filter_ast: filter.makeFilterAST(),
    },
  });

  const performers = data?.findPerformers.performers ?? [];

  return (
    <RecommendationRow
      heading={heading}
      viewAllHref="/performers"
      mode={GQL.FilterMode.Performers}
      loading={loading && !data}
    >
      {performers.map((performer, i) => (
        <SnapCard key={performer.id} kind="performer" index={i}>
          <PerformerCard performer={performer} />
        </SnapCard>
      ))}
    </RecommendationRow>
  );
}

// ── Groups ─────────────────────────────────────────────────────────────────────

interface GroupRowProps {
  heading: string;
  sortBy: string;
  direction: GQL.SortDirectionEnum;
  filter?: ListFilterModel;
}

export function GroupCarouselRow({
  heading,
  sortBy,
  direction,
  filter: filterProp,
}: GroupRowProps) {
  const filter = useCarouselFilter(
    GQL.FilterMode.Groups,
    sortBy,
    direction,
    filterProp,
  );
  const { data, loading } = useQuery(GQL.FindGroupsDocument, {
    variables: {
      filter: filter.makeFindFilter(),
      group_filter_ast: filter.makeFilterAST(),
    },
  });

  const groups = data?.findGroups.groups ?? [];

  return (
    <RecommendationRow
      heading={heading}
      viewAllHref="/groups"
      mode={GQL.FilterMode.Groups}
      loading={loading && !data}
    >
      {groups.map((group, i) => (
        <SnapCard key={group.id} kind="group" index={i}>
          <GroupCard group={group} />
        </SnapCard>
      ))}
    </RecommendationRow>
  );
}

// ── Galleries ──────────────────────────────────────────────────────────────────

interface GalleryRowProps {
  heading: string;
  sortBy: string;
  direction: GQL.SortDirectionEnum;
  filter?: ListFilterModel;
}

export function GalleryCarouselRow({
  heading,
  sortBy,
  direction,
  filter: filterProp,
}: GalleryRowProps) {
  const filter = useCarouselFilter(
    GQL.FilterMode.Galleries,
    sortBy,
    direction,
    filterProp,
  );
  const { data, loading } = useQuery(GQL.FindGalleriesDocument, {
    variables: {
      filter: filter.makeFindFilter(),
      gallery_filter_ast: filter.makeFilterAST(),
    },
  });

  const galleries = data?.findGalleries.galleries ?? [];

  // Reset the homepage's portrait override back to `auto` for galleries.
  // Gallery covers are inherently landscape page-spreads — pillarboxing
  // them into a portrait frame wastes the row's vertical space and reads
  // worse than the natural layout. The frontpage wraps everything in a
  // portrait Provider (see `routes/index.tsx`); this row opts out.
  return (
    <CardAspectContext.Provider value="auto">
      <RecommendationRow
        heading={heading}
        viewAllHref="/galleries"
        mode={GQL.FilterMode.Galleries}
        loading={loading && !data}
      >
        {galleries.map((gallery, i) => (
          <SnapCard key={gallery.id} kind="gallery" index={i}>
            <GalleryCard gallery={gallery} />
          </SnapCard>
        ))}
      </RecommendationRow>
    </CardAspectContext.Provider>
  );
}

// ── Images ─────────────────────────────────────────────────────────────────────

interface ImageRowProps {
  heading: string;
  sortBy: string;
  direction: GQL.SortDirectionEnum;
  filter?: ListFilterModel;
}

export function ImageCarouselRow({
  heading,
  sortBy,
  direction,
  filter: filterProp,
}: ImageRowProps) {
  const filter = useCarouselFilter(
    GQL.FilterMode.Images,
    sortBy,
    direction,
    filterProp,
  );
  const { data, loading } = useQuery(GQL.FindImagesDocument, {
    variables: {
      filter: filter.makeFindFilter(),
      image_filter_ast: filter.makeFilterAST(),
    },
  });

  const images = useMemo(() => data?.findImages.images ?? [], [data]);

  // Same single-row-only lightbox as scenes — see SceneCarouselRow.
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const { setRefAt, scrollToIndex } = useCardScrollRefs(images.length);
  const slides = useMemo<LightboxSlide[]>(
    () => images.map(imageToSlide),
    [images],
  );

  return (
    <RecommendationRow
      heading={heading}
      viewAllHref="/images"
      mode={GQL.FilterMode.Images}
      loading={loading && !data}
    >
      {images.map((image, i) => (
        <SnapCard key={image.id} kind="image" index={i} ref={setRefAt(i)}>
          <ImageCard
            image={image}
            onPreviewClick={() => {
              setIndex(i);
              setOpen(true);
            }}
          />
        </SnapCard>
      ))}
      {open && (
        <Lightbox
          open
          onClose={() => setOpen(false)}
          slides={slides}
          index={index}
          onView={(i) => {
            setIndex(i);
            scrollToIndex(i);
          }}
          finite
        />
      )}
    </RecommendationRow>
  );
}

// ── Tags ───────────────────────────────────────────────────────────────────────

interface TagRowProps {
  heading: string;
  sortBy: string;
  direction: GQL.SortDirectionEnum;
  filter?: ListFilterModel;
}

export function TagCarouselRow({
  heading,
  sortBy,
  direction,
  filter: filterProp,
}: TagRowProps) {
  const filter = useCarouselFilter(
    GQL.FilterMode.Tags,
    sortBy,
    direction,
    filterProp,
  );
  const { data, loading } = useQuery(GQL.FindTagsDocument, {
    variables: {
      filter: filter.makeFindFilter(),
      tag_filter_ast: filter.makeFilterAST(),
    },
  });

  const tags = data?.findTags.tags ?? [];

  return (
    <RecommendationRow
      heading={heading}
      viewAllHref="/tags"
      mode={GQL.FilterMode.Tags}
      loading={loading && !data}
    >
      {tags.map((tag, i) => (
        <SnapCard key={tag.id} kind="tag" index={i}>
          <TagCard tag={tag} />
        </SnapCard>
      ))}
    </RecommendationRow>
  );
}

// ── Scene Markers ──────────────────────────────────────────────────────────────

interface MarkerRowProps {
  heading: string;
  sortBy: string;
  direction: GQL.SortDirectionEnum;
  filter?: ListFilterModel;
}

export function MarkerCarouselRow({
  heading,
  sortBy,
  direction,
  filter: filterProp,
}: MarkerRowProps) {
  const filter = useCarouselFilter(
    GQL.FilterMode.SceneMarkers,
    sortBy,
    direction,
    filterProp,
  );
  const { data, loading } = useQuery(GQL.FindSceneMarkersDocument, {
    variables: {
      filter: filter.makeFindFilter(),
      scene_marker_filter_ast: filter.makeFilterAST(),
    },
  });

  const markers = useMemo(
    () => data?.findSceneMarkers.scene_markers ?? [],
    [data],
  );

  // Marker carousel opens the SceneLightbox in marker mode — same
  // single-row scope as the scene / image carousels. The lightbox
  // streams each marker's parent scene with `initialTimestamp` =
  // marker.seconds and a `clipRange` so the timeline is bounded to
  // the marker's clip.
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const { setRefAt, scrollToIndex } = useCardScrollRefs(markers.length);
  const slides = useMemo(() => markers.map(markerToCarouselSlide), [markers]);

  return (
    <RecommendationRow
      heading={heading}
      viewAllHref="/scenes/markers"
      mode={GQL.FilterMode.SceneMarkers}
      loading={loading && !data}
    >
      {markers.map((marker, i) => (
        <SnapCard key={marker.id} kind="marker" index={i} ref={setRefAt(i)}>
          <MarkerCard
            marker={marker}
            onPreviewClick={() => {
              setIndex(i);
              setOpen(true);
            }}
          />
        </SnapCard>
      ))}
      {open && (
        <SceneLightbox
          open
          onClose={() => setOpen(false)}
          slides={slides}
          index={index}
          onView={(i) => {
            setIndex(i);
            scrollToIndex(i);
          }}
          finite
        />
      )}
    </RecommendationRow>
  );
}

// ── CustomFilter dispatch ──────────────────────────────────────────────────────

interface CustomRowProps {
  heading: string;
  content: ICustomFilter;
}

export function CustomFilterCarouselRow({ heading, content }: CustomRowProps) {
  const { mode, sortBy, direction } = content;

  switch (mode) {
    case GQL.FilterMode.Scenes:
      return (
        <SceneCarouselRow
          heading={heading}
          sortBy={sortBy}
          direction={direction}
        />
      );
    case GQL.FilterMode.Studios:
      return (
        <StudioCarouselRow
          heading={heading}
          sortBy={sortBy}
          direction={direction}
        />
      );
    case GQL.FilterMode.Performers:
      return (
        <PerformerCarouselRow
          heading={heading}
          sortBy={sortBy}
          direction={direction}
        />
      );
    case GQL.FilterMode.Groups:
      return (
        <GroupCarouselRow
          heading={heading}
          sortBy={sortBy}
          direction={direction}
        />
      );
    case GQL.FilterMode.Galleries:
      return (
        <GalleryCarouselRow
          heading={heading}
          sortBy={sortBy}
          direction={direction}
        />
      );
    case GQL.FilterMode.Images:
      return (
        <ImageCarouselRow
          heading={heading}
          sortBy={sortBy}
          direction={direction}
        />
      );
    case GQL.FilterMode.Tags:
      return (
        <TagCarouselRow
          heading={heading}
          sortBy={sortBy}
          direction={direction}
        />
      );
    case GQL.FilterMode.SceneMarkers:
      return (
        <MarkerCarouselRow
          heading={heading}
          sortBy={sortBy}
          direction={direction}
        />
      );
    default:
      return null;
  }
}

// ── SavedFilter row ────────────────────────────────────────────────────────────

interface SavedFilterRowProps {
  content: ISavedFilterRow;
  placeholderOnly?: boolean;
}

function savedFilterToCarouselRow(
  savedFilter: GQL.SavedFilterDataFragment,
): React.ReactElement | null {
  const heading = savedFilter.name;
  const f = new ListFilterModel(savedFilter.mode);
  f.configureFromSavedFilter(savedFilter);
  f.itemsPerPage = CAROUSEL_PAGE_SIZE;
  // Ignore the saved order. Home supplies a visit seed so Back reuses the
  // same cached cards; a browser reload or configuration change reshuffles.
  if (f.sortBy === "random") {
    f.randomSeed = -1;
  }

  switch (savedFilter.mode) {
    case GQL.FilterMode.Scenes:
      return (
        <SceneCarouselRow
          heading={heading}
          sortBy={f.sortBy ?? "date"}
          direction={f.sortDirection}
          filter={f}
        />
      );
    case GQL.FilterMode.Studios:
      return (
        <StudioCarouselRow
          heading={heading}
          sortBy={f.sortBy ?? "name"}
          direction={f.sortDirection}
          filter={f}
        />
      );
    case GQL.FilterMode.Performers:
      return (
        <PerformerCarouselRow
          heading={heading}
          sortBy={f.sortBy ?? "name"}
          direction={f.sortDirection}
          filter={f}
        />
      );
    case GQL.FilterMode.Groups:
      return (
        <GroupCarouselRow
          heading={heading}
          sortBy={f.sortBy ?? "name"}
          direction={f.sortDirection}
          filter={f}
        />
      );
    case GQL.FilterMode.Galleries:
      return (
        <GalleryCarouselRow
          heading={heading}
          sortBy={f.sortBy ?? "date"}
          direction={f.sortDirection}
          filter={f}
        />
      );
    case GQL.FilterMode.Images:
      return (
        <ImageCarouselRow
          heading={heading}
          sortBy={f.sortBy ?? "date"}
          direction={f.sortDirection}
          filter={f}
        />
      );
    case GQL.FilterMode.Tags:
      return (
        <TagCarouselRow
          heading={heading}
          sortBy={f.sortBy ?? "name"}
          direction={f.sortDirection}
          filter={f}
        />
      );
    case GQL.FilterMode.SceneMarkers:
      return (
        <MarkerCarouselRow
          heading={heading}
          sortBy={f.sortBy ?? "created_at"}
          direction={f.sortDirection}
          filter={f}
        />
      );
    default:
      return null;
  }
}

export function SavedFilterCarouselRow({
  content,
  placeholderOnly,
}: SavedFilterRowProps) {
  const intl = useIntl();
  const { data, loading, error, refetch } = useQuery<
    GQL.FindSavedFilterQuery,
    GQL.FindSavedFilterQueryVariables
  >(GQL.FindSavedFilterDocument, {
    variables: { id: String(content.savedFilterId) },
  });

  const savedFilter = data?.findSavedFilter;

  const row = useMemo(
    () => (savedFilter ? savedFilterToCarouselRow(savedFilter) : null),
    [savedFilter],
  );

  if (error && !savedFilter)
    return <QueryError error={error} retry={refetch} retrying={loading} />;
  if (placeholderOnly && savedFilter)
    return (
      <RecommendationRow
        heading={savedFilter.name}
        mode={savedFilter.mode}
        loading
      >
        {null}
      </RecommendationRow>
    );

  if (loading && !savedFilter) {
    return (
      <RecommendationRow
        heading={intl.formatMessage({
          id: "loading.generic",
          defaultMessage: "Loading…",
        })}
        loading
      >
        {null}
      </RecommendationRow>
    );
  }

  return row;
}
