import { useRef, useState, type Dispatch, type SetStateAction } from "react";
import { flushSync } from "react-dom";
import { useIntl } from "react-intl";
import {
  ChevronUp,
  Ellipsis,
  Funnel,
  ListChecks,
  Menu,
  Search,
  Settings2,
  Tags,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { MobileNavSheet } from "src/components/layout/mobile-nav-sheet";
import {
  MobileDetailChromePortal,
  useMobileDetailChrome,
  useMobileDetailInteraction,
} from "@/components/layout/mobile-detail-chrome";
import { useVisualViewportBottomInset } from "@/hooks/use-visual-viewport-bottom-inset";
import { cn } from "@/lib/utils";
import type { ListFilterModel } from "@/models/list-filter/filter";
import { SearchInput } from "./search-input";
import { MobileListPagePicker } from "./mobile-list-pagination";

export interface MobileListControlsProps {
  filter: ListFilterModel;
  setFilter: Dispatch<SetStateAction<ListFilterModel>>;
  totalCount: number;
  activeFilterCount: number;
  selecting: boolean;
  hasSelection: boolean;
  selectedCount: number;
  onSelectAll: () => void;
  onSelectNone: () => void;
  onTaggerMode?: () => void;
  openFilterSidebar: () => void;
  onSearch: (value: string) => void;
  onViewOptions: () => void;
}

/** One row, with explicit replacement modes instead of stacked toolbars. */
export function MobileListControls({
  filter,
  setFilter,
  totalCount,
  activeFilterCount,
  selecting,
  hasSelection,
  selectedCount,
  onSelectAll,
  onSelectNone,
  onTaggerMode,
  openFilterSidebar,
  onSearch,
  onViewOptions,
}: MobileListControlsProps) {
  const intl = useIntl();
  const chrome = useMobileDetailChrome();
  const hosted = chrome?.mobile ?? false;
  const [searchOpen, setSearchOpen] = useState(false);
  const [navOpen, setNavOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [pagesOpen, setPagesOpen] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);
  const searchButtonRef = useRef<HTMLButtonElement>(null);
  const { ref, bottomInset } = useVisualViewportBottomInset<HTMLDivElement>();
  const mode =
    selecting || hasSelection ? "selection" : searchOpen ? "search" : null;
  useMobileDetailInteraction(mode);
  const pageCount = Math.ceil(totalCount / filter.itemsPerPage);
  const moreLabel = intl.formatMessage({
    id: "actions.more",
    defaultMessage: "More",
  });

  function runAction(action: () => void) {
    setMoreOpen(false);
    chrome?.setPanel(null);
    action();
  }

  const pagePicker = (
    <MobileListPagePicker
      currentPage={filter.currentPage}
      itemsPerPage={filter.itemsPerPage}
      totalItems={totalCount}
      onChangePage={(page) => {
        setFilter(filter.changePage(page));
        setPagesOpen(false);
        chrome?.setPanel(null);
      }}
    />
  );
  const actions = (
    <div className="flex flex-col gap-1">
      {hosted && <Separator className="my-1" />}
      <Button
        variant="ghost"
        className="h-11 justify-start"
        onClick={() => runAction(openFilterSidebar)}
      >
        <Funnel data-icon="inline-start" />
        {intl.formatMessage({
          id: "search_filter.edit_filter",
          defaultMessage: "Filters",
        })}
        {activeFilterCount > 0 && (
          <Badge variant="secondary" className="ml-auto">
            {activeFilterCount}
          </Badge>
        )}
      </Button>
      <Button
        variant="ghost"
        className="h-11 justify-start"
        onClick={() => runAction(onViewOptions)}
      >
        <Settings2 data-icon="inline-start" />
        {intl.formatMessage({
          id: "view_options",
          defaultMessage: "View options",
        })}
      </Button>
      {onTaggerMode && (
        <Button
          variant="ghost"
          className="h-11 justify-start"
          onClick={() => runAction(onTaggerMode)}
        >
          <Tags data-icon="inline-start" />
          {intl.formatMessage({
            id: "actions.tagger",
            defaultMessage: "Tagger",
          })}
        </Button>
      )}
      {hosted && (
        <Button
          variant="ghost"
          className="h-11 justify-start"
          onClick={() => runAction(() => setNavOpen(true))}
        >
          <Menu data-icon="inline-start" />
          {intl.formatMessage({
            id: "navigation",
            defaultMessage: "Navigation",
          })}
        </Button>
      )}
    </div>
  );

  return (
    <>
      <MobileNavSheet open={navOpen} onOpenChange={setNavOpen} />
      {hosted && (
        <>
          <MobileDetailChromePortal slot="pagination">
            {pagePicker}
          </MobileDetailChromePortal>
          <MobileDetailChromePortal slot="list-actions">
            {actions}
          </MobileDetailChromePortal>
        </>
      )}
      <div
        ref={hosted ? undefined : ref}
        data-mobile-list-mode={mode ?? "browse"}
        className={cn(
          !hosted &&
            "shrink-0 border-t bg-background pb-[env(safe-area-inset-bottom,0px)]",
        )}
        style={
          !hosted && bottomInset > 0
            ? { transform: `translateY(-${bottomInset}px)` }
            : undefined
        }
      >
        <div className={cn("flex items-center gap-2", !hosted && "h-14 px-3")}>
          {mode === "selection" ? (
            <>
              <span className="min-w-0 flex-1 truncate text-sm tabular-nums">
                {intl.formatMessage(
                  {
                    id: "list.selected_count",
                    defaultMessage: "{count} selected",
                  },
                  { count: selectedCount },
                )}
              </span>
              <Button
                variant="ghost"
                size="icon-lg"
                className="size-11 shrink-0"
                onClick={onSelectAll}
                aria-label={intl.formatMessage({
                  id: "actions.select_all_on_page",
                })}
              >
                <ListChecks />
              </Button>
              <Button
                variant="ghost"
                size="icon-lg"
                className="size-11 shrink-0"
                onClick={onSelectNone}
                aria-label={intl.formatMessage({ id: "actions.select_none" })}
              >
                <X />
              </Button>
            </>
          ) : mode === "search" ? (
            <>
              <SearchInput
                inputRef={searchRef}
                mobile
                value={filter.searchTerm}
                onChange={onSearch}
                className="min-w-0 flex-1"
              />
              <Button
                variant="ghost"
                size="icon-lg"
                className="size-11 shrink-0"
                onClick={() => {
                  searchRef.current?.blur();
                  flushSync(() => setSearchOpen(false));
                  searchButtonRef.current?.focus({ preventScroll: true });
                }}
                aria-label={intl.formatMessage({
                  id: "actions.close_search",
                  defaultMessage: "Close search",
                })}
              >
                <X />
              </Button>
            </>
          ) : (
            <>
              {!hosted && (
                <>
                  <Button
                    variant="ghost"
                    size="icon-lg"
                    className="size-11 shrink-0"
                    onClick={() => setNavOpen(true)}
                    aria-label={intl.formatMessage({
                      id: "navigation",
                      defaultMessage: "Navigation",
                    })}
                  >
                    <Menu />
                  </Button>
                  <div className="min-w-0 flex-1">
                    {pageCount > 1 ? (
                      <Popover open={pagesOpen} onOpenChange={setPagesOpen}>
                        <PopoverTrigger
                          render={
                            <Button
                              variant="ghost"
                              className="h-11 w-full justify-between px-2"
                              aria-label={intl.formatMessage({
                                id: "pagination.pages",
                                defaultMessage: "Pages",
                              })}
                            />
                          }
                        >
                          <span className="truncate tabular-nums">
                            {intl.formatNumber(filter.currentPage)} /{" "}
                            {intl.formatNumber(pageCount)}
                          </span>
                          <ChevronUp data-icon="inline-end" />
                        </PopoverTrigger>
                        <PopoverContent
                          side="top"
                          className="w-80 max-w-[calc(100vw-1.5rem)]"
                        >
                          <PopoverTitle>
                            {intl.formatMessage({
                              id: "pagination.pages",
                              defaultMessage: "Pages",
                            })}
                          </PopoverTitle>
                          {pagePicker}
                        </PopoverContent>
                      </Popover>
                    ) : (
                      <span className="text-sm text-muted-foreground tabular-nums">
                        {intl.formatNumber(totalCount)}
                      </span>
                    )}
                  </div>
                </>
              )}
              <Button
                ref={searchButtonRef}
                variant={filter.searchTerm ? "secondary" : "ghost"}
                size="icon-lg"
                className="size-11 shrink-0"
                onClick={() => {
                  // Mount and focus in the touch handler so iOS opens its keyboard.
                  flushSync(() => setSearchOpen(true));
                  searchRef.current?.focus({ preventScroll: true });
                }}
                aria-label={intl.formatMessage({
                  id: "search",
                  defaultMessage: "Search…",
                })}
              >
                <Search />
              </Button>
              {!hosted && (
                <Popover open={moreOpen} onOpenChange={setMoreOpen}>
                  <PopoverTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-lg"
                        className="size-11 shrink-0"
                        aria-label={moreLabel}
                      />
                    }
                  >
                    <Ellipsis />
                  </PopoverTrigger>
                  <PopoverContent side="top" align="end">
                    <PopoverTitle>{moreLabel}</PopoverTitle>
                    {actions}
                  </PopoverContent>
                </Popover>
              )}
            </>
          )}
        </div>
      </div>
    </>
  );
}
