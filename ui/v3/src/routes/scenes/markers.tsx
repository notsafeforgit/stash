import { useMemo } from "react";
import { createFileRoute } from "@tanstack/react-router";
import * as GQL from "src/core/generated-graphql";
import { EntityListPage, type EntityListPageConfig } from "src/components/list";
import { MarkerCard } from "src/components/cards";
import { useMarkerLightbox } from "src/components/lightbox";
import { View } from "src/components/list/views";
import { markerTableColumns } from "./-markers-table-columns";

type MarkersQuery = GQL.FindSceneMarkersQuery;
type MarkerItem =
  GQL.FindSceneMarkersQuery["findSceneMarkers"]["scene_markers"][number];

function MarkersPage() {
  const {
    onCardPreviewClick,
    onItemsChanged,
    pageNavRef,
    lightboxElement,
    lightboxOpen,
  } = useMarkerLightbox();

  const config = useMemo<EntityListPageConfig<MarkersQuery, MarkerItem>>(
    () => ({
      filterMode: GQL.FilterMode.SceneMarkers,
      view: View.SceneMarkers,
      query: GQL.FindSceneMarkersDocument,
      makeVariables: (filter) => ({
        filter: filter.makeFindFilter(),
        scene_marker_filter: filter.makeFilter(),
        scene_marker_filter_ast: filter.makeFilterAST(),
      }),
      extractResult: (data) => ({
        count: data?.findSceneMarkers.count ?? 0,
        items: data?.findSceneMarkers.scene_markers ?? [],
      }),
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
      tableColumns: markerTableColumns,
      tableVisibilityKey: "markers",
      onCardPreviewClick,
      pageNavRef,
      onItemsChanged,
    }),
    [onCardPreviewClick, onItemsChanged, pageNavRef],
  );

  return (
    <>
      <EntityListPage
        config={config}
        keyboardShortcutsDisabled={lightboxOpen}
      />
      {lightboxElement}
    </>
  );
}

export const Route = createFileRoute("/scenes/markers")({
  component: MarkersPage,
});
