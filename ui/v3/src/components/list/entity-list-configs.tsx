/**
 * Shared entity list config hooks.
 *
 * Each hook returns a base EntityListPageConfig (without view/tableVisibilityKey)
 * that can be spread into a useMemo on main list pages or used directly in embedded tabs.
 * tableColumns is included in hooks for entity types that support Table display mode
 * (currently Images and Groups) so embedded tabs also get working table view.
 *
 * For images the hook also returns `lightboxElement` (the paged lightbox JSX), since
 * the lightbox state is managed internally alongside the config.
 */

import type React from "react";
import { useState, useCallback, useRef, useMemo, useEffect } from "react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { imageTitle } from "src/core/files";
import type { EntityListPageConfig, PageNavHandle } from "src/components/list";
import {
  SceneCard,
  ImageCard,
  GalleryCard,
  PerformerCard,
  GroupCard,
  TagCard,
} from "src/components/cards";
import { SceneRowContextMenu } from "src/components/cards/use-scene-context-menu";
import { ImageRowContextMenu } from "src/components/cards/use-image-context-menu";
import { GalleryRowContextMenu } from "src/components/cards/use-gallery-context-menu";
import { GroupRowContextMenu } from "src/components/cards/use-group-context-menu";
import { PerformerRowContextMenu } from "src/components/cards/use-performer-context-menu";
import { TagRowContextMenu } from "src/components/cards/use-tag-context-menu";
import { useSceneTableColumns } from "src/routes/scenes/-table-columns";
import {
  Lightbox,
  type LightboxSlide,
  useSceneLightbox,
} from "src/components/lightbox";
import {
  selectionColumn,
  thumbnailColumn,
  titleColumn,
  textColumn,
  numberColumn,
  ratingColumn,
  tagsColumn,
  studioColumn,
} from "src/components/list/table-columns";

// ── Item types ─────────────────────────────────────────────────────────────────

type SceneItem = GQL.SlimSceneDataFragment;
type ImageItem = GQL.FindImagesQuery["findImages"]["images"][number];
type GalleryItem = GQL.FindGalleriesQuery["findGalleries"]["galleries"][number];
type PerformerItem =
  GQL.FindPerformersQuery["findPerformers"]["performers"][number];
type GroupItem = GQL.FindGroupsQuery["findGroups"]["groups"][number];
type TagItem = GQL.FindTagsQuery["findTags"]["tags"][number];

// ── imageToSlide ───────────────────────────────────────────────────────────────

export function imageToSlide(image: ImageItem): LightboxSlide {
  const src =
    image.paths.image ?? image.paths.preview ?? image.paths.thumbnail ?? "";
  const title = imageTitle(image);
  return { src, alt: title, imageId: image.id, imageTitle: title };
}

// ── useImageLightbox ───────────────────────────────────────────────────────────

const LOADING_SLIDE: LightboxSlide = { src: "", loading: true };

function buildLightboxSlides(
  real: LightboxSlide[],
  hasPrev: boolean,
  hasNext: boolean,
): LightboxSlide[] {
  return [
    ...(hasPrev ? [LOADING_SLIDE] : []),
    ...real,
    ...(hasNext ? [LOADING_SLIDE] : []),
  ];
}

/**
 * Encapsulates the paged lightbox state used by all image list views.
 * Sentinel loading slides are always maintained at boundaries when adjacent
 * pages exist, so both swipe and button presses naturally navigate to them.
 * Landing on a sentinel triggers a page load via the onView callback.
 */
