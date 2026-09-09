import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

/** Seven 44px targets fit at 320px; wider bars spend spare space on gaps. */
export function MobileToolbarRow({
  className,
  ...props
}: ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "flex h-14 items-center gap-[var(--mobile-toolbar-gap)] px-1.5 [--mobile-toolbar-gap:clamp(0px,calc((100cqw_-_320px)/6),4px)]",
        className,
      )}
      {...props}
    />
  );
}
