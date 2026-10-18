import { useState, useMemo } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useDocumentTitle } from "src/hooks/title";
import { ListCreateButton } from "@/components/list/list-create-button";
import { EntityListPage } from "src/components/list";
import { useGroupListConfig } from "src/components/list/entity-list-configs";
import { View } from "src/components/list/views";
import {
  GroupEditSheet,
  GroupCreateSheet,
} from "@/components/detail/deferred-overlays";

function GroupsPage() {
  const intl = useIntl();
  useDocumentTitle(
    intl.formatMessage({ id: "groups", defaultMessage: "Groups" }),
  );
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
        <ListCreateButton
          entityType={intl.formatMessage({ id: "group" })}
          onClick={() => setCreating(true)}
        />
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
