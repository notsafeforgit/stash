import { useIntl } from "react-intl";
import { EntityEditSheet } from "./entity-edit-sheet";
import { StudioEditForm } from "./studio-edit-form";

interface StudioCreateSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (id: string) => void;
}

export function StudioCreateSheet({
  open,
  onOpenChange,
  onCreated,
}: StudioCreateSheetProps) {
  const intl = useIntl();

  return (
    <EntityEditSheet
      open={open}
      onClose={() => onOpenChange(false)}
      mode="create"
      entityType={intl.formatMessage({
        id: "studio",
        defaultMessage: "Studio",
      })}
    >
      <StudioEditForm
        mode="create"
        onCreated={(id) => {
          onCreated?.(id);
          onOpenChange(false);
        }}
      />
    </EntityEditSheet>
  );
}
