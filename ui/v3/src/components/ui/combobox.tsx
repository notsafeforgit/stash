import * as React from "react";
import { useIntl } from "react-intl";
import {
  Combobox as ComboboxPrimitive,
  ComboboxChip as ComboboxChipPrimitive,
  ComboboxChipRemove as ComboboxChipRemovePrimitive,
  ComboboxChips as ComboboxChipsPrimitive,
  ComboboxClear as ComboboxClearPrimitive,
  ComboboxContent as ComboboxContentPrimitive,
  ComboboxInput as ComboboxInputPrimitive,
  ComboboxItem as ComboboxItemPrimitive,
  ComboboxItemIndicator as ComboboxItemIndicatorPrimitive,
  ComboboxList as ComboboxListPrimitive,
  ComboboxTrigger as ComboboxTriggerPrimitive,
  type ComboboxContentState,
  type ComboboxProps,
  type ListData,
  type ListFilter,
} from "@ui-sdk/react/combobox";
import { XIcon } from "lucide-react";

import { cn } from "@/lib/utils";
import {
  type ShortcutOverlayRootProps,
  useOverlayOpenState,
} from "@/components/shortcut-provider";
import { useOverlayContainerElement } from "./overlay-container";

export {
  ComboboxEmpty,
  ComboboxGroup,
  ComboboxGroupLabel,
  ComboboxLabel,
  ComboboxSeparator,
  ComboboxValue,
} from "@ui-sdk/react/combobox";

type OpenChangeArgs<F> = F extends (open: boolean, ...args: infer Rest) => void
  ? Rest
  : never;

type QueryChangeDetails = Parameters<
  NonNullable<ComboboxProps<unknown>["onQueryChange"]>
>[1];

/** Base UI's name for a text change that came from the clear button. */
export type ComboboxInputValueChangeDetails = Omit<
  QueryChangeDetails,
  "reason"
> & {
  reason: QueryChangeDetails["reason"] | "input-clear";
};

/** Part props with a plain string `className`, which `cn()` accepts. */
type WithClassName<P> = Omit<P, "className"> & { className?: string };

type AnchorProp =
  | React.ComponentProps<typeof ComboboxContentPrimitive>["anchor"]
  | React.RefObject<HTMLElement | null>;

type DistributiveOmit<T, K extends PropertyKey> = T extends unknown
  ? Omit<T, K>
  : never;

type ValueOf<Value, Multiple> = Multiple extends true ? Value[] : Value | null;

type ValueChangeDetails = Parameters<
  NonNullable<ComboboxProps<string>["onValueChange"]>
>[1];

/**
 * The props the wrapper accepts. Base UI names (`itemToStringLabel`,
 * `inputValue`, `filter: null`, ...) stay, since the catalogMetadata plugin
 * and call sites use them; they map onto ui-sdk props in `Combobox`.
 */
type ComboboxRootProps<
  Value,
  Multiple extends boolean | undefined,
> = DistributiveOmit<
  ComboboxProps<Value, Multiple>,
  | "items"
  | "filter"
  | "query"
  | "defaultQuery"
  | "onQueryChange"
  | "itemToLabel"
  | "itemToValue"
  | "multiple"
  | "value"
  | "defaultValue"
  | "onValueChange"
> & {
  multiple?: Multiple;
  value?: ValueOf<Value, Multiple>;
  defaultValue?: ValueOf<Value, Multiple>;
  onValueChange?: (
    value: ValueOf<Value, Multiple>,
    details: ValueChangeDetails,
  ) => void;
  items?: readonly Value[];
  filteredItems?: readonly Value[];
  filter?: ListFilter<Value> | null;
  inputValue?: string;
  onInputValueChange?: (
    query: string,
    details: ComboboxInputValueChangeDetails,
  ) => void;
  itemToStringLabel?: (item: Value) => string;
  itemToStringValue?: (item: Value) => string;
};

type ComboboxCompat = {
  itemToLabel?: (item: unknown) => string;
};

const ComboboxCompatContext = React.createContext<ComboboxCompat>({});

function withIndicator<State>(
  children: React.ReactNode | ((state: State) => React.ReactNode),
  indicator: React.ReactNode,
) {
  return (state: State) => (
    <>
      {typeof children === "function" ? children(state) : children}
      {indicator}
    </>
  );
}

/** The text of plain children, so a label can be read without rendering. */
function textOf(children: unknown): string | undefined {
  if (children == null) return undefined;
  const parts: unknown[] = Array.isArray(children) ? children : [children];
  if (
    !parts.every(
      (part) =>
        part == null ||
        typeof part === "boolean" ||
        typeof part === "string" ||
        typeof part === "number",
    )
  ) {
    return undefined;
  }
  return parts
    .map((part) =>
      typeof part === "string" || typeof part === "number" ? String(part) : "",
    )
    .join("");
}

