import { cn } from "src/lib/utils";
import { TabsList, TabsTrigger } from "src/components/ui/tabs";
import { useIntl } from "react-intl";

export interface DetailTabStripItem {
  id: string;
  label: string;
}

export interface DetailTabStripProps {
  tabs: readonly DetailTabStripItem[];
  mobile?: boolean;
  onTabClick?: (id: string) => void;
  className?: string;
}

/**
 * The styled `<TabsList>` used by both detail-page layouts. Triggers
 * keep `flex-1` for equal share when space allows but stop shrinking
 * below their content width (`min-w-fit`), and the strip itself becomes
 * horizontally scrollable (`overflow-x-auto`) when 5+ labels exceed the
 * column width. This is the only place the scroll behaviour lives.
 */
export function DetailTabStrip({
  tabs,
  mobile = false,
  onTabClick,
  className,
}: DetailTabStripProps) {
  const intl = useIntl();
  return (
    // `w-full` overrides the shadcn TabsList's default `w-fit`: without
    // it the strip sizes to its content's intrinsic width, which means
    // `overflow-x-auto` has no constraint to overflow against and the
    // tabs spill out of the parent (clipped by the sidebar's
    // `overflow-hidden`). Pinning to the container width is what makes
    // the horizontal scroll engage when 5+ labels exceed the column.
    <TabsList
      aria-label={intl.formatMessage({
        id: "accessibility.detail_sections",
        defaultMessage: "Detail sections",
      })}
      className={cn(
        "w-full shrink-0 overflow-x-auto overflow-y-hidden",
        // Pack tabs shoulder-to-shoulder before overflowing:
        //  - `gap-0` overrides the line-variant's 4 px inter-trigger gap
        //  - `[&>*]:px-1` shrinks each trigger's horizontal padding
        //    from the default 6 px to 4 px
        //  - `[&>*]:min-w-fit` stops triggers shrinking below their
        //    label width
        // 5 detail tabs in the 320 px scene sidebar fit at this density
        // without scrolling; 6+ start scrolling, which is the right
        // breakpoint for a 320 px column.
        "gap-0 [&>*]:px-1 [&>*]:min-w-fit",
        // Hide the scrollbar (Firefox + WebKit) — the strip still
        // scrolls via swipe / shift-wheel / drag, just without a chunky
        // bar eating vertical space in a 32 px-tall element.
        "[scrollbar-width:none] [&::-webkit-scrollbar]:hidden",
        // Lock touch input to horizontal panning only and stop iOS's
        // rubber-band on vertical drag (`touch-pan-x`), and don't
        // forward leftover scroll momentum to the page beneath
        // (`overscroll-contain`).
        "touch-pan-x overscroll-contain",
        mobile &&
          "h-12 group-data-horizontal/tabs:h-12 justify-start p-0 [&>*]:px-3",
        className,
      )}
    >
      {tabs.map((t) => (
        <TabsTrigger
          key={t.id}
          value={t.id}
          onClick={() => onTabClick?.(t.id)}
          className={
            mobile
              ? "h-12 after:inset-x-0 after:bottom-0 group-data-horizontal/tabs:after:bottom-0 after:h-0.5"
              : undefined
          }
        >
          {t.label}
        </TabsTrigger>
      ))}
    </TabsList>
  );
}
