import { useId } from "react";
import { useIntl } from "react-intl";
import { Field, FieldLabel } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";

export function BulkApplyToggle({
  totalCount,
  checked,
  onCheckedChange,
  disabled = false,
}: {
  totalCount: number;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
}) {
  const id = useId();
  const intl = useIntl();
  return (
    <Field orientation="horizontal" data-disabled={disabled}>
      <FieldLabel htmlFor={id}>
        {intl.formatMessage(
          {
            id: "dialogs.bulk_edit.apply_to_all",
            defaultMessage: "Apply to all {count} matching",
          },
          { count: totalCount },
        )}
      </FieldLabel>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
        size="sm"
      />
    </Field>
  );
}
