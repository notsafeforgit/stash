import { useEffect, useState } from "react";
import { FormattedMessage } from "react-intl";
import { Funnel } from "lucide-react";
import type { FilterMode } from "src/core/generated-graphql";
import type { ListFilterModel } from "src/models/list-filter/filter";
import { FilterBuilder } from "src/components/filters/filter-builder";
import { Button } from "src/components/ui/button";
import {
  BottomSheet,
  BottomSheetHeader,
  BottomSheetTitle,
} from "src/components/ui/bottom-sheet";
import { cn } from "src/lib/utils";
import { useMediaQuery } from "src/utils/screen";

const MOBILE_QUERY = "only screen and (max-width: 767px)";

/**
 * Standalone FilterBuilder wrapper for tool pages (duplicate checkers) that
 * aren't built on the entity-list infrastructure. Holds the saved-filter
 * name locally; the owning page holds the `ListFilterModel` and syncs its
 * encoded AST (`fa`) to the URL.
 */
export function ToolFilterSidebar({
  mode,
  open,
  filter,
  setFilter,
  onOpenChange,
  applyMode = "live",
}: {
  mode: FilterMode;
  open: boolean;
  filter: ListFilterModel;
  setFilter: (filter: ListFilterModel) => void;
  onOpenChange?: (open: boolean) => void;
  applyMode?: "live" | "manual";
}) {
  const isMobileSidebar = useMediaQuery(MOBILE_QUERY);
  const [currentSavedFilterName, setCurrentSavedFilterName] =
    useState<string>();
  const [draftFilter, setDraftFilter] = useState(() => filter.clone());
  const [hasPendingChanges, setHasPendingChanges] = useState(false);

  useEffect(() => {
    if (applyMode === "manual") {
      setDraftFilter(filter.clone());
      setHasPendingChanges(false);
    }
  }, [applyMode, filter]);

  const displayedFilter = applyMode === "manual" ? draftFilter : filter;
  const setDisplayedFilter =
    applyMode === "manual"
      ? (next: ListFilterModel) => {
          setDraftFilter(next);
          setHasPendingChanges(true);
        }
      : setFilter;

  // FilterBuilder expects the root to be a group node; top-level filter ASTs
  // are always groups in practice (the URL decoder wraps bare conditions in a group).
  const root =
    displayedFilter.filterAst?.kind === "group"
      ? displayedFilter.filterAst
      : undefined;

  const sidebarContent = (
    <>
      {applyMode === "manual" && (
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-card p-3">
          <span className="text-sm text-muted-foreground">
            {hasPendingChanges ? (
              <FormattedMessage
                id="search_filter.pending_changes"
                defaultMessage="Pending changes"
              />
            ) : (
              <FormattedMessage
                id="search_filter.applied"
                defaultMessage="Applied"
              />
            )}
          </span>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!hasPendingChanges}
              onClick={() => {
                setDraftFilter(filter.clone());
                setHasPendingChanges(false);
              }}
            >
              <FormattedMessage id="actions.reset" defaultMessage="Reset" />
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={!hasPendingChanges}
              onClick={() => {
                setFilter(draftFilter.clone());
                setHasPendingChanges(false);
              }}
            >
              <FormattedMessage id="actions.apply" defaultMessage="Apply" />
            </Button>
          </div>
        </div>
      )}
      <FilterBuilder
        mode={mode}
        filter={displayedFilter}
        setFilter={setDisplayedFilter}
        root={root}
        onChange={(node) => {
          const next = displayedFilter.clone();
          next.filterAst = node;
          setDisplayedFilter(next);
        }}
        isOpen={open}
        currentSavedFilterName={currentSavedFilterName}
        onCurrentSavedFilterChange={(next) =>
          setCurrentSavedFilterName(next?.name)
        }
      />
    </>
  );

  if (isMobileSidebar) {
    return (
      <BottomSheet
        open={open}
        onOpenChange={(nextOpen) => onOpenChange?.(nextOpen)}
        className="h-[88svh] max-h-[88svh]"
      >
        <BottomSheetHeader className="shrink-0 border-b border-border px-4! py-3!">
          <BottomSheetTitle className="flex items-center gap-2 text-base">
            <Funnel className="size-4" />
            <FormattedMessage
              id="search_filter.edit_filter"
              defaultMessage="Filters"
            />
            {displayedFilter.count() > 0 && (
              <span className="rounded-full bg-primary px-[0.4rem] py-[0.15rem] text-[0.7rem] font-semibold leading-none text-primary-foreground">
                {displayedFilter.count()}
              </span>
            )}
          </BottomSheetTitle>
        </BottomSheetHeader>
        <div className="flex-1 overflow-y-auto p-3">{sidebarContent}</div>
      </BottomSheet>
    );
  }

  return (
    <aside
      aria-hidden={!open}
      inert={!open ? true : undefined}
      className={cn(
        "filter-sidebar-panel w-full shrink-0 overflow-y-auto overflow-x-hidden transition-[max-height,width,margin] duration-200 ease-in-out lg:max-h-full",
        open
          ? "mb-4 max-h-[80vh] lg:mb-0 lg:mr-4 lg:w-[320px]"
          : "mb-0 max-h-0 lg:mr-0 lg:w-0",
      )}
    >
      <div className="w-full lg:w-[320px]">{sidebarContent}</div>
    </aside>
  );
}
