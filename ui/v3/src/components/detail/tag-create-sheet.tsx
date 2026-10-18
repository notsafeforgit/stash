import { useIntl } from "react-intl";
import { EntityEditSheet } from "./entity-edit-sheet";
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
    <EntityEditSheet
      open={open}
      onClose={() => onOpenChange(false)}
      mode="create"
      entityType={intl.formatMessage({ id: "tag", defaultMessage: "Tag" })}
    >
      <TagEditForm
        mode="create"
        onCreated={(id) => {
          onCreated?.(id);
          onOpenChange(false);
        }}
      />
    </EntityEditSheet>
  );
}
