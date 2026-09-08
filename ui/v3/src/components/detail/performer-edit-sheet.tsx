import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { EntityEditSheet } from "./entity-edit-sheet";
import { PerformerEditForm } from "./performer-edit-form";

interface PerformerEditSheetProps {
  id: string | null;
  onClose: () => void;
}

export function PerformerEditSheet({ id, onClose }: PerformerEditSheetProps) {
  const intl = useIntl();
  const { data, loading } = useQuery(GQL.FindPerformerDocument, {
    variables: { id: id ?? "" },
    skip: !id,
  });

  const performer = data?.findPerformer;

  return (
    <EntityEditSheet
      open={!!id}
      onClose={onClose}
      entityType={intl.formatMessage({
        id: "performer",
        defaultMessage: "Performer",
      })}
      loading={loading && !performer}
    >
      {performer && (
        <PerformerEditForm
          performer={performer}
          onSaved={onClose}
          onDeleted={onClose}
        />
      )}
    </EntityEditSheet>
  );
}
