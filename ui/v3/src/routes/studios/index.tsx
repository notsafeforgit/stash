import { StudioListItems } from "@/components/list/entity-list-items";
import { useState, useMemo } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useDocumentTitle } from "src/hooks/title";
import { listCreateAction } from "@/components/list/list-create-action";
import * as GQL from "src/core/generated-graphql";
import { EntityListPage, type EntityListPageConfig } from "src/components/list";
import { StudioCard, StudioCardLayout } from "src/components/cards";
import { StudioRowContextMenu } from "src/components/cards/use-studio-context-menu";
import { View } from "src/components/list/views";
import { useStudioTableColumns } from "./-table-columns";
import {
  StudioEditSheet,
  StudioCreateSheet,
} from "@/components/detail/deferred-overlays";

type StudiosQuery = GQL.FindStudioListQuery;
type StudioItem = GQL.FindStudioListQuery["findStudios"]["studios"][number];

function StudiosPage() {
  const intl = useIntl();
  useDocumentTitle(
    intl.formatMessage({ id: "studios", defaultMessage: "Studios" }),
  );
  const navigate = useNavigate();
  const tableColumns = useStudioTableColumns();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const config = useMemo<
    EntityListPageConfig<
      StudiosQuery,
      StudioItem,
      GQL.FindStudioListQueryVariables
    >
  >(
    () => ({
      filterMode: GQL.FilterMode.Studios,
      ItemsProvider: StudioListItems.Provider,
      view: View.Studios,
      source: {
        kind: "graphql",
        query: GQL.FindStudioListDocument,
        countQuery: GQL.FindStudioListCountDocument,
        makeVariables: (filter) => ({
          filter: filter.makeFindFilter(),
          studio_filter_ast: filter.makeFilterAST(),
        }),
        extractResult: (data) => ({
          items: data?.findStudios.studios ?? [],
        }),
      },
      renderCardLayout: (studio, isMobile) => (
        <StudioCardLayout studio={studio} isMobile={isMobile} />
      ),
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
      renderTableRow: (studio, defaultRow, onSelectedChanged) => (
        <StudioRowContextMenu
          studio={studio}
          onEdit={() => setEditingId(studio.id)}
          onSelectedChanged={onSelectedChanged}
        >
          {defaultRow}
        </StudioRowContextMenu>
      ),
      zoomable: true,
      tableColumns,
      tableVisibilityKey: "studios",
      pageActions: [
        listCreateAction(intl, intl.formatMessage({ id: "studio" }), () =>
          setCreating(true),
        ),
      ],
    }),
    [tableColumns, intl],
  );

  return (
    <>
      <EntityListPage config={config} />
      <StudioEditSheet id={editingId} onClose={() => setEditingId(null)} />
      <StudioCreateSheet
        open={creating}
        onOpenChange={setCreating}
        onCreated={(id) =>
          navigate({ to: "/studios/$studioId", params: { studioId: id } })
        }
      />
    </>
  );
}

export const Route = createFileRoute("/studios/")({
  component: StudiosPage,
});
