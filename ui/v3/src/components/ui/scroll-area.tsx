import type * as React from "react";
import {
  ScrollArea as ScrollAreaPrimitive,
  ScrollAreaContent,
  ScrollAreaCorner,
  ScrollAreaScrollbar,
  ScrollAreaThumb,
  ScrollAreaViewport,
} from "@ui-sdk/react/scroll-area";

type ScrollAreaProps = Omit<
  React.ComponentProps<typeof ScrollAreaPrimitive>,
  "children"
> & {
  children?: React.ReactNode;
  /** Extra classes for the viewport — set height constraints (`h-64`,
   * `max-h-96`) here rather than on the root, so the viewport itself is
   * what clips and scrolls. */
  viewportClassName?: string;
  /** Also render a horizontal scrollbar (e.g. wide tables). */
  horizontal?: boolean;
};

function ScrollArea({
  viewportClassName,
  horizontal = false,
  children,
  ...props
}: ScrollAreaProps) {
  return (
    <ScrollAreaPrimitive {...props}>
      <ScrollAreaViewport className={viewportClassName}>
        <ScrollAreaContent>{children}</ScrollAreaContent>
      </ScrollAreaViewport>
      <ScrollBar />
      {horizontal && <ScrollBar orientation="horizontal" />}
      <ScrollAreaCorner />
    </ScrollAreaPrimitive>
  );
}

type ScrollBarProps = React.ComponentProps<typeof ScrollAreaScrollbar>;

function ScrollBar({ orientation = "vertical", ...props }: ScrollBarProps) {
  return (
    <ScrollAreaScrollbar orientation={orientation} {...props}>
      <ScrollAreaThumb />
    </ScrollAreaScrollbar>
  );
}

export { ScrollArea, ScrollBar };
