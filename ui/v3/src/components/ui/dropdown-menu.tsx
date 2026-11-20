"use client";

import { type ComponentProps, type ReactNode, useId } from "react";
import {
  Menu,
  type MenuFinalFocus,
  type MenuProps,
  MenuCheckboxItem,
  MenuContent,
  MenuGroup,
  MenuGroupLabel,
  MenuItem,
  MenuItemIcon,
  MenuItemIndicator,
  MenuRadioGroup,
  MenuRadioItem,
  MenuSeparator,
  MenuShortcut,
  MenuSub,
  MenuSubContent,
  MenuSubTrigger,
  MenuTrigger,
} from "@ui-sdk/react/menu";
import { ChevronRightIcon } from "lucide-react";

import { cn } from "@/lib/utils";
import {
  type ShortcutOverlayRootProps,
  useOverlayOpenState,
} from "@/components/shortcut-provider";
import { containEscape, useOverlayContainerElement } from "./overlay-container";

type DropdownMenuProps = MenuProps & ShortcutOverlayRootProps;

// Modal by default, as the Base UI primitives were: scroll lock, focus trap and
// an inert page. A call site that needs a non-modal menu passes modal={false}.
function DropdownMenu({
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
  modal = true,
  ...props
}: DropdownMenuProps) {
  const overlayProps = useOverlayOpenState<
    Parameters<NonNullable<MenuProps["onOpenChange"]>> extends [
      boolean,
      ...infer Rest,
    ]
      ? Rest
      : never
  >({
    open,
    defaultOpen,
    onOpenChange,
    blocksListShortcuts,
  });

  return <Menu modal={modal} {...overlayProps} {...props} />;
}

// ui-sdk renders popups in the top layer, so there is no portal part. Kept so
// existing imports still resolve; its children render where they are placed.
function DropdownMenuPortal({ children }: { children?: ReactNode }) {
  return <>{children}</>;
}

const DropdownMenuTrigger = MenuTrigger;

type DropdownMenuContentProps = Omit<
  ComponentProps<typeof MenuContent>,
  "children" | "className" | "finalFocus"
> & {
  children?: ReactNode;
  className?: string;
  /** Classes for the same element; kept for callers that set z-index here. */
  positionerClassName?: string;
  finalFocus?: MenuFinalFocus | false;
};

function DropdownMenuContent({
  align = "start",
  side = "bottom",
  sideOffset = 4,
  alignOffset,
  className,
  positionerClassName,
  finalFocus,
  container,
  onKeyDown,
  ...props
}: DropdownMenuContentProps) {
  const overlay = useOverlayContainerElement();
  return (
    <MenuContent
      container={container ?? overlay}
      onKeyDown={containEscape(onKeyDown)}
      side={side}
      align={align}
      alignOffset={alignOffset}
      sideOffset={sideOffset}
      finalFocus={finalFocus === false ? "none" : finalFocus}
      className={cn(
        "z-50 w-(--anchor-width) overflow-x-hidden",
        positionerClassName,
        className,
      )}
      {...props}
    />
  );
}

function DropdownMenuGroup(props: ComponentProps<typeof MenuGroup>) {
  return <MenuGroup {...props} />;
}

function DropdownMenuLabel({
  className,
  inset,
  ...props
}: ComponentProps<typeof MenuGroupLabel> & { inset?: boolean }) {
  return <MenuGroupLabel data-inset={inset} className={className} {...props} />;
}

type ItemClickProps = {
  /** Runs on a pointer click and on Enter or Space, like a click handler. */
  onClick?: (event: Event) => void;
  inset?: boolean;
  variant?: "default" | "destructive";
};

function DropdownMenuItem({
  className,
  inset,
  variant = "default",
  onClick,
  onSelect,
  ...props
}: Omit<ComponentProps<typeof MenuItem>, "onClick"> & ItemClickProps) {
  return (
    <MenuItem
      data-inset={inset}
      destructive={variant === "destructive"}
      className={className}
      onSelect={
        onClick
          ? (event) => {
              onClick(event);
            }
          : onSelect
      }
      {...props}
    />
  );
}

function DropdownMenuSub(props: ComponentProps<typeof MenuSub>) {
  return <MenuSub {...props} />;
}

function DropdownMenuSubTrigger({
  className,
  inset,
  children,
  ...props
}: Omit<ComponentProps<typeof MenuSubTrigger>, "children"> & {
  children?: ReactNode;
  inset?: boolean;
}) {
  return (
    <MenuSubTrigger data-inset={inset} className={className} {...props}>
      {children}
      <MenuItemIcon mirror className="ml-auto">
        <ChevronRightIcon />
      </MenuItemIcon>
    </MenuSubTrigger>
  );
}

const DropdownMenuSubContent = MenuSubContent;

function DropdownMenuCheckboxItem({
  className,
  children,
  value,
  inset,
  ...props
}: Omit<ComponentProps<typeof MenuCheckboxItem>, "children" | "value"> & {
  children?: ReactNode;
  value?: string;
  inset?: boolean;
}) {
  const fallbackValue = useId();
  return (
    <MenuCheckboxItem
      value={value ?? fallbackValue}
      data-inset={inset}
      className={className}
      {...props}
    >
      <MenuItemIndicator />
      {children}
    </MenuCheckboxItem>
  );
}

const DropdownMenuRadioGroup = MenuRadioGroup;

function DropdownMenuRadioItem({
  className,
  children,
  inset,
  ...props
}: Omit<ComponentProps<typeof MenuRadioItem>, "children"> & {
  children?: ReactNode;
  inset?: boolean;
}) {
  return (
    <MenuRadioItem data-inset={inset} className={className} {...props}>
      <MenuItemIndicator />
      {children}
    </MenuRadioItem>
  );
}

const DropdownMenuSeparator = MenuSeparator;

const DropdownMenuShortcut = MenuShortcut;

export {
  DropdownMenu,
  DropdownMenuPortal,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuCheckboxItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
};
