import type React from "react";
import { useState } from "react";
import { useIntl } from "react-intl";
import { DateInput as DateInputRoot } from "@ui-sdk/react/date-input";
import { CalendarIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { DatePicker } from "@/components/ui/date-picker";
import { FieldError } from "@/components/ui/field";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

interface Props {
  className?: string;
  disabled?: boolean;
  invalid?: boolean;
  disabledDays?: (date: Date) => boolean;
  /** "YYYY-MM-DD", or "YYYY-MM-DD HH:MM" when isTime is set */
  value: string;
  isTime?: boolean;
  onValueChange(value: string): void;
  placeholder?: string;
  error?: string;
}

const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/;
const TIME_PATTERN = /^\d{2}:\d{2}$/;

/** Reads the value as Date Input's ISO form. Partial or invalid text is null. */
function toSegmentValue(value: string, isTime: boolean): string | null {
  const text = value.trim();
  const date = text.slice(0, 10);
  if (!DATE_PATTERN.test(date)) return null;
  if (!isTime) return date;
  const time = text.slice(11, 16);
  return TIME_PATTERN.test(time) ? `${date}T${time}` : null;
}

function fromSegmentValue(value: string | null, isTime: boolean): string {
  if (value === null) return "";
  return isTime ? value.replace("T", " ") : value;
}

/** Calendar and Date Input match unavailable dates by their ISO day. */
function toUnavailable(disabledDays?: (date: Date) => boolean) {
  return disabledDays
    ? (iso: string) => disabledDays(new Date(`${iso.slice(0, 10)}T00:00:00`))
    : undefined;
}

export const DateInput: React.FC<Props> = ({
  className,
  disabled,
  invalid,
  disabledDays,
  value,
  isTime = false,
  onValueChange,
  placeholder,
  error,
}) => {
  const intl = useIntl();
  const [open, setOpen] = useState(false);
  const label =
    placeholder ?? intl.formatMessage({ id: "date", defaultMessage: "Date" });
  const unavailable = toUnavailable(disabledDays);

  if (!isTime) {
    return (
      <>
        <DatePicker
          className={className}
          value={value}
          onChange={onValueChange}
          disabled={disabled}
          invalid={invalid}
          placeholder={placeholder}
          unavailable={unavailable}
        />
        {error && <FieldError>{error}</FieldError>}
      </>
    );
  }

  const text = value.trim();
  const day = DATE_PATTERN.test(text.slice(0, 10)) ? text.slice(0, 10) : null;
  const time = text.slice(11, 16);

  // A Date Input with a time can't show a bare date, so a day picked before
  // any time is set starts at midnight. A chosen time is kept.
  function pickDay(next: string) {
    onValueChange(`${next} ${TIME_PATTERN.test(time) ? time : "00:00"}`);
    setOpen(false);
  }

  return (
    <>
      <div
        data-slot="date-time-input"
        className={cn("flex items-center gap-1", className)}
      >
        <DateInputRoot
          className="min-w-0 flex-1"
          aria-label={label}
          {...(invalid ? { "data-invalid": true } : {})}
          value={toSegmentValue(value, true)}
          onValueChange={(next) => onValueChange(fromSegmentValue(next, true))}
          granularity="minute"
          locale={intl.locale}
          disabled={disabled}
          unavailable={unavailable}
        />
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="shrink-0"
                aria-label={intl.formatMessage({
                  id: "actions.choose_date",
                  defaultMessage: "Choose a date",
                })}
                disabled={disabled}
              >
                <CalendarIcon />
              </Button>
            }
          />
          <PopoverContent align="end" className="w-auto p-0">
            <Calendar
              captionLayout="dropdown"
              locale={intl.locale}
              value={day}
              unavailable={unavailable}
              onValueChange={(next) => {
                if (typeof next === "string") pickDay(next);
              }}
            />
          </PopoverContent>
        </Popover>
      </div>
      {error && <FieldError>{error}</FieldError>}
    </>
  );
};
