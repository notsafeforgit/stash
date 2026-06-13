import { useState, useMemo } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useDocumentTitle } from "src/hooks/title";
import { Plus } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { EntityListPage, type EntityListPageConfig } from "src/components/list";
import { StudioCard } from "src/components/cards";
import { StudioRowContextMenu } from "src/components/cards/use-studio-context-menu";
import { View } from "src/components/list/views";
import { Button } from "src/components/ui/button";
import { useStudioTableColumns } from "./-table-columns";
import { StudioEditSheet } from "src/components/detail/studio-edit-sheet";
import { StudioCreateSheet } from "src/components/detail/studio-create-sheet";

type StudiosQuery = GQL.FindStudiosQuery;
type StudioItem = GQL.FindStudiosQuery["findStudios"]["studios"][number];

function StudiosPage() {
  const intl = useIntl();
  useDocumentTitle(
    intl.formatMessage({ id: "studios", defaultMessage: "Studios" }),
  );
  const navigate = useNavigate();
  const tableColumns = useStudioTableColumns();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const config = useMemo<EntityListPageConfig<StudiosQuery, StudioItem>>(
    () => ({
      filterMode: GQL.FilterMode.Studios,
      view: View.Studios,
      query: GQL.FindStudiosDocument,
      makeVariables: (filter) => ({
        filter: filter.makeFindFilter(),
        studio_filter_ast: filter.makeFilterAST(),
      }),
      extractResult: (data) => ({
        count: data?.findStudios.count ?? 0,
        items: data?.findStudios.studios ?? [],
      }),
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
      pageActions: (
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="size-4" />
          {intl.formatMessage({ id: "actions.new", defaultMessage: "New" })}
        </Button>
      ),
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
