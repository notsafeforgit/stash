import type React from "react";
import { useMemo } from "react";
import { useIntl } from "react-intl";
import { getCountries, countryCodeToFlag } from "src/utils/country";
import { PinnableComboBox } from "src/components/ui/pinnable-combo-box";

interface Props {
  value?: string;
  onChange?: (value: string) => void;
  disabled?: boolean;
  // menuPortalTarget accepted but ignored — PinnableComboBox uses a Radix Portal
  menuPortalTarget?: HTMLElement | null;
}

export const CountrySelect: React.FC<Props> = ({
  value = "",
  onChange,
  disabled,
}) => {
  const { locale } = useIntl();

  const options = useMemo(
    () =>
      getCountries(locale).map((o) => {
        const flag = countryCodeToFlag(o.value);
        return {
          value: o.value,
          label: flag ? `${flag} ${o.label}` : o.label,
        };
      }),
    [locale],
  );

  const currentLabel =
    options.find((o) => o.value === value)?.label ?? value ?? "";

  return (
    <PinnableComboBox
      currentLabel={currentLabel}
      options={options}
      selectedValue={value}
      disabled={disabled}
      onSelect={(v) => onChange?.(v)}
    />
  );
};