export function useImageLightbox() {
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const [lightboxSlides, setLightboxSlides] = useState<LightboxSlide[]>([]);
  const [lightboxIndex, setLightboxIndex] = useState(0);
  const [hasPrevPage, setHasPrevPage] = useState(false);
  const [hasNextPage, setHasNextPage] = useState(false);
  const pageNavRef = useRef<PageNavHandle | null>(null);
  const pendingPageDirectionRef = useRef<"forward" | "backward" | null>(null);

  // Refs so onLightboxView doesn't need to be recreated on every state change.
  const lightboxSlidesRef = useRef(lightboxSlides);
  const hasPrevPageRef = useRef(hasPrevPage);
  const hasNextPageRef = useRef(hasNextPage);
  useEffect(() => {
    lightboxSlidesRef.current = lightboxSlides;
  }, [lightboxSlides]);
  useEffect(() => {
    hasPrevPageRef.current = hasPrevPage;
  }, [hasPrevPage]);
  useEffect(() => {
    hasNextPageRef.current = hasNextPage;
  }, [hasNextPage]);

  const onCardPreviewClick = useCallback(
    (_item: ImageItem, allItems: ImageItem[], index: number) => {
      const nav = pageNavRef.current;
      const prevPage = nav ? nav.currentPage > 1 : false;
      const nextPage = nav ? nav.currentPage < nav.totalPages : false;
      setHasPrevPage(prevPage);
      setHasNextPage(nextPage);
      const slides = buildLightboxSlides(
        allItems.map(imageToSlide),
        prevPage,
        nextPage,
      );
      setLightboxSlides(slides);
      setLightboxIndex(index + (prevPage ? 1 : 0));
      setLightboxOpen(true);
    },
    [],
  );

  // Called on every YARL view event. Detects landing on a sentinel slide and
  // triggers the adjacent page load.
  const onLightboxView = useCallback((index: number) => {
    const slides = lightboxSlidesRef.current;
    const nav = pageNavRef.current;
    if (!nav || pendingPageDirectionRef.current) return;

    const atEndSentinel =
      hasNextPageRef.current &&
      index === slides.length - 1 &&
      slides[index]?.loading;
    const atStartSentinel =
      hasPrevPageRef.current && index === 0 && slides[0]?.loading;

    if (atEndSentinel) {
      pendingPageDirectionRef.current = "forward";
      setHasNextPage(false);
      nav.nextPage();
    } else if (atStartSentinel) {
      pendingPageDirectionRef.current = "backward";
      setHasPrevPage(false);
      nav.prevPage();
    }
  }, []);

  const onItemsChanged = useCallback((items: ImageItem[]) => {
    const dir = pendingPageDirectionRef.current;
    if (!dir) return;
    pendingPageDirectionRef.current = null;
    const nav = pageNavRef.current;
    const prevPage = nav ? nav.currentPage > 1 : false;
    const nextPage = nav ? nav.currentPage < nav.totalPages : false;
    setHasPrevPage(prevPage);
    setHasNextPage(nextPage);
    const realSlides = items.map(imageToSlide);
    const slides = buildLightboxSlides(realSlides, prevPage, nextPage);
    setLightboxSlides(slides);
    // Land on the first real slide (forward) or last real slide (backward).
    setLightboxIndex(
      dir === "forward"
        ? prevPage
          ? 1
          : 0
        : slides.length - 1 - (nextPage ? 1 : 0),
    );
  }, []);

  const lightboxElement = lightboxOpen ? (
    <Lightbox
      open
      onClose={() => setLightboxOpen(false)}
      slides={lightboxSlides}
      index={lightboxIndex}
      onView={onLightboxView}
      finite={lightboxSlides.some((s) => s.loading)}
    />
  ) : null;

  return {
    onCardPreviewClick,
    onItemsChanged,
    pageNavRef,
    lightboxElement,
    lightboxOpen,
  };
}

// ── useSceneListConfig ─────────────────────────────────────────────────────────

export function useSceneListConfig(
  onEdit: (id: string) => void,
  hidePerformers?: boolean,
): {
  config: EntityListPageConfig<GQL.FindScenesQuery, SceneItem>;
  lightboxElement: React.ReactNode;
  lightboxOpen: boolean;
} {
  const {
    onCardPreviewClick,
    onItemsChanged,
    pageNavRef,
    lightboxElement,
    lightboxOpen,
  } = useSceneLightbox();

  const renderCard = useCallback(
    (
      scene: SceneItem,
      isMobile: boolean,
      selected: boolean,
      onSelectedChanged: (s: boolean, shift: boolean) => void,
      onPreviewClick?: () => void,
    ) => (
      <SceneCard
        key={scene.id}
        scene={scene}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onPreviewClick={onPreviewClick}
        onEdit={() => onEdit(scene.id)}
        hidePerformers={hidePerformers}
      />
    ),
    [onEdit, hidePerformers],
  );

  const renderTableRow = useCallback(
    (
      scene: SceneItem,
      defaultRow: React.ReactElement,
      onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    ) => (
      <SceneRowContextMenu
        scene={scene}
        onEdit={() => onEdit(scene.id)}
        onSelectedChanged={onSelectedChanged}
      >
        {defaultRow}
      </SceneRowContextMenu>
    ),
    [onEdit],
  );

  // Keep tableColumns inside the config hook so embedded scene lists
  // (performer detail, studio detail, tag detail, etc.) get the same Table
  // display mode the global /scenes route does.
  const tableColumns = useSceneTableColumns();

  const config = useMemo<EntityListPageConfig<GQL.FindScenesQuery, SceneItem>>(
    () => ({
      filterMode: GQL.FilterMode.Scenes,
      query: GQL.FindScenesDocument,
      makeVariables: (filter) => ({
        filter: filter.makeFindFilter(),
        scene_filter: filter.makeFilter(),
        scene_filter_ast: filter.makeFilterAST(),
      }),
      extractResult: (data) => ({
        count: data?.findScenes.count ?? 0,
        items: data?.findScenes.scenes ?? [],
      }),
      renderCard,
      renderTableRow,
      tableColumns,
      zoomable: true,
      onCardPreviewClick,
      pageNavRef,
      onItemsChanged,
      getWallDimensions: (scene: SceneItem) => {
        const f = scene.files[0];
        return f?.width && f.height
          ? { width: f.width, height: f.height }
          : { width: 16, height: 9 };
      },
    }),
    [
      renderCard,
      renderTableRow,
      tableColumns,
      onCardPreviewClick,
      onItemsChanged,
      pageNavRef,
    ],
  );

  return { config, lightboxElement, lightboxOpen };
}

