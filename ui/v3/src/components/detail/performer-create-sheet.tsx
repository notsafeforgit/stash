import { useIntl } from "react-intl";
import { EntityEditSheet } from "./entity-edit-sheet";
import { PerformerEditForm } from "./performer-edit-form";

interface PerformerCreateSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (id: string) => void;
}

export function PerformerCreateSheet({
  open,
  onOpenChange,
  onCreated,
}: PerformerCreateSheetProps) {
  const intl = useIntl();

  return (
    <EntityEditSheet
      open={open}
      onClose={() => onOpenChange(false)}
      mode="create"
      entityType={intl.formatMessage({
        id: "performer",
        defaultMessage: "Performer",
      })}
    >
      <PerformerEditForm
        mode="create"
        onCreated={(id) => {
          onCreated?.(id);
          onOpenChange(false);
        }}
      />
    </EntityEditSheet>
  );
}
