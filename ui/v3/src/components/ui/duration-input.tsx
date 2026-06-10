import type React from "react";
import { useMemo, useState } from "react";
import { ChevronUp, ChevronDown, Clock } from "lucide-react";
import { secondsToTimestamp, timestampToSeconds } from "src/utils/duration";
import { Input } from "src/components/ui/input";
import { Button } from "src/components/ui/button";

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
      className={`duration-input flex items-center gap-1 ${className ?? ""}`}
    >
      <Input
        type="text"
        disabled={disabled}
        value={inputValue}
        onChange={onChange}
        onBlur={onBlur}
        placeholder={placeholderText}
      />
      {!disabled && (
        <div className="flex flex-col">
          <Button
            variant="outline"
            size="icon"
            className="h-4 w-6 rounded-b-none"
            onClick={increment}
            tabIndex={-1}
          >
            <ChevronUp className="size-3" />
          </Button>
          <Button
            variant="outline"
            size="icon"
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
          className="h-8 w-8 shrink-0"
          onClick={onReset}
          tabIndex={-1}
        >
          <Clock className="size-4" />
        </Button>
      )}
      {error && <div className="text-destructive text-sm">{error}</div>}
    </div>
  );
};
