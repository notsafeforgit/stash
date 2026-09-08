import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { EntityEditSheet } from "./entity-edit-sheet";
import { ImageEditForm } from "./image-edit-form";

interface ImageEditSheetProps {
  id: string | null;
  onClose: () => void;
}

export function ImageEditSheet({ id, onClose }: ImageEditSheetProps) {
  const intl = useIntl();
  const { data, loading } = useQuery(GQL.FindImageDocument, {
    variables: { id: id ?? "" },
    skip: !id,
  });

  const image = data?.findImage;

  return (
    <EntityEditSheet
      open={!!id}
      onClose={onClose}
      entityType={intl.formatMessage({ id: "image", defaultMessage: "Image" })}
      loading={loading && !image}
    >
      {image && <ImageEditForm image={image} onSaved={onClose} />}
    </EntityEditSheet>
  );
}
