import { useMemo, useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useDocumentTitle } from "src/hooks/title";
import * as GQL from "src/core/generated-graphql";
import { EntityListPage, type EntityListPageConfig } from "src/components/list";
import { MarkerCard } from "src/components/cards";
import { useMarkerLightbox } from "src/components/lightbox";
import { View } from "src/components/list/views";
import { markerTableColumns, markerTitle } from "./-markers-table-columns";
import { MarkerRowContextMenu } from "@/components/cards/use-marker-context-menu";
import { MarkerDeleteDialog } from "@/components/detail/marker-delete-dialog";
import type { ListDeleteSelection } from "@/components/list/entity-list-types";

type MarkersQuery = GQL.FindSceneMarkersQuery;
type MarkerItem =
  GQL.FindSceneMarkersQuery["findSceneMarkers"]["scene_markers"][number];

function MarkersPage() {
  const intl = useIntl();
  const [deleteSelection, setDeleteSelection] =
    useState<ListDeleteSelection | null>(null);
  useDocumentTitle(
    intl.formatMessage({ id: "markers", defaultMessage: "Markers" }),
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
      MarkersQuery,
      MarkerItem,
      GQL.FindSceneMarkersQueryVariables
    >
  >(
    () => ({
      filterMode: GQL.FilterMode.SceneMarkers,
      view: View.SceneMarkers,
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
      tableColumns: markerTableColumns,
      renderTableRow: (marker, row, onSelectedChanged) => (
        <MarkerRowContextMenu
          marker={marker}
          title={markerTitle(marker)}
          onSelectedChanged={onSelectedChanged}
        >
          {row}
        </MarkerRowContextMenu>
      ),
      tableVisibilityKey: "markers",
      onDeleteSelection: setDeleteSelection,
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
      {deleteSelection && (
        <MarkerDeleteDialog
          selection={deleteSelection}
          onClose={() => setDeleteSelection(null)}
        />
      )}
    </>
  );
}

export const Route = createFileRoute("/scenes/markers")({
  component: MarkersPage,
});
