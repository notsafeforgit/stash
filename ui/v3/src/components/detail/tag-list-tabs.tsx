/**
 * Tag detail embedded list tab panels.
 *
 * Each panel renders an EntityListPage scoped to the current tag,
 * with URL sync disabled so the filter state doesn't contaminate the URL.
 */

import { useMemo, useState, useCallback } from "react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import {
  FilterMode,
  FilterGroupOperator,
  CriterionModifier,
} from "src/core/generated-graphql";
import { EntityListPage, type EntityListPageConfig } from "src/components/list";
import { View } from "src/components/list/views";
import {
  useSceneListConfig,
  useImageListConfig,
  useGalleryListConfig,
  usePerformerListConfig,
  useGroupListConfig,
} from "src/components/list/entity-list-configs";
import { StudioCard, MarkerCard } from "src/components/cards";
import { useMarkerLightbox } from "src/components/lightbox";
import { useMutation } from "@apollo/client/react";
import { ListFilterModel } from "src/models/list-filter/filter";
import {
  TagsCriterion,
  TagsCriterionOption,
} from "src/models/list-filter/criteria/tags";
import {
  createASTConditionFromCriterion,
  createASTGroup,
} from "src/models/list-filter/filter-ast";
import { useConfigurationContextOptional } from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { SceneEditSheet } from "./scene-edit-sheet";
import { ImageEditSheet } from "./image-edit-sheet";
import { GalleryEditSheet } from "./gallery-edit-sheet";
import { PerformerEditSheet } from "./performer-edit-sheet";
import { GroupEditSheet } from "./group-edit-sheet";
import { StudioEditSheet } from "./studio-edit-sheet";

// ── Helpers ────────────────────────────────────────────────────────────────────

type TagData = NonNullable<GQL.FindTagQuery["findTag"]>;
type ImageItem = GQL.FindImagesQuery["findImages"]["images"][number];
type StudioItem = GQL.FindStudiosQuery["findStudios"]["studios"][number];
type MarkerItem =
  GQL.FindSceneMarkersQuery["findSceneMarkers"]["scene_markers"][number];

function makeTagFilter(
  mode: FilterMode,
  tagId: string,
  tagName: string,
  config: GQL.ConfigDataFragment | undefined,
): ListFilterModel {
  const filter = new ListFilterModel(mode, config);

  const criterion = new TagsCriterion(TagsCriterionOption);
  criterion.modifier = CriterionModifier.IncludesAll;
  criterion.value = {
    items: [{ id: tagId, label: tagName }],
    excluded: [],
    depth: 0,
  };

  const conditionNode = createASTConditionFromCriterion(mode, criterion);
  filter.lockedFilterAst = createASTGroup(mode, FilterGroupOperator.And, [
    conditionNode,
  ]);

  return filter;
}

// ── Scenes tab ─────────────────────────────────────────────────────────────────

export function TagScenesTab({ tag }: { tag: TagData }) {
  const ctx = useConfigurationContextOptional();
  const { id: tagId, name: tagName } = tag;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () => makeTagFilter(FilterMode.Scenes, tagId, tagName, gqlConfig),
    [tagId, tagName, gqlConfig],
  );
  const { config, lightboxElement, lightboxOpen } =
    useSceneListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={tagId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.TagScenes}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <SceneEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

// ── Images tab ─────────────────────────────────────────────────────────────────

export function TagImagesTab({ tag }: { tag: TagData }) {
  const ctx = useConfigurationContextOptional();
  const { id: tagId, name: tagName } = tag;
  const gqlConfig = ctx?.configuration;
  const intl = useIntl();
  const toast = useToast();
  const defaultFilter = useMemo(
    () => makeTagFilter(FilterMode.Images, tagId, tagName, gqlConfig),
    [tagId, tagName, gqlConfig],
  );
  const [updateTag] = useMutation(GQL.TagUpdateDocument);
  const [editingId, setEditingId] = useState<string | null>(null);

  const getExtraCardProps = useCallback(
    (image: ImageItem) => ({
      onSetTagImage: async () => {
        try {
          await updateTag({
            variables: {
              input: { id: tag.id, image_input: { image_id: image.id } },
            },
          });
          toast.success(
            intl.formatMessage({
              id: "toast.tag_image_set",
              defaultMessage: "Tag image updated.",
            }),
          );
        } catch (e) {
          toast.error(e);
        }
      },
    }),
    [updateTag, tag.id, toast, intl],
  );

  const { config, lightboxElement, lightboxOpen } = useImageListConfig(
    setEditingId,
    getExtraCardProps,
  );

  return (
    <>
      <EntityListPage
        key={tagId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.TagImages}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <ImageEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

// ── Galleries tab ──────────────────────────────────────────────────────────────

export function TagGalleriesTab({ tag }: { tag: TagData }) {
  const ctx = useConfigurationContextOptional();
  const { id: tagId, name: tagName } = tag;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () => makeTagFilter(FilterMode.Galleries, tagId, tagName, gqlConfig),
    [tagId, tagName, gqlConfig],
  );
  const { config, lightboxElement, lightboxOpen } =
    useGalleryListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={tagId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.TagGalleries}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <GalleryEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

// ── Performers tab ─────────────────────────────────────────────────────────────

export function TagPerformersTab({ tag }: { tag: TagData }) {
  const ctx = useConfigurationContextOptional();
  const { id: tagId, name: tagName } = tag;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () => makeTagFilter(FilterMode.Performers, tagId, tagName, gqlConfig),
    [tagId, tagName, gqlConfig],
  );
  const config = usePerformerListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={tagId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.TagPerformers}
        mobileChromeFixed
      />
      <PerformerEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}

// ── Groups tab ─────────────────────────────────────────────────────────────────

export function TagGroupsTab({ tag }: { tag: TagData }) {
  const ctx = useConfigurationContextOptional();
  const { id: tagId, name: tagName } = tag;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () => makeTagFilter(FilterMode.Groups, tagId, tagName, gqlConfig),
    [tagId, tagName, gqlConfig],
  );
  const config = useGroupListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={tagId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.TagGroups}
        mobileChromeFixed
      />
      <GroupEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}

