import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { EntityEditSheet } from "./entity-edit-sheet";
import { GalleryEditForm } from "./gallery-edit-form";

interface GalleryEditSheetProps {
  id: string | null;
  onClose: () => void;
}

export function GalleryEditSheet({ id, onClose }: GalleryEditSheetProps) {
  const intl = useIntl();
  const { data, loading } = useQuery(GQL.FindGalleryDocument, {
    variables: { id: id ?? "" },
    skip: !id,
  });

  const gallery = data?.findGallery;

  return (
    <EntityEditSheet
      open={!!id}
      onClose={onClose}
      entityType={intl.formatMessage({
        id: "gallery",
        defaultMessage: "Gallery",
      })}
      loading={loading && !gallery}
    >
      {gallery && (
        <GalleryEditForm
          gallery={gallery}
          onSaved={onClose}
          onDeleted={onClose}
        />
      )}
    </EntityEditSheet>
  );
}
