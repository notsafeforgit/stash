/**
 * Shared building blocks for simple detail pages (no media player).
 *
 * Usage:
 *   <DetailBackBar title={entity.name} onBack={goBack} />
 *   <DetailPageState loading={loading} error={error} notFoundMessage="Not found" skeletonProps={...}>
 *     {entity && (...)}
 *   </DetailPageState>
 *   <DetailTabs tabs={tabs} activeTab={activeTab} onTabChange={setActiveTab} />
 *
 * `DetailTabs` lives in `./detail-tabs.tsx`; this file keeps the
 * non-tab building blocks (page state, back bars).
 */
import type React from "react";
import { DetailPageSkeleton } from "src/components/detail/detail-page-skeleton";
import { ChevronLeft } from "lucide-react";
import { Button } from "src/components/ui/button";

// ── DetailPageState ───────────────────────────────────────────────────────────

export interface DetailPageStateProps {
  loading: boolean;
  error?: { message: string };
  /** True when the entity was not found (e.g. `!studio`) */
  notFound: boolean;
  /** Message to show when not found or errored */
  notFoundMessage: string;
  skeletonProps?: React.ComponentProps<typeof DetailPageSkeleton>;
  children: React.ReactNode;
}

export function DetailPageState({
  loading,
  error,
  notFound,
  notFoundMessage,
  skeletonProps,
  children,
}: DetailPageStateProps) {
  // Only show the skeleton on the initial load (no data yet). Once the entity
  // has rendered, keep it visible through any subsequent `loading=true` blip
  // (refetch, partial-cache → network transition, strict-mode effect re-run)
  // so the page doesn't flash skeleton-then-content again.
  if (loading && notFound) return <DetailPageSkeleton {...skeletonProps} />;
  if (error || notFound) {
    return (
      <div className="p-4 text-destructive">
        {error?.message ?? notFoundMessage}
      </div>
    );
  }
  return <>{children}</>;
}

// ── DetailBackBar ─────────────────────────────────────────────────────────────

export interface DetailBackBarProps {
  title: string;
  onBack: () => void;
}

// Render this OUTSIDE the page's scroll container (as a sibling above it).
// Putting it inside as `position: sticky` causes iOS Safari's overlay
// scrollbar to paint over the bar, since the scrollbar runs the full height
// of the scroll container including over sticky children.
//
// Mobile-only: on desktop the back button moves into the sidebar (see
// `DetailSidebarBack`) so the redundant title bar doesn't take up vertical
// space above the two-column layout.
export function DetailBackBar({ title, onBack }: DetailBackBarProps) {
  return (
    <div className="md:hidden flex items-center gap-1 h-10 shrink-0 px-1 bg-background border-b border-border">
      <Button variant="ghost" size="sm" className="px-2" onClick={onBack}>
        <ChevronLeft size={18} />
      </Button>
      <span className="text-sm font-medium truncate">{title}</span>
    </div>
  );
}

// ── DetailSidebarBack ─────────────────────────────────────────────────────────

// Desktop-only back button + entity title row rendered as the first child of
// a detail page's `<aside>`. Pairs with `DetailBackBar` (mobile-only) so each
// viewport gets exactly one back affordance. Hosting the title here lets the
// image row drop its own `<h1>`, saving vertical space in the sidebar.
export function DetailSidebarBack({
  onBack,
  title,
}: {
  onBack: () => void;
  title?: string;
}) {
  return (
    <div className="hidden md:flex shrink-0 items-center gap-1 px-1 py-1 border-b border-border">
      <Button
        variant="ghost"
        size="sm"
        className="px-2 shrink-0"
        onClick={onBack}
      >
        <ChevronLeft size={18} />
      </Button>
      {title && (
        <h1 className="text-base font-semibold leading-tight truncate min-w-0">
          {title}
        </h1>
      )}
    </div>
  );
}