// ── Studios tab ────────────────────────────────────────────────────────────────

export function TagStudiosTab({ tag }: { tag: TagData }) {
  const ctx = useConfigurationContextOptional();
  const { id: tagId, name: tagName } = tag;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () => makeTagFilter(FilterMode.Studios, tagId, tagName, gqlConfig),
    [tagId, tagName, gqlConfig],
  );

  const config = useMemo<
    EntityListPageConfig<
      GQL.FindStudiosQuery,
      StudioItem,
      GQL.FindStudiosQueryVariables
    >
  >(
    () => ({
      filterMode: GQL.FilterMode.Studios,
      source: {
        kind: "graphql",
        query: GQL.FindStudiosDocument,
        makeVariables: (filter) => ({
          filter: filter.makeFindFilter(),
          studio_filter_ast: filter.makeFilterAST(),
        }),
        extractResult: (data) => ({
          count: data?.findStudios.count ?? 0,
          items: data?.findStudios.studios ?? [],
        }),
      },
      renderCard: (studio, isMobile, selected, onSelectedChanged) => (
        <StudioCard
          key={studio.id}
          studio={studio}
          isMobile={isMobile}
          selected={selected}
          onSelectedChanged={onSelectedChanged}
          onEdit={() => setEditingId(studio.id)}
        />
      ),
      zoomable: true,
    }),
    [],
  );

  return (
    <>
      <EntityListPage
        key={tagId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.TagStudios}
        mobileChromeFixed
      />
      <StudioEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}

// ── Markers tab ────────────────────────────────────────────────────────────────

export function TagMarkersTab({ tag }: { tag: TagData }) {
  const ctx = useConfigurationContextOptional();
  const { id: tagId, name: tagName } = tag;
  const gqlConfig = ctx?.configuration;
  const defaultFilter = useMemo(
    () => makeTagFilter(FilterMode.SceneMarkers, tagId, tagName, gqlConfig),
    [tagId, tagName, gqlConfig],
  );

  const {
    onCardPreviewClick,
    onItemsChanged,
    pageNavRef,
    lightboxElement,
    lightboxOpen,
  } = useMarkerLightbox();

  const config = useMemo<
    EntityListPageConfig<
      GQL.FindSceneMarkersQuery,
      MarkerItem,
      GQL.FindSceneMarkersQueryVariables
    >
  >(
    () => ({
      filterMode: GQL.FilterMode.SceneMarkers,
      source: {
        kind: "graphql",
        query: GQL.FindSceneMarkersDocument,
        makeVariables: (filter) => ({
          filter: filter.makeFindFilter(),
          scene_marker_filter_ast: filter.makeFilterAST(),
        }),
        extractResult: (data) => ({
          count: data?.findSceneMarkers.count ?? 0,
          items: data?.findSceneMarkers.scene_markers ?? [],
        }),
      },
      renderCard: (
        marker,
        isMobile,
        selected,
        onSelectedChanged,
        onPreviewClick,
      ) => (
        <MarkerCard
          key={marker.id}
          marker={marker}
          isMobile={isMobile}
          selected={selected}
          onSelectedChanged={onSelectedChanged}
          onPreviewClick={onPreviewClick}
        />
      ),
      onCardPreviewClick,
      pageNavRef,
      onItemsChanged,
    }),
    [onCardPreviewClick, onItemsChanged, pageNavRef],
  );

  return (
    <>
      <EntityListPage
        key={tagId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.TagMarkers}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      {lightboxElement}
    </>
  );
}
