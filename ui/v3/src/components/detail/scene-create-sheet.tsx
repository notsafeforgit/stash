import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Sheet, SheetContent, SheetTitle } from "src/components/ui/sheet";
import { SceneEditForm, type SceneFormValues } from "./scene-edit-form";

interface SceneCreateSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Pre-fill the form (used by split-from-file). */
  initialValues?: Partial<SceneFormValues>;
  /** Extra fields merged into the SceneCreateInput (e.g. file_ids). */
  createInputExtras?: Partial<GQL.SceneCreateInput>;
  onCreated?: (id: string) => void;
}

export function SceneCreateSheet({
  open,
  onOpenChange,
  initialValues,
  createInputExtras,
  onCreated,
}: SceneCreateSheetProps) {
  const intl = useIntl();

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="sm:max-w-xl overflow-y-auto p-0"
        showCloseButton={false}
      >
        <SheetTitle className="sr-only">
          {intl.formatMessage(
            {
              id: "actions.create_entity",
              defaultMessage: "Create {entityType}",
            },
            {
              entityType: intl
                .formatMessage({ id: "scene", defaultMessage: "Scene" })
                .toLocaleLowerCase(),
            },
          )}
        </SheetTitle>
        {open && (
          <SceneEditForm
            mode="create"
            initialValues={initialValues}
            createInputExtras={createInputExtras}
            onCreated={(id) => {
              onCreated?.(id);
              onOpenChange(false);
            }}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}