// ── useImageListConfig ─────────────────────────────────────────────────────────

type ImageCardExtras = {
  onSetGalleryCover?: () => void;
  onSetPerformerImage?: () => void;
  onSetStudioImage?: () => void;
  onSetTagImage?: () => void;
};

/**
 * Returns an image list config with an integrated paged lightbox.
 *
 * @param onEdit         Called when the user opens the edit sheet for an image.
 * @param getExtraProps  Optional per-image extra card props (e.g. onSetPerformerImage).
 *                       Must be a stable callback (wrap in useCallback at call site).
 */
export function useImageListConfig(
  onEdit: (id: string) => void,
  getExtraProps?: (image: ImageItem) => ImageCardExtras,
  hidePerformers?: boolean,
): {
  config: EntityListPageConfig<GQL.FindImagesQuery, ImageItem>;
  lightboxElement: React.ReactNode;
  lightboxOpen: boolean;
} {
  const intl = useIntl();
  const {
    onCardPreviewClick,
    onItemsChanged,
    pageNavRef,
    lightboxElement,
    lightboxOpen,
  } = useImageLightbox();

  const tableColumns = useMemo(
    () => [
      selectionColumn<ImageItem>(),
      thumbnailColumn<ImageItem>(
        (img) => img.paths.thumbnail,
        (img) => `/images/${img.id}`,
      ),
      titleColumn<ImageItem>({
        id: "title",
        header: intl.formatMessage({ id: "title" }),
        getTitle: imageTitle,
        getHref: (img) => `/images/${img.id}`,
      }),
      textColumn<ImageItem>({
        id: "date",
        header: intl.formatMessage({ id: "date" }),
        getValue: (img) => img.date,
        className:
          "tabular-nums text-muted-foreground text-xs whitespace-nowrap",
      }),
      studioColumn<ImageItem>({
        getStudio: (img) => img.studio,
        header: intl.formatMessage({ id: "studio" }),
      }),
      ratingColumn<ImageItem>({
        getRating: (img) => img.rating100,
        header: intl.formatMessage({ id: "rating" }),
      }),
      tagsColumn<ImageItem>({
        getTags: (img) => img.tags,
        header: intl.formatMessage({ id: "tags" }),
      }),
      textColumn<ImageItem>({
        id: "path",
        header: intl.formatMessage({ id: "path" }),
        getValue: (img) => img.visual_files[0]?.path ?? null,
        className: "text-xs text-muted-foreground font-mono truncate max-w-xs",
      }),
    ],
    [intl],
  );

  const renderCard = useCallback(
    (
      image: ImageItem,
      isMobile: boolean,
      selected: boolean,
      onSelectedChanged: (s: boolean, shift: boolean) => void,
      onPreviewClick?: () => void,
    ) => (
      <ImageCard
        key={image.id}
        image={image}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onPreviewClick={onPreviewClick}
        {...(getExtraProps?.(image) ?? {})}
        onEdit={() => onEdit(image.id)}
        hidePerformers={hidePerformers}
      />
    ),
    [onEdit, getExtraProps, hidePerformers],
  );

  const renderTableRow = useCallback(
    (
      image: ImageItem,
      defaultRow: React.ReactElement,
      onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    ) => (
      <ImageRowContextMenu
        image={image}
        onEdit={() => onEdit(image.id)}
        onSelectedChanged={onSelectedChanged}
      >
        {defaultRow}
      </ImageRowContextMenu>
    ),
    [onEdit],
  );

  const config = useMemo<EntityListPageConfig<GQL.FindImagesQuery, ImageItem>>(
    () => ({
      filterMode: GQL.FilterMode.Images,
      query: GQL.FindImagesDocument,
      makeVariables: (filter) => ({
        filter: filter.makeFindFilter(),
        image_filter: filter.makeFilter(),
        image_filter_ast: filter.makeFilterAST(),
      }),
      extractResult: (data) => ({
        count: data?.findImages.count ?? 0,
        items: data?.findImages.images ?? [],
      }),
      renderCard,
      renderTableRow,
      zoomable: true,
      onCardPreviewClick,
      pageNavRef,
      onItemsChanged,
      tableColumns,
      getWallDimensions: (image: ImageItem) => {
        const f = image.visual_files[0];
        return f?.width && f.height
          ? { width: f.width, height: f.height }
          : { width: 4, height: 3 };
      },
    }),
    [
      renderCard,
      renderTableRow,
      onCardPreviewClick,
      onItemsChanged,
      pageNavRef,
      tableColumns,
    ],
  );

  return { config, lightboxElement, lightboxOpen };
}