function defaultLabel(item: unknown): string {
  if (typeof item === "string") return item;
  if (
    item &&
    typeof item === "object" &&
    "label" in item &&
    typeof item.label === "string"
  ) {
    return item.label;
  }
  return String(item);
}

function toListData<T>(
  items: readonly T[] | undefined,
  toLabel: ((item: T) => string) | undefined,
): ListData<T> | undefined {
  return items?.map((item) => ({
    value: item,
    label: toLabel ? toLabel(item) : defaultLabel(item),
  }));
}

function toQueryChange(
  onChange: (query: string, details: ComboboxInputValueChangeDetails) => void,
) {
  return (query: string, details: QueryChangeDetails) =>
    onChange(query, {
      ...details,
      reason: details.reason === "clear-press" ? "input-clear" : details.reason,
    });
}

function Combobox<Value, Multiple extends boolean | undefined = false>({
  items,
  filteredItems,
  filter,
  inputValue,
  onInputValueChange,
  itemToStringLabel,
  itemToStringValue,
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
  ...props
}: ComboboxRootProps<Value, Multiple> & ShortcutOverlayRootProps) {
  const overlayProps = useOverlayOpenState<
    OpenChangeArgs<NonNullable<ComboboxProps<Value, Multiple>["onOpenChange"]>>
  >({
    open,
    defaultOpen,
    onOpenChange,
    blocksListShortcuts,
  });
  const listItems = React.useMemo(
    () => toListData(filteredItems ?? items, itemToStringLabel),
    [filteredItems, items, itemToStringLabel],
  );
  const compat = React.useMemo<ComboboxCompat>(
    () => ({
      itemToLabel: itemToStringLabel as ComboboxCompat["itemToLabel"],
    }),
    [itemToStringLabel],
  );

  return (
    <ComboboxCompatContext.Provider value={compat}>
      <ComboboxPrimitive
        {...(props as ComboboxProps<unknown, false>)}
        open={overlayProps.open}
        defaultOpen={overlayProps.defaultOpen}
        onOpenChange={
          overlayProps.onOpenChange as ComboboxProps<
            unknown,
            false
          >["onOpenChange"]
        }
        items={listItems}
        filter={
          filteredItems || filter === null
            ? false
            : (filter as ListFilter<unknown> | undefined)
        }
        query={inputValue}
        onQueryChange={onInputValueChange && toQueryChange(onInputValueChange)}
        itemToLabel={itemToStringLabel as ComboboxProps<unknown>["itemToLabel"]}
        itemToValue={itemToStringValue as ComboboxProps<unknown>["itemToValue"]}
      />
    </ComboboxCompatContext.Provider>
  );
}

function useResolvedAnchor(anchor: AnchorProp | undefined) {
  return React.useMemo(() => {
    if (anchor && typeof anchor === "object" && "current" in anchor) {
      const ref = anchor;
      return {
        getBoundingClientRect: () =>
          ref.current?.getBoundingClientRect() ??
          ({
            x: 0,
            y: 0,
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            width: 0,
            height: 0,
            toJSON: () => ({}),
          } as DOMRect),
      };
    }
    return anchor;
  }, [anchor]);
}

function ComboboxTrigger(
  props: WithClassName<React.ComponentProps<typeof ComboboxTriggerPrimitive>>,
) {
  return <ComboboxTriggerPrimitive {...props} />;
}

function ComboboxClear({
  className,
  children,
  ...props
}: WithClassName<React.ComponentProps<typeof ComboboxClearPrimitive>>) {
  const intl = useIntl();
  return (
    <ComboboxClearPrimitive
      aria-label={intl.formatMessage({
        id: "actions.clear",
        defaultMessage: "Clear",
      })}
      className={cn(className)}
      {...props}
    >
      {children ?? <XIcon aria-hidden="true" />}
    </ComboboxClearPrimitive>
  );
}

function ComboboxInput({
  className,
  children,
  disabled = false,
  showTrigger = true,
  showClear = false,
  ...props
}: WithClassName<React.ComponentProps<typeof ComboboxInputPrimitive>> & {
  children?: React.ReactNode;
  showTrigger?: boolean;
  showClear?: boolean;
}) {
  const intl = useIntl();
  return (
    <div
      data-slot="combobox-field"
      className={cn("relative flex w-auto min-w-0 items-center", className)}
    >
      <ComboboxInputPrimitive disabled={disabled} {...props} />
      {showTrigger && (
        <ComboboxTriggerPrimitive
          aria-label={intl.formatMessage({
            id: "actions.show_options",
            defaultMessage: "Show options",
          })}
          disabled={disabled}
        />
      )}
      {showClear && <ComboboxClear disabled={disabled} />}
      {children}
    </div>
  );
}

