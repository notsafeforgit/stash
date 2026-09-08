import { useMemo } from "react";
import { useIntl } from "react-intl";
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxList,
  ComboboxItem,
  useComboboxFilter,
} from "@/components/ui/combobox";

export interface CustomFieldNameInputProps {
  id: string;
  value: string;
  onChange: (name: string) => void;
  options: string[];
  loading?: boolean;
  disabled?: boolean;
  "aria-invalid"?: boolean;
  "aria-describedby"?: string;
}

/** Suggestions are optional: the raw name remains valid without a selection. */
export function CustomFieldNameInput({
  value,
  onChange,
  options,
  loading,
  disabled,
  ...inputProps
}: CustomFieldNameInputProps) {
  const intl = useIntl();
  const { contains } = useComboboxFilter({ locale: intl.locale });
  // The form owns the raw name, including names absent from the suggestions.
  // Filter from that value so a closing popup's query cannot become stale
  // when the user immediately reopens it to continue editing.
  const filteredOptions = useMemo(
    () => options.filter((name) => contains(name, value.trim())),
    [options, value, contains],
  );
  return (
    <Combobox
      items={options}
      filteredItems={filteredOptions}
      value={options.includes(value) ? value : null}
      inputValue={value}
      onValueChange={(name) => {
        if (name !== null) onChange(name);
      }}
      onInputValueChange={(name, details) => {
        // Selection resets must not erase a freely entered name. Suggestions
        // commit through onValueChange; typing (including deletion) comes here.
        if (details.reason === "input-change") {
          onChange(name);
        }
      }}
      disabled={disabled}
    >
      <ComboboxInput
        {...inputProps}
        disabled={disabled}
        placeholder={intl.formatMessage({
          id: "custom_fields.bulk.name_placeholder",
        })}
      />
      <ComboboxContent>
        <ComboboxEmpty>
          {loading
            ? intl.formatMessage({ id: "loading.generic" })
            : intl.formatMessage({ id: "custom_fields.bulk.type_name" })}
        </ComboboxEmpty>
        <ComboboxList>
          {(name: string) => (
            <ComboboxItem key={name} value={name}>
              {name}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}
