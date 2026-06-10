/**
 * Uncontrolled search input with internal 300 ms debounce.
 *
 * Uses the shadcn `<Input>` (Base UI primitive) in uncontrolled mode
 * (ref + defaultValue) so React never mutates the DOM value while the
 * user is typing — that was a real iOS cursor-reset hazard for a
 * controlled input updated on each keystroke. External filter resets
 * (e.g. loading a saved filter) are synced back to the DOM only when
 * the input is not focused.
 */

import type React from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import { X } from "lucide-react";
import { cn } from "src/lib/utils";
import { useIntl } from "react-intl";
import { Input } from "src/components/ui/input";
import { Button } from "src/components/ui/button";

export interface SearchInputProps {
  /** Current search term from the filter model (for external reset sync). */
  value: string | undefined;
  /** Called with the new search string after the 300 ms debounce settles. */
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  inputClassName?: string;
}

export const SearchInput: React.FC<SearchInputProps> = ({
  value,
  onChange,
  placeholder,
  className,
  inputClassName,
}) => {
  const intl = useIntl();
  const inputRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<number | null>(null);
  const [hasValue, setHasValue] = useState(!!value);

  // Sync external resets (e.g. loading a saved filter) without clobbering
  // what the user is currently typing.
  useEffect(() => {
    const el = inputRef.current;
    if (el && el !== document.activeElement) {
      el.value = value ?? "";
      setHasValue(!!(value ?? ""));
    }
  }, [value]);

  const handleChange = useCallback(
    (raw: string) => {
      setHasValue(!!raw);
      if (debounceRef.current) window.clearTimeout(debounceRef.current);
      debounceRef.current = window.setTimeout(() => {
        onChange(raw);
        debounceRef.current = null;
      }, 300);
    },
    [onChange],
  );

  const clear = useCallback(() => {
    if (inputRef.current) inputRef.current.value = "";
    handleChange("");
  }, [handleChange]);

  // Clean up pending debounce on unmount.
  useEffect(
    () => () => {
      if (debounceRef.current) window.clearTimeout(debounceRef.current);
    },
    [],
  );

  const defaultPlaceholder = intl.formatMessage({
    id: "search",
    defaultMessage: "Search…",
  });

  return (
    <div className={cn("relative", className)}>
      <Input
        ref={inputRef}
        type="search"
        className={cn(
          // Suppress the WebKit/Chromium native clear button on
          // type=search inputs — we render our own × button below.
          "h-7 text-sm [&::-webkit-search-cancel-button]:appearance-none",
          hasValue && "pr-6",
          inputClassName,
        )}
        placeholder={placeholder ?? defaultPlaceholder}
        defaultValue={value}
        onChange={(e) => handleChange(e.target.value)}
        onFocus={(e) => e.currentTarget.select()}
        onKeyDown={(e) => {
          if (e.key === "Enter") e.currentTarget.blur();
          if (e.key === "Escape") {
            clear();
            e.currentTarget.blur();
          }
        }}
      />
      {hasValue && (
        <div className="absolute inset-y-0 right-1 flex items-center">
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={clear}
            className="text-muted-foreground hover:text-foreground"
            aria-label={intl.formatMessage({
              id: "actions.clear",
              defaultMessage: "Clear",
            })}
            tabIndex={-1}
          >
            <X size={12} />
          </Button>
        </div>
      )}
    </div>
  );
};
