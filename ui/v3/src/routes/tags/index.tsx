import { useState, useMemo, useCallback } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { Plus } from "lucide-react";
import { EntityListPage } from "src/components/list";
import { useTagListConfig } from "src/components/list/entity-list-configs";
import { View } from "src/components/list/views";
import { Button } from "src/components/ui/button";
import { useTagTableColumns } from "./-table-columns";
import { TagEditSheet } from "src/components/detail/tag-edit-sheet";
import { TagCreateSheet } from "src/components/detail/tag-create-sheet";
import { TagTagger } from "src/components/tagger/tag-tagger";
import type * as GQL from "src/core/generated-graphql";

type TagItem = GQL.FindTagsQuery["findTags"]["tags"][number];

function TagsPage() {
  const intl = useIntl();
  const navigate = useNavigate();
  const tableColumns = useTagTableColumns();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const tagBase = useTagListConfig(setEditingId);

  const renderTagger = useCallback(
    (items: TagItem[]) => <TagTagger tags={items} />,
    [],
  );

  const config = useMemo(
    () => ({
      ...tagBase,
      view: View.Tags,
      tableColumns,
      tableVisibilityKey: "tags",
      renderTagger,
      pageActions: (
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="size-4" />
          {intl.formatMessage({ id: "actions.new", defaultMessage: "New" })}
        </Button>
      ),
    }),
    [tagBase, tableColumns, renderTagger, intl],
  );

  return (
    <>
      <EntityListPage config={config} />
      <TagEditSheet id={editingId} onClose={() => setEditingId(null)} />
      <TagCreateSheet
        open={creating}
        onOpenChange={setCreating}
        onCreated={(id) =>
          navigate({ to: "/tags/$tagId", params: { tagId: id } })
        }
      />
    </>
  );
}

export const Route = createFileRoute("/tags/")({
  component: TagsPage,
});