// ── useGalleryListConfig ───────────────────────────────────────────────────────

export function useGalleryListConfig(
  onEdit: (id: string) => void,
): EntityListPageConfig<GQL.FindGalleriesQuery, GalleryItem> {
  const renderCard = useCallback(
    (
      gallery: GalleryItem,
      isMobile: boolean,
      selected: boolean,
      onSelectedChanged: (s: boolean, shift: boolean) => void,
    ) => (
      <GalleryCard
        key={gallery.id}
        gallery={gallery}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onEdit={() => onEdit(gallery.id)}
      />
    ),
    [onEdit],
  );

  const renderTableRow = useCallback(
    (
      gallery: GalleryItem,
      defaultRow: React.ReactElement,
      onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    ) => (
      <GalleryRowContextMenu
        gallery={gallery}
        onEdit={() => onEdit(gallery.id)}
        onSelectedChanged={onSelectedChanged}
      >
        {defaultRow}
      </GalleryRowContextMenu>
    ),
    [onEdit],
  );

  return useMemo(
    () => ({
      filterMode: GQL.FilterMode.Galleries,
      query: GQL.FindGalleriesDocument,
      makeVariables: (filter) => ({
        filter: filter.makeFindFilter(),
        gallery_filter: filter.makeFilter(),
        gallery_filter_ast: filter.makeFilterAST(),
      }),
      extractResult: (data) => ({
        count: data?.findGalleries.count ?? 0,
        items: data?.findGalleries.galleries ?? [],
      }),
      renderCard,
      renderTableRow,
      zoomable: true,
      getWallDimensions: () => ({ width: 4, height: 3 }),
    }),
    [renderCard, renderTableRow],
  );
}

// ── usePerformerListConfig ─────────────────────────────────────────────────────

export function usePerformerListConfig(
  onEdit: (id: string) => void,
): EntityListPageConfig<GQL.FindPerformersQuery, PerformerItem> {
  const renderCard = useCallback(
    (
      performer: PerformerItem,
      isMobile: boolean,
      selected: boolean,
      onSelectedChanged: (s: boolean, shift: boolean) => void,
    ) => (
      <PerformerCard
        key={performer.id}
        performer={performer}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onEdit={() => onEdit(performer.id)}
      />
    ),
    [onEdit],
  );

  const renderTableRow = useCallback(
    (
      performer: PerformerItem,
      defaultRow: React.ReactElement,
      onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    ) => (
      <PerformerRowContextMenu
        performer={performer}
        onEdit={() => onEdit(performer.id)}
        onSelectedChanged={onSelectedChanged}
      >
        {defaultRow}
      </PerformerRowContextMenu>
    ),
    [onEdit],
  );

  return useMemo(
    () => ({
      filterMode: GQL.FilterMode.Performers,
      query: GQL.FindPerformersDocument,
      makeVariables: (filter) => ({
        filter: filter.makeFindFilter(),
        performer_filter: filter.makeFilter(),
        performer_filter_ast: filter.makeFilterAST(),
      }),
      extractResult: (data) => ({
        count: data?.findPerformers.count ?? 0,
        items: data?.findPerformers.performers ?? [],
      }),
      renderCard,
      renderTableRow,
      zoomable: true,
      cardIsPortrait: true,
    }),
    [renderCard, renderTableRow],
  );
}

// ── useGroupListConfig ─────────────────────────────────────────────────────────

