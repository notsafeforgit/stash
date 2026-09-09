/**
 * Shared building blocks for simple detail pages (no media player).
 *
 * Usage:
 *   <DetailPageState loading={loading} error={error} notFoundMessage="Not found" skeletonProps={...}>
 *     {entity && (...)}
 *   </DetailPageState>
 *   <DetailTabs tabs={tabs} activeTab={activeTab} onTabChange={setActiveTab} />
 *
 * `DetailTabs` lives in `./detail-tabs.tsx`; this file keeps the
 * non-tab building blocks (page state, desktop navigation).
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

// Desktop sidebar navigation; CollectionDetailLayout owns mobile navigation.
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
