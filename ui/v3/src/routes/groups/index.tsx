import { useState, useMemo } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { Plus } from "lucide-react";
import { EntityListPage } from "src/components/list";
import { useGroupListConfig } from "src/components/list/entity-list-configs";
import { View } from "src/components/list/views";
import { Button } from "src/components/ui/button";
import { GroupEditSheet } from "src/components/detail/group-edit-sheet";
import { GroupCreateSheet } from "src/components/detail/group-create-sheet";

function GroupsPage() {
  const intl = useIntl();
  const navigate = useNavigate();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const groupBase = useGroupListConfig(setEditingId);

  const config = useMemo(
    () => ({
      ...groupBase,
      view: View.Groups,
      tableVisibilityKey: "groups",
      pageActions: (
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="size-4" />
          {intl.formatMessage({ id: "actions.new", defaultMessage: "New" })}
        </Button>
      ),
    }),
    [groupBase, intl],
  );

  return (
    <>
      <EntityListPage config={config} />
      <GroupEditSheet id={editingId} onClose={() => setEditingId(null)} />
      <GroupCreateSheet
        open={creating}
        onOpenChange={setCreating}
        onCreated={(id) =>
          navigate({ to: "/groups/$groupId", params: { groupId: id } })
        }
      />
    </>
  );
}

export const Route = createFileRoute("/groups/")({
  component: GroupsPage,
});
