import { useState, useMemo } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { Plus } from "lucide-react";
import { EntityListPage } from "src/components/list";
import { usePerformerListConfig } from "src/components/list/entity-list-configs";
import { View } from "src/components/list/views";
import { Button } from "src/components/ui/button";
import { usePerformerTableColumns } from "./-table-columns";
import { PerformerEditSheet } from "src/components/detail/performer-edit-sheet";
import { PerformerCreateSheet } from "src/components/detail/performer-create-sheet";

function PerformersPage() {
  const intl = useIntl();
  const navigate = useNavigate();
  const tableColumns = usePerformerTableColumns();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const performerBase = usePerformerListConfig(setEditingId);

  const config = useMemo(
    () => ({
      ...performerBase,
      view: View.Performers,
      tableColumns,
      tableVisibilityKey: "performers",
      pageActions: (
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="size-4" />
          {intl.formatMessage({ id: "actions.new", defaultMessage: "New" })}
        </Button>
      ),
    }),
    [performerBase, tableColumns, intl],
  );

  return (
    <>
      <EntityListPage config={config} />
      <PerformerEditSheet id={editingId} onClose={() => setEditingId(null)} />
      <PerformerCreateSheet
        open={creating}
        onOpenChange={setCreating}
        onCreated={(id) =>
          navigate({
            to: "/performers/$performerId",
            params: { performerId: id },
          })
        }
      />
    </>
  );
}

export const Route = createFileRoute("/performers/")({
  component: PerformersPage,
});
