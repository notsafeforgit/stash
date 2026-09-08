import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { EntityEditSheet } from "./entity-edit-sheet";
import { GroupEditForm } from "./group-edit-form";

interface GroupEditSheetProps {
  id: string | null;
  onClose: () => void;
}

export function GroupEditSheet({ id, onClose }: GroupEditSheetProps) {
  const intl = useIntl();
  const { data, loading } = useQuery(GQL.FindGroupDocument, {
    variables: { id: id ?? "" },
    skip: !id,
  });

  const group = data?.findGroup;

  return (
    <EntityEditSheet
      open={!!id}
      onClose={onClose}
      entityType={intl.formatMessage({ id: "group", defaultMessage: "Group" })}
      loading={loading && !group}
    >
      {group && (
        <GroupEditForm group={group} onSaved={onClose} onDeleted={onClose} />
      )}
    </EntityEditSheet>
  );
}
