import { useId } from "react";
import { FormattedMessage } from "react-intl";
import { Field, FieldLabel } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";

export function CaseSensitivityToggle({
  checked,
  onCheckedChange,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  const id = useId();
  return (
    <Field orientation="horizontal">
      <Switch
        id={id}
        size="sm"
        checked={checked}
        onCheckedChange={onCheckedChange}
      />
      <FieldLabel htmlFor={id}>
        <FormattedMessage
          id="search_filter.case_sensitive"
          defaultMessage="Case sensitive"
        />
      </FieldLabel>
    </Field>
  );
}
