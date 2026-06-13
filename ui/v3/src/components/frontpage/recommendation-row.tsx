/**
 * RecommendationRow — a horizontally scrollable carousel row for the FrontPage.
 *
 * EntityCarouselRow handles the per-entity-type query and renders cards inside
 * a scroll-snap carousel. SavedFilterCarouselRow loads a saved filter by ID
 * first, then delegates to EntityCarouselRow.
 */

import React, { useMemo, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
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
  // Trim if items shrink, so stale refs to removed cards don't leak.
  if (refs.current.length > count) refs.current.length = count;
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
}

export function RecommendationRow({
  heading,
  viewAllHref,
  children,
  loading,
}: CarouselProps) {
  const scrollRef = useRef<HTMLDivElement>(null);

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
        className={cn(
          "flex gap-3 overflow-x-auto px-4 pb-2",
          "[scrollbar-width:none] [&::-webkit-scrollbar]:hidden",
          "scroll-snap-type-x-mandatory [scroll-snap-type:x_mandatory]",
        )}
      >
        {loading
          ? Array.from({ length: 8 }).map((_, i) => (
              <div
                key={i}
                className="shrink-0 w-56 h-40 rounded-md bg-muted animate-pulse [scroll-snap-align:start]"
              />
            ))
          : children}
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

const SnapCard = React.forwardRef<
  HTMLDivElement,
  {
    kind: SnapCardKind;
    children: React.ReactNode;
  }
>(function SnapCard({ kind, children }, ref) {
  return (
    <div
      ref={ref}
      className={cn("shrink-0 [scroll-snap-align:start]", SNAP_WIDTHS[kind])}
    >
      {children}
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
  // Memoise so the filter object is stable across re-renders. Without
  // this, every render of this component would either rebuild via
  // `buildFilter` (resetting `randomSeed` to -1) or accept a fresh
  // `filterProp` from a non-memoised parent — both cases force a new
  // seed each render and refetch the carousel needlessly. With this,
  // the seed is generated on the first read and reused for the lifetime
  // of the mount; the carousel reshuffles only when the component is
  // re-mounted (page revisit) or the inputs actually change.
  const filter = useMemo(
    () => filterProp ?? buildFilter(GQL.FilterMode.Scenes, sortBy, direction),
    [filterProp, sortBy, direction],
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
      loading={loading && !data}
    >
      {scenes.map((scene, i) => (
        <SnapCard key={scene.id} kind="scene" ref={setRefAt(i)}>
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
  const filter = useMemo(
    () => filterProp ?? buildFilter(GQL.FilterMode.Studios, sortBy, direction),
    [filterProp, sortBy, direction],
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
      loading={loading && !data}
    >
      {studios.map((studio) => (
        <SnapCard key={studio.id} kind="studio">
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
  const filter = useMemo(
    () =>
      filterProp ?? buildFilter(GQL.FilterMode.Performers, sortBy, direction),
    [filterProp, sortBy, direction],
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
      loading={loading && !data}
    >
      {performers.map((performer) => (
        <SnapCard key={performer.id} kind="performer">
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
  const filter = useMemo(
    () => filterProp ?? buildFilter(GQL.FilterMode.Groups, sortBy, direction),
    [filterProp, sortBy, direction],
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
      loading={loading && !data}
    >
      {groups.map((group) => (
        <SnapCard key={group.id} kind="group">
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
  const filter = useMemo(
    () =>
      filterProp ?? buildFilter(GQL.FilterMode.Galleries, sortBy, direction),
    [filterProp, sortBy, direction],
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
        loading={loading && !data}
      >
        {galleries.map((gallery) => (
          <SnapCard key={gallery.id} kind="gallery">
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
  const filter = useMemo(
    () => filterProp ?? buildFilter(GQL.FilterMode.Images, sortBy, direction),
    [filterProp, sortBy, direction],
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
      loading={loading && !data}
    >
      {images.map((image, i) => (
        <SnapCard key={image.id} kind="image" ref={setRefAt(i)}>
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
  const filter = useMemo(
    () => filterProp ?? buildFilter(GQL.FilterMode.Tags, sortBy, direction),
    [filterProp, sortBy, direction],
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
      loading={loading && !data}
    >
      {tags.map((tag) => (
        <SnapCard key={tag.id} kind="tag">
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
  const filter = useMemo(
    () =>
      filterProp ?? buildFilter(GQL.FilterMode.SceneMarkers, sortBy, direction),
    [filterProp, sortBy, direction],
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
      loading={loading && !data}
    >
      {markers.map((marker, i) => (
        <SnapCard key={marker.id} kind="marker" ref={setRefAt(i)}>
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
}

function savedFilterToCarouselRow(
  savedFilter: GQL.SavedFilterDataFragment,
): React.ReactElement | null {
  const heading = savedFilter.name;
  const f = new ListFilterModel(savedFilter.mode);
  f.configureFromSavedFilter(savedFilter);
  f.itemsPerPage = CAROUSEL_PAGE_SIZE;
  // Carousels reshuffle on every page load: a saved filter's persisted
  // random seed represents the order at save-time, but homepage rows
  // aren't URL-bookmarkable and the user expectation is "new content
  // each visit." Clear the seed so the first read inside the carousel
  // generates a fresh one. (List views, which DO want refresh-stable
  // ordering, parse the seed directly from the URL.)
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

export function SavedFilterCarouselRow({ content }: SavedFilterRowProps) {
  const intl = useIntl();
  const { data, loading } = useQuery<
    GQL.FindSavedFilterQuery,
    GQL.FindSavedFilterQueryVariables
  >(GQL.FindSavedFilterDocument, {
    variables: { id: String(content.savedFilterId) },
  });

  const savedFilter = data?.findSavedFilter;

  // Memoise the constructed row (and its embedded `ListFilterModel`)
  // per saved-filter reference. Without this, every parent re-render
  // would rebuild the filter via `savedFilterToCarouselRow` — and
  // because we deliberately clear `randomSeed` in that function (so
  // page-load reshuffles), each rebuild would also generate a new
  // seed and cause the carousel to refetch. The memo keeps the seed
  // stable for the lifetime of this mount, so the carousel reshuffles
  // exactly once (on mount / page revisit), not on every parent tick.
  const row = useMemo(
    () => (savedFilter ? savedFilterToCarouselRow(savedFilter) : null),
    [savedFilter],
  );

  if (loading) {
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
