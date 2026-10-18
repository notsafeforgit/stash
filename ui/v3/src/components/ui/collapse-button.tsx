import {
  ChevronDown,
  ChevronRight,
  ChevronUp,
  type LucideIcon,
} from "lucide-react";
import type React from "react";
import { useId, useState } from "react";
import { useIntl } from "react-intl";
import { Button } from "@/components/ui/button";

interface Props {
  className?: string;
  text: React.ReactNode;
  outsideCollapse?: React.ReactNode;
  onOpenChanged?: (o: boolean) => void;
  open?: boolean;
}

export const CollapseButton: React.FC<React.PropsWithChildren<Props>> = (
  props: React.PropsWithChildren<Props>,
) => {
  const [internalOpen, setOpen] = useState(false);
  const open = props.open ?? internalOpen;
  const panelId = useId();

  function toggleOpen() {
    const nv = !open;
    if (props.open === undefined) setOpen(nv);
    props.onOpenChanged?.(nv);
  }

  return (
    <div className={props.className}>
      <div>
        <Button
          type="button"
          variant="ghost"
          aria-expanded={open}
          aria-controls={panelId}
          onClick={() => toggleOpen()}
        >
          {open ? (
            <ChevronDown data-icon="inline-start" />
          ) : (
            <ChevronRight data-icon="inline-start" />
          )}
          <span>{props.text}</span>
        </Button>
      </div>
      {props.outsideCollapse}
      <div id={panelId} hidden={!open}>
        {open && props.children}
      </div>
    </div>
  );
};

export const ExpandCollapseButton: React.FC<{
  label: string;
  collapsed: boolean;
  setCollapsed: (collapsed: boolean) => void;
  collapsedIcon?: LucideIcon;
  notCollapsedIcon?: LucideIcon;
}> = ({ label, collapsedIcon, notCollapsedIcon, collapsed, setCollapsed }) => {
  const intl = useIntl();
  const action = intl.formatMessage(
    { id: collapsed ? "actions.expand_item" : "actions.collapse_item" },
    { name: label },
  );
  const ButtonIcon = collapsed
    ? (collapsedIcon ?? ChevronDown)
    : (notCollapsedIcon ?? ChevronUp);

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      aria-label={action}
      title={action}
      aria-expanded={!collapsed}
      onClick={(e) => {
        setCollapsed(!collapsed);
        e.stopPropagation();
      }}
    >
      <ButtonIcon />
    </Button>
  );
};
