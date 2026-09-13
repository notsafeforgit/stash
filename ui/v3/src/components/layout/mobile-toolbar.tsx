import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

/** Keep 44px targets, with comfortable edge margins wherever space permits. */
export function MobileToolbarRow({
  className,
  ...props
}: ComponentProps<"div">) {
  return (
    <div
      data-mobile-toolbar-row
      className={cn(
        "mobile-toolbar-row flex h-14 items-center gap-[var(--mobile-toolbar-gap)]",
        className,
      )}
      {...props}
    />
  );
}
