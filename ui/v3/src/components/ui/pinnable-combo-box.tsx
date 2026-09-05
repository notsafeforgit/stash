import React from "react";
import { CheckIcon, Pin } from "lucide-react";
import { Combobox as ComboboxPrimitive } from "@base-ui/react";
import { cn } from "@/lib/utils";
import {
  Combobox,
  ComboboxContent,
  ComboboxGroup,
  ComboboxInput,
  ComboboxLabel,
  ComboboxList,
  ComboboxSeparator,
} from "@/components/ui/combobox";

// ── Pin button ────────────────────────────────────────────────────────────────

export interface PinButtonProps {
  pinned: boolean;
  onToggle: () => void;
  pinnedTitle?: string;
  unpinnedTitle?: string;
}

export function PinButton({
  pinned,
  onToggle,
  pinnedTitle = "Unpin",
  unpinnedTitle = "Pin",
}: PinButtonProps) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex shrink-0 items-center rounded border-none bg-transparent p-0.5 cursor-pointer transition-colors",
        pinned
          ? "text-primary hover:text-primary/70"
          : "text-foreground/40 hover:text-foreground",
      )}
      title={pinned ? pinnedTitle : unpinnedTitle}
      aria-label={pinned ? pinnedTitle : unpinnedTitle}
      onPointerDown={(e) => {
        e.stopPropagation();
        e.preventDefault();
      }}
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
      }}
    >
      <Pin
        className={cn("size-4 transition-transform", !pinned && "rotate-45")}
      />
    </button>
  );
}

// ── PinnableComboBox ──────────────────────────────────────────────────────────

export interface PinnableComboBoxOption {
  value: string;
  label: string;
}

export interface PinnableComboBoxProps {
  id?: string;
  "aria-label"?: string;
  "aria-labelledby"?: string;
  /** Displayed in the trigger. Derived automatically from the selected option;
   *  kept for call-site compatibility. */
  currentLabel: string;
  options: PinnableComboBoxOption[];
  selectedValue: string;
  searchPlaceholder?: string;
  pinnedValues?: string[];
  pinnedSectionLabel?: string;
  allSectionLabel?: string;
  side?: "top" | "bottom" | "left" | "right" | "inline-start" | "inline-end";
  align?: "start" | "end" | "center";
  disabled?: boolean;
  triggerClassName?: string;
  contentClassName?: string;
  /** Number of items visible before the list scrolls. Default: 8. */
  visibleItems?: number;
  renderItemAddon?: (value: string, isPinned: boolean) => React.ReactNode;
  onSelect: (value: string) => void;
}

export function PinnableComboBox({
  id,
  "aria-label": ariaLabel,
  "aria-labelledby": ariaLabelledBy,
  currentLabel,
  options,
  selectedValue,
  searchPlaceholder = "Search…",
  pinnedValues,
  pinnedSectionLabel = "Pinned",
  allSectionLabel,
  side,
  align = "start",
  disabled,
  triggerClassName,
  contentClassName,
  visibleItems: _visibleItems,
  renderItemAddon,
  onSelect,
}: PinnableComboBoxProps) {
  const [query, setQuery] = React.useState("");
  const [open, setOpen] = React.useState(false);

  // Numeric collation so "20"/"40"/"60"/"100" order numerically, while
  // regular string labels still sort alphabetically.
  const sorted = [...options].sort((a, b) =>
    a.label.localeCompare(b.label, undefined, { numeric: true }),
  );
  const q = query.trim().toLowerCase();
  const filtered = q
    ? sorted.filter((o) => o.label.toLowerCase().includes(q))
    : sorted;
  const pinned = pinnedValues?.length
    ? filtered.filter((o) => pinnedValues.includes(o.value))
    : [];
  const unpinned = pinnedValues?.length
    ? filtered.filter((o) => !pinnedValues.includes(o.value))
    : filtered;

  return (
    <Combobox
      value={selectedValue}
      onValueChange={(v: string | null) => {
        if (v !== null) onSelect(v);
      }}
      onInputValueChange={(v: string) => setQuery(v)}
      onOpenChange={(open: boolean) => {
        setOpen(open);
        if (open) setQuery("");
      }}
      itemToStringLabel={(v: string | null) =>
        options.find((o) => o.value === v)?.label ??
        (v === selectedValue ? currentLabel : String(v))
      }
    >
      <ComboboxInput
        id={id}
        aria-label={ariaLabel}
        aria-labelledby={ariaLabelledBy}
        placeholder={
          open ? searchPlaceholder : currentLabel || searchPlaceholder
        }
        showTrigger
        disabled={disabled}
        className={triggerClassName}
      />
      <ComboboxContent side={side} align={align} className={contentClassName}>
        <ComboboxList>
          {pinned.length > 0 && (
            <>
              <ComboboxGroup>
                <ComboboxLabel>{pinnedSectionLabel}</ComboboxLabel>
                {pinned.map((option) =>
                  renderItem(option, pinnedValues, renderItemAddon),
                )}
              </ComboboxGroup>
              <ComboboxSeparator />
            </>
          )}
          <ComboboxGroup>
            {allSectionLabel && (
              <ComboboxLabel>{allSectionLabel}</ComboboxLabel>
            )}
            {unpinned.map((option) =>
              renderItem(option, pinnedValues, renderItemAddon),
            )}
          </ComboboxGroup>
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

function renderItem(
  option: PinnableComboBoxOption,
  pinnedValues: string[] | undefined,
  renderItemAddon: PinnableComboBoxProps["renderItemAddon"],
) {
  const isPinned = pinnedValues?.includes(option.value) ?? false;
  return (
    <ComboboxPrimitive.Item
      key={option.value}
      value={option.value}
      className="relative flex w-full cursor-pointer select-none items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden data-highlighted:bg-accent data-highlighted:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50"
    >
      <span className="flex min-w-0 items-center gap-2">
        <ComboboxPrimitive.ItemIndicator className="flex size-4 shrink-0 items-center justify-center">
          <CheckIcon className="size-4" />
        </ComboboxPrimitive.ItemIndicator>
        <span className="truncate">{option.label}</span>
      </span>
      {renderItemAddon?.(option.value, isPinned)}
    </ComboboxPrimitive.Item>
  );
}
