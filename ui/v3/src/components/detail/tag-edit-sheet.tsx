import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { EntityEditSheet } from "./entity-edit-sheet";
import { TagEditForm } from "./tag-edit-form";

interface TagEditSheetProps {
  id: string | null;
  onClose: () => void;
}

export function TagEditSheet({ id, onClose }: TagEditSheetProps) {
  const intl = useIntl();
  const { data, loading } = useQuery(GQL.FindTagDocument, {
    variables: { id: id ?? "" },
    skip: !id,
  });

  const tag = data?.findTag;

  return (
    <EntityEditSheet
      open={!!id}
      onClose={onClose}
      entityType={intl.formatMessage({ id: "tag", defaultMessage: "Tag" })}
      loading={loading && !tag}
    >
      {tag && <TagEditForm tag={tag} onSaved={onClose} onDeleted={onClose} />}
    </EntityEditSheet>
  );
}
