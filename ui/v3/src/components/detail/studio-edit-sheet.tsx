import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { EntityEditSheet } from "./entity-edit-sheet";
import { StudioEditForm } from "./studio-edit-form";

interface StudioEditSheetProps {
  id: string | null;
  onClose: () => void;
}

export function StudioEditSheet({ id, onClose }: StudioEditSheetProps) {
  const intl = useIntl();
  const { data, loading } = useQuery(GQL.FindStudioDocument, {
    variables: { id: id ?? "" },
    skip: !id,
  });

  const studio = data?.findStudio;

  return (
    <EntityEditSheet
      open={!!id}
      onClose={onClose}
      entityType={intl.formatMessage({
        id: "studio",
        defaultMessage: "Studio",
      })}
      loading={loading && !studio}
    >
      {studio && (
        <StudioEditForm studio={studio} onSaved={onClose} onDeleted={onClose} />
      )}
    </EntityEditSheet>
  );
}
