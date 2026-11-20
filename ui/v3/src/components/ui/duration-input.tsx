import type React from "react";
import { useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { ChevronUp, ChevronDown, Clock } from "lucide-react";
import { secondsToTimestamp, timestampToSeconds } from "src/utils/duration";
import { Input } from "src/components/ui/input";
import { Button } from "src/components/ui/button";
import { FieldError } from "@/components/ui/field";
import { cn } from "@/lib/utils";

interface Props {
  disabled?: boolean;
  value: number | null | undefined;
  setValue(value: number | null): void;
  onReset?(): void;
  className?: string;
  placeholder?: string;
  allowNegative?: boolean;
  error?: string;
}

const includeMS = true;

// Time Input has no milliseconds, and Number Input formats only with Intl
// options, so the hh:mm:ss.ms text stays a plain text field here.
export const DurationInput: React.FC<Props> = ({
  disabled,
  value,
  setValue,
  onReset,
  className,
  placeholder,
  allowNegative = false,
  error,
}) => {
  const intl = useIntl();
  const [tmpValue, setTmpValue] = useState<string>();

  function onChange(e: React.ChangeEvent<HTMLInputElement>) {
    setTmpValue(e.currentTarget.value);
  }

  function updateValue(v: number | null) {
    const next = v !== null && !allowNegative && v < 0 ? null : v;
    setValue(next);
  }

  function onBlur() {
    if (tmpValue !== undefined) {
      updateValue(timestampToSeconds(tmpValue));
      setTmpValue(undefined);
    }
  }

  function increment() {
    setTmpValue(undefined);
    updateValue((value ?? 0) + 1);
  }

  function decrement() {
    setTmpValue(undefined);
    if (allowNegative) {
      updateValue((value ?? 0) - 1);
    } else {
      updateValue(value ? value - 1 : 0);
    }
  }

  const inputValue = useMemo(() => {
    if (tmpValue !== undefined) {
      return tmpValue;
    } else if (value !== null && value !== undefined) {
      return secondsToTimestamp(value, includeMS);
    }
    return "";
  }, [value, tmpValue]);

  const format = "hh:mm:ss.ms";
  const placeholderText = placeholder ? `${placeholder} (${format})` : format;

  return (
    <div
      data-slot="duration-input"
      className={cn("duration-input flex items-center gap-1", className)}
    >
      <Input
        type="text"
        aria-label={placeholderText}
        aria-invalid={error ? true : undefined}
        disabled={disabled}
        value={inputValue}
        onChange={onChange}
        onBlur={onBlur}
        placeholder={placeholderText}
      />
      {!disabled && (
        <div data-slot="duration-input-steps" className="flex flex-col">
          <Button
            variant="outline"
            size="icon-xs"
            aria-label={intl.formatMessage({
              id: "duration_input.increase",
              defaultMessage: "Increase",
            })}
            className="h-4 w-6 rounded-b-none"
            onClick={increment}
            tabIndex={-1}
          >
            <ChevronUp className="size-3" />
          </Button>
          <Button
            variant="outline"
            size="icon-xs"
            aria-label={intl.formatMessage({
              id: "duration_input.decrease",
              defaultMessage: "Decrease",
            })}
            className="h-4 w-6 rounded-t-none border-t-0"
            onClick={decrement}
            tabIndex={-1}
          >
            <ChevronDown className="size-3" />
          </Button>
        </div>
      )}
      {onReset && (
        <Button
          variant="outline"
          size="icon"
          aria-label={intl.formatMessage({
            id: "duration_input.use_current_time",
            defaultMessage: "Use current time",
          })}
          className="shrink-0"
          onClick={onReset}
          tabIndex={-1}
        >
          <Clock className="size-4" />
        </Button>
      )}
      {error && <FieldError>{error}</FieldError>}
    </div>
  );
};
