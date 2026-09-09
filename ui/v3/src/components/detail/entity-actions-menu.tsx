import { Fragment, useState, type ComponentProps } from "react";
import { useIntl } from "react-intl";
import { EllipsisVertical, type LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Separator } from "@/components/ui/separator";
import { Spinner } from "@/components/ui/spinner";
import { useMobileDetailChrome } from "@/components/layout/mobile-detail-chrome";
import { cn } from "@/lib/utils";

export interface EntityAction {
  key: string;
  label: string;
  icon: LucideIcon;
  onSelect: () => void | Promise<void>;
  /** Evaluated when either presentation opens, including imperative player state. */
  disabled?: boolean | (() => boolean);
  destructive?: boolean;
}

interface EntityActionGroup {
  key: string;
  label: string;
  icon: LucideIcon;
  disabled?: boolean;
  actions: readonly EntityAction[];
}

export type EntityActionItem =
  | EntityAction
  | EntityActionGroup
  | { key: string; separator: true };

/** Desktop submenus become labelled groups of direct actions in the mobile drawer. */
export function EntityActionsMenu({
  items,
  busy = false,
}: {
  items: readonly EntityActionItem[];
  busy?: boolean;
}) {
  const intl = useIntl();
  const chrome = useMobileDetailChrome();
  const mobile = chrome?.mobile ?? false;
  const [open, setOpen] = useState(false);
  const label = intl.formatMessage({
    id: "operations",
    defaultMessage: "Operations",
  });

  function renderAction(action: EntityAction, groupDisabled = false) {
    const Icon = action.icon;
    const disabled =
      groupDisabled ||
      (typeof action.disabled === "function"
        ? action.disabled()
        : action.disabled);
    const onClick = () => {
      if (mobile) chrome?.setPanel(null);
      return action.onSelect();
    };
    return mobile ? (
      <Button
        key={action.key}
        variant={action.destructive ? "destructive" : "ghost"}
        disabled={disabled}
        onClick={onClick}
        className="h-auto min-h-11 w-full justify-start whitespace-normal text-left"
      >
        <Icon data-icon="inline-start" />
        {action.label}
      </Button>
    ) : (
      <DropdownMenuItem
        key={action.key}
        disabled={disabled}
        onClick={onClick}
        variant={action.destructive ? "destructive" : "default"}
      >
        <Icon />
        {action.label}
      </DropdownMenuItem>
    );
  }

  const content = items.map((item) => {
    if ("separator" in item)
      return mobile ? (
        <Separator key={item.key} className="my-1" />
      ) : (
        <DropdownMenuSeparator key={item.key} />
      );
    if (!("actions" in item)) return renderAction(item);
    const Icon = item.icon;
    return mobile ? (
      <Fragment key={item.key}>
        <p className="px-3 pt-3 pb-1 text-xs font-medium text-muted-foreground">
          {item.label}
        </p>
        {item.actions.map((action) => renderAction(action, item.disabled))}
      </Fragment>
    ) : (
      <DropdownMenuSub key={item.key}>
        <DropdownMenuSubTrigger disabled={item.disabled}>
          <Icon />
          {item.label}
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          <DropdownMenuGroup>
            {item.actions.map((action) => renderAction(action, item.disabled))}
          </DropdownMenuGroup>
        </DropdownMenuSubContent>
      </DropdownMenuSub>
    );
  });

  if (mobile)
    return <div className="flex w-full min-w-0 flex-col gap-1">{content}</div>;
  return (
    <DropdownMenu modal={false} open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        render={<Button variant="outline" size="sm" />}
        aria-label={label}
        title={label}
      >
        {busy ? <Spinner /> : <EllipsisVertical />}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuGroup>{content}</DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** Labelled drawer action on mobile; the existing toolbar button on desktop. */
export function EntityActionButton({
  label,
  icon: Icon,
  className,
  variant = "outline",
  size = "sm",
  onClick,
  ...props
}: Omit<ComponentProps<typeof Button>, "children"> & {
  label: string;
  icon: LucideIcon;
}) {
  const chrome = useMobileDetailChrome();
  const mobile = chrome?.mobile ?? false;
  return (
    <Button
      {...props}
      variant={mobile ? "ghost" : variant}
      size={mobile ? "default" : size}
      className={cn(className, mobile && "h-11 w-full justify-start")}
      aria-label={label}
      onClick={(event) => {
        if (mobile) chrome?.setPanel(null);
        onClick?.(event);
      }}
    >
      <Icon data-icon="inline-start" />
      <span>{label}</span>
    </Button>
  );
}
