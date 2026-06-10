import { useIntl } from "react-intl";
import { Sheet, SheetContent, SheetTitle } from "src/components/ui/sheet";
import { TagEditForm } from "./tag-edit-form";

interface TagCreateSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (id: string) => void;
}

export function TagCreateSheet({
  open,
  onOpenChange,
  onCreated,
}: TagCreateSheetProps) {
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
                .formatMessage({ id: "tag", defaultMessage: "Tag" })
                .toLocaleLowerCase(),
            },
          )}
        </SheetTitle>
        <TagEditForm
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
