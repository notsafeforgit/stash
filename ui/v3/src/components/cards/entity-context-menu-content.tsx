import type { ComponentProps } from "react";
import {
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuLabel,
  ContextMenuSeparator,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";

/** Keep the full entity name readable when a card title is truncated. */
export function EntityContextMenuContent({
  title,
  children,
  className,
  ...props
}: ComponentProps<typeof ContextMenuContent> & { title: string }) {
  return (
    // Without a width the menu shrinks into the space right of the press point
    // instead of sliding into view.
    <ContextMenuContent
      className={cn("w-max max-w-[min(24rem,calc(100vw-2rem))]", className)}
      {...props}
    >
      <ContextMenuGroup>
        {title && (
          <>
            <ContextMenuLabel className="whitespace-normal [overflow-wrap:anywhere]">
              {title}
            </ContextMenuLabel>
            <ContextMenuSeparator />
          </>
        )}
        {children}
      </ContextMenuGroup>
    </ContextMenuContent>
  );
}
