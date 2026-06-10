import { cn } from "src/lib/utils";
import { Skeleton } from "src/components/ui/skeleton";

interface DetailPageSkeletonProps {
  /** Tailwind aspect-ratio class for the image, e.g. "aspect-square" or "aspect-[2/3]" */
  imageAspect?: string;
}

// Chrome-only skeleton (image, title, toolbar, meta rows, tab bar).
// Mirrors the live two-column detail layout: on desktop the entity-details
// portion sits in a fixed-width sidebar on the left and the tab bar lives in
// the right column. On mobile everything stacks vertically.
//
// Detail pages whose body is an embedded entity list (galleries, performers,
// tags, studios, groups) intentionally don't include a card-grid skeleton
// here — once the entity itself loads the inner `EntityListPage` renders
// its own card skeletons for any pending list data, so a second card
// skeleton at the page level just makes the cold-load feel like "the
// whole page is loading" instead of "only the list is loading".
export function DetailPageSkeleton({
  imageAspect = "aspect-square",
}: DetailPageSkeletonProps) {
  // Tall (portrait) aspects get a centered, 3/5-width clamp on mobile so
  // they don't dominate the viewport. Square / landscape aspects are
  // shorter for the same width and look fine taking the full pane.
  const isPortrait = imageAspect.includes("aspect-[2/3]");
  const mobileWidthClass = isPortrait ? "max-md:w-3/5 max-md:self-center" : "";
  return (
    <div className="md:h-full md:flex md:flex-row">
      {/* Left sidebar: back button + title (desktop) / image + toolbar / meta rows */}
      <aside className="md:w-72 lg:w-80 md:shrink-0 md:flex md:flex-col md:border-r md:border-border">
        <div className="hidden md:flex shrink-0 items-center gap-1 px-1 py-1 border-b border-border">
          <Skeleton className="h-7 w-9 rounded-md shrink-0" />
          <Skeleton className="h-4 w-2/3" />
        </div>
        <div className="md:flex-1 md:min-h-0 md:overflow-y-auto">
          <div className="flex flex-col items-stretch gap-3 p-3">
            <Skeleton
              className={cn(
                "shrink-0 rounded md:w-full",
                mobileWidthClass || "w-full",
                imageAspect,
              )}
            />
            <div className="flex flex-col gap-2 min-w-0 md:order-first">
              <div className="flex gap-2">
                <Skeleton className="h-7 w-16 rounded-md" />
                <Skeleton className="h-7 w-16 rounded-md" />
              </div>
            </div>
          </div>
          <div className="px-3 pb-3 flex flex-col gap-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex gap-3">
                <Skeleton className="h-3.5 w-20 shrink-0" />
                <Skeleton className="h-3.5 w-32" />
              </div>
            ))}
          </div>
        </div>
      </aside>

      {/* Right column: tab bar */}
      <div className="md:flex-1 md:min-w-0">
        <div className="flex gap-1 px-3 py-1.5 border-b border-border">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-7 w-20 rounded-md" />
          ))}
        </div>
      </div>
    </div>
  );
}
