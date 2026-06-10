import type React from "react";
import { useId, useState } from "react";
import { useIntl } from "react-intl";
import { format } from "date-fns";
import { ChevronDownIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

interface Props {
  className?: string;
  disabled?: boolean;
  disabledDays?: (date: Date) => boolean;
  value: string;
  isTime?: boolean;
  onValueChange(value: string): void;
  placeholder?: string;
  error?: string;
}

function parseDate(value: string): Date | undefined {
  // Accept YYYY-MM-DD or YYYY-MM-DD HH:MM
  const datePart = value.trim().slice(0, 10);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(datePart)) return undefined;
  const d = new Date(datePart + "T00:00:00");
  return Number.isNaN(d.getTime()) ? undefined : d;
}

function formatDate(d: Date): string {
  const year = d.getFullYear();
  const month = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export const DateInput: React.FC<Props> = ({
  disabled,
  disabledDays,
  value,
  isTime,
  onValueChange,
  placeholder,
  error,
}) => {
  const intl = useIntl();
  const dateId = useId();
  const timeId = useId();
  const [open, setOpen] = useState(false);

  const selected = parseDate(value);
  // Extract HH:MM from "YYYY-MM-DD HH:MM"
  const timePart = value.trim().slice(11);

  function handleDaySelect(day: Date | undefined) {
    if (!day) return;
    const dateStr = formatDate(day);
    onValueChange(isTime && timePart ? `${dateStr} ${timePart}` : dateStr);
    setOpen(false);
  }

  function handleTimeChange(e: React.ChangeEvent<HTMLInputElement>) {
    const dateStr = selected ? formatDate(selected) : "";
    onValueChange(dateStr ? `${dateStr} ${e.target.value}` : e.target.value);
  }

  // Placeholder applies to the date-picker button. When `isTime` is true the
  // time is entered in a separate sibling input, so the date button should
  // still display only the date format, not "YYYY-MM-DD HH:MM".
  const placeholderText =
    placeholder ?? intl.formatMessage({ id: "date_format" });

  const datePicker = (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            variant="outline"
            id={dateId}
            disabled={disabled}
            className="w-full justify-between font-normal"
          />
        }
      >
        {selected ? (
          format(selected, "PP")
        ) : (
          <span className="text-muted-foreground">{placeholderText}</span>
        )}
        <ChevronDownIcon data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent className="w-auto overflow-hidden p-0" align="start">
        <Calendar
          mode="single"
          selected={selected}
          captionLayout="dropdown"
          defaultMonth={selected}
          onSelect={handleDaySelect}
          disabled={disabledDays}
        />
      </PopoverContent>
    </Popover>
  );

  if (isTime) {
    return (
      <>
        <FieldGroup className="flex-row gap-2">
          <Field>
            <FieldLabel htmlFor={dateId}>
              {intl.formatMessage({ id: "date", defaultMessage: "Date" })}
            </FieldLabel>
            {datePicker}
          </Field>
          <Field className="w-32 shrink-0">
            <FieldLabel htmlFor={timeId}>
              {intl.formatMessage({ id: "time", defaultMessage: "Time" })}
            </FieldLabel>
            <Input
              type="time"
              id={timeId}
              step="60"
              value={timePart}
              disabled={disabled || !selected}
              onChange={handleTimeChange}
              className="appearance-none bg-background [&::-webkit-calendar-picker-indicator]:hidden [&::-webkit-calendar-picker-indicator]:appearance-none"
            />
          </Field>
        </FieldGroup>
        {error && <div className="text-sm text-destructive">{error}</div>}
      </>
    );
  }

  return (
    <>
      {datePicker}
      {error && <div className="text-sm text-destructive">{error}</div>}
    </>
  );
};
