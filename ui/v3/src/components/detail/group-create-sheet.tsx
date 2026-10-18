import { useIntl } from "react-intl";
import { EntityEditSheet } from "./entity-edit-sheet";
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
    <EntityEditSheet
      open={open}
      onClose={() => onOpenChange(false)}
      mode="create"
      entityType={intl.formatMessage({ id: "group", defaultMessage: "Group" })}
    >
      <GroupEditForm
        mode="create"
        onCreated={(id) => {
          onCreated?.(id);
          onOpenChange(false);
        }}
      />
    </EntityEditSheet>
  );
}
