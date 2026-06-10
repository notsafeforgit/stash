import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { Sheet, SheetContent, SheetTitle } from "src/components/ui/sheet";
import { Spinner } from "src/components/ui/spinner";
import { SceneEditForm } from "./scene-edit-form";

interface SceneEditSheetProps {
  id: string | null;
  onClose: () => void;
}

export function SceneEditSheet({ id, onClose }: SceneEditSheetProps) {
  const intl = useIntl();
  const { data, loading } = useQuery(GQL.FindSceneDocument, {
    variables: { id: id ?? "" },
    skip: !id,
  });

  const scene = data?.findScene;

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
                .formatMessage({ id: "scene", defaultMessage: "Scene" })
                .toLocaleLowerCase(),
            },
          )}
        </SheetTitle>
        {loading && (
          <div className="flex items-center justify-center p-8">
            <Spinner />
          </div>
        )}
        {scene && <SceneEditForm scene={scene} onSaved={onClose} />}
      </SheetContent>
    </Sheet>
  );
}
