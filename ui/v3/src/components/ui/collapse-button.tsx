import {
  ChevronDown,
  ChevronRight,
  ChevronUp,
  type LucideIcon,
} from "lucide-react";
import type React from "react";
import { useEffect, useState } from "react";

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
  const [open, setOpen] = useState(props.open ?? false);

  function toggleOpen() {
    const nv = !open;
    setOpen(nv);
    props.onOpenChanged?.(nv);
  }

  useEffect(() => {
    if (props.open !== undefined) {
      setOpen(props.open);
    }
  }, [props.open]);

  return (
    <div className={props.className}>
      <div className="collapse-header">
        <button
          type="button"
          onClick={() => toggleOpen()}
          className="minimal collapse-button"
        >
          {open ? (
            <ChevronDown className="icon" size={16} />
          ) : (
            <ChevronRight className="icon" size={16} />
          )}
          <span>{props.text}</span>
        </button>
      </div>
      {props.outsideCollapse}
      {open && <div>{props.children}</div>}
    </div>
  );
};

export const ExpandCollapseButton: React.FC<{
  collapsed: boolean;
  setCollapsed: (collapsed: boolean) => void;
  collapsedIcon?: LucideIcon;
  notCollapsedIcon?: LucideIcon;
}> = ({ collapsedIcon, notCollapsedIcon, collapsed, setCollapsed }) => {
  const ButtonIcon = collapsed
    ? (collapsedIcon ?? ChevronDown)
    : (notCollapsedIcon ?? ChevronUp);

  return (
    <span className="detail-expand-collapse">
      <button
        type="button"
        className="minimal expand-collapse"
        onClick={(e) => {
          setCollapsed(!collapsed);
          e.stopPropagation();
        }}
      >
        <ButtonIcon className="icon" size={16} />
      </button>
    </span>
  );
};