function ComboboxContent({
  className,
  side = "bottom",
  sideOffset = 6,
  align = "start",
  alignOffset = 0,
  anchor,
  ...props
}: WithClassName<
  Omit<React.ComponentProps<typeof ComboboxContentPrimitive>, "anchor">
> & {
  anchor?: AnchorProp;
}) {
  const container = useOverlayContainerElement();
  const resolvedAnchor = useResolvedAnchor(anchor);
  return (
    <ComboboxContentPrimitive
      side={side}
      sideOffset={sideOffset}
      align={align}
      alignOffset={alignOffset}
      anchor={
        resolvedAnchor as React.ComponentProps<
          typeof ComboboxContentPrimitive
        >["anchor"]
      }
      container={container}
      className={cn(
        "isolate z-50 max-w-(--available-width)",
        anchor != null
          ? "min-w-(--anchor-width)"
          : "min-w-[calc(var(--anchor-width)+--spacing(7))]",
        className,
      )}
      {...props}
    />
  );
}

function renderItems<T>(
  state: ComboboxContentState,
  render: (item: T, index: number) => React.ReactNode,
) {
  return state.items.map((entry, index) =>
    typeof entry === "object" && "value" in entry
      ? render(entry.value as T, index)
      : null,
  );
}

function ComboboxList<T = unknown>({
  className,
  children,
  ...props
}: WithClassName<
  Omit<React.ComponentProps<typeof ComboboxListPrimitive>, "children">
> & {
  children?: React.ReactNode | ((item: T, index: number) => React.ReactNode);
}) {
  return (
    <ComboboxListPrimitive
      className={cn(
        "no-scrollbar max-h-[min(calc(--spacing(72)---spacing(9)),calc(var(--available-height)---spacing(9)))] scroll-py-1 overflow-x-hidden overflow-y-auto overscroll-contain touch-pan-y data-empty:p-0",
        className,
      )}
      {...props}
    >
      {typeof children === "function"
        ? (state: ComboboxContentState) => renderItems(state, children)
        : children}
    </ComboboxListPrimitive>
  );
}

function ComboboxItem<T = string>({
  className,
  children,
  label,
  value,
  ...props
}: WithClassName<Parameters<typeof ComboboxItemPrimitive<T>>[0]>) {
  const compat = React.useContext(ComboboxCompatContext);
  const resolvedLabel =
    label ??
    textOf(children) ??
    (compat.itemToLabel ? compat.itemToLabel(value) : undefined);
  return (
    <ComboboxItemPrimitive
      value={value}
      label={resolvedLabel}
      className={cn(className)}
      {...props}
    >
      {withIndicator(children, <ComboboxItemIndicatorPrimitive />)}
    </ComboboxItemPrimitive>
  );
}

function ComboboxChips({
  className,
  ...props
}: WithClassName<React.ComponentProps<typeof ComboboxChipsPrimitive>>) {
  return <ComboboxChipsPrimitive className={cn(className)} {...props} />;
}

function ComboboxChip<T = string>({
  className,
  children,
  showRemove = true,
  ...props
}: WithClassName<
  Omit<Parameters<typeof ComboboxChipPrimitive<T>>[0], "children">
> & {
  children?: React.ReactNode;
  showRemove?: boolean;
}) {
  const intl = useIntl();
  return (
    <ComboboxChipPrimitive className={cn(className)} {...props}>
      <span className="truncate">{children}</span>
      {showRemove && (
        <ComboboxChipRemovePrimitive
          aria-label={intl.formatMessage({
            id: "actions.remove",
            defaultMessage: "Remove",
          })}
        >
          <XIcon aria-hidden="true" />
        </ComboboxChipRemovePrimitive>
      )}
    </ComboboxChipPrimitive>
  );
}

function ComboboxChipsInput({
  className,
  ...props
}: WithClassName<React.ComponentProps<typeof ComboboxInputPrimitive>>) {
  return <ComboboxInputPrimitive className={cn(className)} {...props} />;
}

function useComboboxAnchor() {
  return React.useRef<HTMLDivElement | null>(null);
}

export {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxChips,
  ComboboxChip,
  ComboboxChipsInput,
  ComboboxClear,
  ComboboxTrigger,
  useComboboxAnchor,
};
