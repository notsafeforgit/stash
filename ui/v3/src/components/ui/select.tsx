import type * as React from "react";
import {
  Select as SelectPrimitive,
  SelectContent as SelectContentPrimitive,
  SelectGroup as SelectGroupPrimitive,
  SelectGroupLabel as SelectGroupLabelPrimitive,
  SelectItem as SelectItemPrimitive,
  SelectItemIndicator as SelectItemIndicatorPrimitive,
  SelectLabel as SelectLabelPrimitive,
  SelectSeparator as SelectSeparatorPrimitive,
  SelectTrigger as SelectTriggerPrimitive,
  SelectValue as SelectValuePrimitive,
  type SelectProps,
} from "@ui-sdk/react/select";
import { cn } from "@/lib/utils";
import { useOverlayContainerElement } from "./overlay-container";
import {
  type ShortcutOverlayRootProps,
  useOverlayOpenState,
} from "@/components/shortcut-provider";

type OpenChangeArgs<F> = F extends (open: boolean, ...args: infer Rest) => void
  ? Rest
  : never;

function Select<Value, Multiple extends boolean | undefined = false>({
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
  ...props
}: SelectProps<Value, Multiple> & ShortcutOverlayRootProps) {
  const overlayProps = useOverlayOpenState<
    OpenChangeArgs<NonNullable<SelectProps<Value, Multiple>["onOpenChange"]>>
  >({
    open,
    defaultOpen,
    onOpenChange,
    blocksListShortcuts,
  });

  return (
    <SelectPrimitive
      {...(props as SelectProps<unknown, false>)}
      open={overlayProps.open}
      defaultOpen={overlayProps.defaultOpen}
      onOpenChange={
        overlayProps.onOpenChange as SelectProps<unknown, false>["onOpenChange"]
      }
    />
  );
}

function SelectLabel({
  className,
  ...props
}: React.ComponentProps<typeof SelectLabelPrimitive>) {
  return <SelectLabelPrimitive className={cn(className)} {...props} />;
}

function SelectGroup({
  className,
  ...props
}: React.ComponentProps<typeof SelectGroupPrimitive>) {
  return <SelectGroupPrimitive className={cn(className)} {...props} />;
}

function SelectGroupLabel({
  className,
  ...props
}: React.ComponentProps<typeof SelectGroupLabelPrimitive>) {
  return <SelectGroupLabelPrimitive className={cn(className)} {...props} />;
}

function SelectValue({
  className,
  ...props
}: React.ComponentProps<typeof SelectValuePrimitive>) {
  return (
    <SelectValuePrimitive
      className={cn("flex min-w-0 flex-1 truncate text-left", className)}
      {...props}
    />
  );
}

function SelectTrigger({
  className,
  size = "default",
  ...props
}: React.ComponentProps<typeof SelectTriggerPrimitive> & {
  size?: "sm" | "default";
}) {
  return (
    <SelectTriggerPrimitive
      data-size={size}
      className={cn("w-fit min-w-0", className)}
      {...props}
    />
  );
}

// Each item is ~2rem (32px) tall: text-sm line-height + py-1 padding.
const ITEM_HEIGHT_REM = 2;

function SelectContent({
  className,
  side = "bottom",
  sideOffset = 4,
  align = "center",
  alignOffset = 0,
  alignItemWithTrigger = true,
  visibleItems,
  style,
  ...props
}: React.ComponentProps<typeof SelectContentPrimitive> & {
  /** Number of items visible before the list scrolls. */
  visibleItems?: number;
}) {
  const container = useOverlayContainerElement();
  return (
    <SelectContentPrimitive
      side={side}
      sideOffset={sideOffset}
      align={align}
      alignOffset={alignOffset}
      alignItemWithTrigger={alignItemWithTrigger}
      container={container}
      className={cn("isolate z-50", className)}
      style={
        visibleItems !== undefined
          ? { maxHeight: `${visibleItems * ITEM_HEIGHT_REM}rem`, ...style }
          : style
      }
      {...props}
    />
  );
}

function SelectItem({
  className,
  children,
  label,
  ...props
}: React.ComponentProps<typeof SelectItemPrimitive>) {
  return (
    <SelectItemPrimitive
      label={label ?? textOf(children)}
      className={cn(className)}
      {...props}
    >
      {withIndicator(children, <SelectItemIndicatorPrimitive />)}
    </SelectItemPrimitive>
  );
}

function SelectSeparator({
  className,
  ...props
}: React.ComponentProps<typeof SelectSeparatorPrimitive>) {
  return <SelectSeparatorPrimitive className={cn(className)} {...props} />;
}

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

export {
  Select,
  SelectContent,
  SelectGroup,
  SelectGroupLabel,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
};
