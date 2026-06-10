import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { Sheet, SheetContent, SheetTitle } from "src/components/ui/sheet";
import { Spinner } from "src/components/ui/spinner";
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
    <Sheet
      open={!!id}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        side="right"
        className="sm:max-w-xl overflow-y-auto p-0"
        showCloseButton={false}
      >
        <SheetTitle className="sr-only">
          {intl.formatMessage(
            {
              id: "actions.edit_entity",
              defaultMessage: "Edit {entityType}",
            },
            {
              entityType: intl
                .formatMessage({ id: "gallery", defaultMessage: "Gallery" })
                .toLocaleLowerCase(),
            },
          )}
        </SheetTitle>
        {loading && (
          <div className="flex items-center justify-center p-8">
            <Spinner />
          </div>
        )}
        {gallery && (
          <GalleryEditForm
            gallery={gallery}
            onSaved={onClose}
            onDeleted={onClose}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}
