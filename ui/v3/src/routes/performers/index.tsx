import { useState, useMemo } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useDocumentTitle } from "src/hooks/title";
import { listCreateAction } from "@/components/list/list-create-action";
import { EntityListPage } from "src/components/list";
import { usePerformerListConfig } from "src/components/list/entity-list-configs";
import { View } from "src/components/list/views";
import { usePerformerTableColumns } from "./-table-columns";
import {
  PerformerEditSheet,
  PerformerCreateSheet,
} from "@/components/detail/deferred-overlays";

function PerformersPage() {
  const intl = useIntl();
  useDocumentTitle(
    intl.formatMessage({ id: "performers", defaultMessage: "Performers" }),
  );
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
      pageActions: [
        listCreateAction(intl, intl.formatMessage({ id: "performer" }), () =>
          setCreating(true),
        ),
      ],
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
