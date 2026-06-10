import { useIntl } from "react-intl";
import { Sheet, SheetContent, SheetTitle } from "src/components/ui/sheet";
import { GroupEditForm } from "./group-edit-form";

interface GroupCreateSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (id: string) => void;
}

export function GroupCreateSheet({
  open,
  onOpenChange,
  onCreated,
}: GroupCreateSheetProps) {
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
                .formatMessage({ id: "group", defaultMessage: "Group" })
                .toLocaleLowerCase(),
            },
          )}
        </SheetTitle>
        <GroupEditForm
          mode="create"
          onCreated={(id) => {
            onCreated?.(id);
            onOpenChange(false);
          }}
        />
      </SheetContent>
    </Sheet>
  );
}
