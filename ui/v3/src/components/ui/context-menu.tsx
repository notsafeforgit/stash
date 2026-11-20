"use client";

import { type ComponentProps, type ReactNode, useId } from "react";
import {
  Menu,
  MenuContent,
  MenuContextTrigger,
  MenuCheckboxItem,
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
  type MenuProps,
} from "@ui-sdk/react/menu";
import { ChevronRightIcon } from "lucide-react";

import { cn } from "@/lib/utils";
import {
  type ShortcutOverlayRootProps,
  useOverlayOpenState,
} from "@/components/shortcut-provider";
import { useOverlayContainerElement } from "./overlay-container";

// Controllers open a menu from outside its trigger, for example anchored to the
// element that a long press or the context-menu key targets.
export {
  createMenuController as createContextMenuController,
  type MenuController as ContextMenuController,
} from "@ui-sdk/react/menu";

type ContextMenuProps = MenuProps & ShortcutOverlayRootProps;

// Modal by default, as the Base UI primitives were: scroll lock, focus trap and
// an inert page. A call site that needs a non-modal menu passes modal={false}.
function ContextMenu({
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
  modal = true,
  ...props
}: ContextMenuProps) {
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
function ContextMenuPortal({ children }: { children?: ReactNode }) {
  return <>{children}</>;
}

function ContextMenuTrigger({
  className,
  ...props
}: ComponentProps<typeof MenuContextTrigger>) {
  return (
    <MenuContextTrigger className={cn("select-none", className)} {...props} />
  );
}

type ContextMenuContentProps = Omit<
  ComponentProps<typeof MenuContent>,
  "children" | "className"
> & {
  children?: ReactNode;
  className?: string;
};

function ContextMenuContent({
  className,
  align = "start",
  alignOffset = 4,
  side = "right",
  sideOffset = 0,
  container,
  ...props
}: ContextMenuContentProps) {
  const overlay = useOverlayContainerElement();
  return (
    <MenuContent
      container={container ?? overlay}
      align={align}
      alignOffset={alignOffset}
      side={side}
      sideOffset={sideOffset}
      className={cn("z-50", className)}
      {...props}
    />
  );
}

function ContextMenuGroup(props: ComponentProps<typeof MenuGroup>) {
  return <MenuGroup {...props} />;
}

function ContextMenuLabel({
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

function ContextMenuItem({
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

function ContextMenuSub(props: ComponentProps<typeof MenuSub>) {
  return <MenuSub {...props} />;
}

function ContextMenuSubTrigger({
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

const ContextMenuSubContent = MenuSubContent;

function ContextMenuCheckboxItem({
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

const ContextMenuRadioGroup = MenuRadioGroup;

function ContextMenuRadioItem({
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

const ContextMenuSeparator = MenuSeparator;

const ContextMenuShortcut = MenuShortcut;

export {
  ContextMenu,
  ContextMenuTrigger,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuCheckboxItem,
  ContextMenuRadioItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuGroup,
  ContextMenuPortal,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuRadioGroup,
};
