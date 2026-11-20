import { useIntl } from "react-intl";
import {
  DatePicker as DatePickerRoot,
  DatePickerContent,
  DatePickerInput,
  DatePickerTrigger,
} from "@ui-sdk/react/date-picker";
import { CalendarPanel } from "@/components/ui/calendar";
import { cn } from "@/lib/utils";

interface DatePickerProps {
  /** ISO date string (YYYY-MM-DD) or empty string */
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  invalid?: boolean;
  /** Dates that can't be picked, as ISO dates (YYYY-MM-DD) */
  unavailable?: (iso: string) => boolean;
  placeholder?: string;
  className?: string;
}

export function DatePicker({
  value,
  onChange,
  disabled,
  invalid,
  unavailable,
  placeholder,
  className,
}: DatePickerProps) {
  const intl = useIntl();
  const label =
    placeholder ??
    intl.formatMessage({
      id: "actions.choose_date",
      defaultMessage: "Choose a date",
    });

  return (
    <DatePickerRoot
      value={value || null}
      onValueChange={(next) => onChange(typeof next === "string" ? next : "")}
      locale={intl.locale}
      disabled={disabled}
      unavailable={unavailable}
      className={cn("w-full", className)}
    >
      <DatePickerInput
        aria-label={label}
        placeholder={label}
        {...(invalid ? { "data-invalid": true } : {})}
      />
      <DatePickerTrigger />
      <DatePickerContent>
        <CalendarPanel captionLayout="dropdown" />
      </DatePickerContent>
    </DatePickerRoot>
  );
}