export function useGroupListConfig(
  onEdit: (id: string) => void,
): EntityListPageConfig<GQL.FindGroupsQuery, GroupItem> {
  const intl = useIntl();

  const tableColumns = useMemo(
    () => [
      selectionColumn<GroupItem>(),
      thumbnailColumn<GroupItem>(
        (g) => g.front_image_path,
        (g) => `/groups/${g.id}`,
      ),
      titleColumn<GroupItem>({
        id: "name",
        header: intl.formatMessage({ id: "name" }),
        getTitle: (g) => g.name,
        getHref: (g) => `/groups/${g.id}`,
      }),
      textColumn<GroupItem>({
        id: "date",
        header: intl.formatMessage({ id: "date" }),
        getValue: (g) => g.date,
        className:
          "tabular-nums text-muted-foreground text-xs whitespace-nowrap",
      }),
      studioColumn<GroupItem>({
        getStudio: (g) => g.studio,
        header: intl.formatMessage({ id: "studio" }),
      }),
      numberColumn<GroupItem>({
        id: "scenes_count",
        header: intl.formatMessage({ id: "scene_count" }),
        getValue: (g) => g.scene_count,
      }),
      numberColumn<GroupItem>({
        id: "sub_group_count",
        header: intl.formatMessage({ id: "sub_group_count" }),
        getValue: (g) => g.sub_group_count,
      }),
      ratingColumn<GroupItem>({
        getRating: (g) => g.rating100,
        header: intl.formatMessage({ id: "rating" }),
      }),
      tagsColumn<GroupItem>({
        getTags: (g) => g.tags,
        header: intl.formatMessage({ id: "tags" }),
      }),
    ],
    [intl],
  );

  const renderCard = useCallback(
    (
      group: GroupItem,
      isMobile: boolean,
      selected: boolean,
      onSelectedChanged: (s: boolean, shift: boolean) => void,
    ) => (
      <GroupCard
        key={group.id}
        group={group}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onEdit={() => onEdit(group.id)}
      />
    ),
    [onEdit],
  );

  const renderTableRow = useCallback(
    (
      group: GroupItem,
      defaultRow: React.ReactElement,
      onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    ) => (
      <GroupRowContextMenu
        group={group}
        onEdit={() => onEdit(group.id)}
        onSelectedChanged={onSelectedChanged}
      >
        {defaultRow}
      </GroupRowContextMenu>
    ),
    [onEdit],
  );

  return useMemo(
    () => ({
      filterMode: GQL.FilterMode.Groups,
      query: GQL.FindGroupsDocument,
      makeVariables: (filter) => ({
        filter: filter.makeFindFilter(),
        group_filter: filter.makeFilter(),
        group_filter_ast: filter.makeFilterAST(),
      }),
      extractResult: (data) => ({
        count: data?.findGroups.count ?? 0,
        items: data?.findGroups.groups ?? [],
      }),
      renderCard,
      renderTableRow,
      zoomable: true,
      tableColumns,
    }),
    [renderCard, renderTableRow, tableColumns],
  );
}

// ── useTagListConfig ───────────────────────────────────────────────────────────

export function useTagListConfig(
  onEdit: (id: string) => void,
): EntityListPageConfig<GQL.FindTagsQuery, TagItem> {
  const renderCard = useCallback(
    (
      tag: TagItem,
      isMobile: boolean,
      selected: boolean,
      onSelectedChanged: (s: boolean, shift: boolean) => void,
    ) => (
      <TagCard
        key={tag.id}
        tag={tag}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onEdit={() => onEdit(tag.id)}
      />
    ),
    [onEdit],
  );

  const renderTableRow = useCallback(
    (
      tag: TagItem,
      defaultRow: React.ReactElement,
      onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    ) => (
      <TagRowContextMenu
        tag={tag}
        onEdit={() => onEdit(tag.id)}
        onSelectedChanged={onSelectedChanged}
      >
        {defaultRow}
      </TagRowContextMenu>
    ),
    [onEdit],
  );

  return useMemo(
    () => ({
      filterMode: GQL.FilterMode.Tags,
      query: GQL.FindTagsDocument,
      makeVariables: (filter) => ({
        filter: filter.makeFindFilter(),
        tag_filter: filter.makeFilter(),
        tag_filter_ast: filter.makeFilterAST(),
      }),
      extractResult: (data) => ({
        count: data?.findTags.count ?? 0,
        items: data?.findTags.tags ?? [],
      }),
      renderCard,
      renderTableRow,
      zoomable: true,
    }),
    [renderCard, renderTableRow],
  );
}
