import { useState, type Dispatch, type SetStateAction } from "react";
import { useIntl } from "react-intl";
import {
  ChevronUp,
  Funnel,
  ListChecks,
  Menu,
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
import { MobileNavSheet } from "@/components/layout/mobile-nav-sheet";
import { MobileToolbarRow } from "@/components/layout/mobile-toolbar";
import { MobileSearchRow } from "@/components/layout/mobile-search-row";
import {
  MobileDetailChromePortal,
  useMobileDetailChrome,
  useMobileDetailInteraction,
} from "@/components/layout/mobile-detail-chrome";
import { useMobileKeyboardLayout } from "@/hooks/use-mobile-keyboard-layout";
import { useMobileSearch } from "@/hooks/use-mobile-search";
import { cn } from "@/lib/utils";
import type { ListFilterModel } from "@/models/list-filter/filter";
import { SearchInput } from "./search-input";
import { MobileListPagePicker } from "./mobile-list-pagination";
import { MobileSearchButton } from "./mobile-search-button";

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
  const search = useMobileSearch();
  const [navOpen, setNavOpen] = useState(false);
  const [pagesOpen, setPagesOpen] = useState(false);
  const keyboardRef = useMobileKeyboardLayout<HTMLDivElement>();
  const mode =
    selecting || hasSelection ? "selection" : search.isOpen ? "search" : null;
  useMobileDetailInteraction(mode);
  const pageCount = Math.ceil(totalCount / filter.itemsPerPage);

  function runAction(action: () => void) {
    setPagesOpen(false);
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
  const taggerLabel = intl.formatMessage({
    id: "actions.tagger",
    defaultMessage: "Tagger",
  });
  const Row = hosted ? "div" : MobileToolbarRow;

  return (
    <>
      {!hosted && <MobileNavSheet open={navOpen} onOpenChange={setNavOpen} />}
      {hosted && (
        <>
          <MobileDetailChromePortal slot="pagination">
            {pagePicker}
          </MobileDetailChromePortal>
          {onTaggerMode && (
            <MobileDetailChromePortal slot="list-actions">
              <Separator className="my-1" />
              <Button
                variant="ghost"
                className="h-11 w-full justify-start"
                onClick={() => runAction(onTaggerMode)}
              >
                <Tags />
                {taggerLabel}
              </Button>
            </MobileDetailChromePortal>
          )}
        </>
      )}
      <div
        ref={hosted ? undefined : keyboardRef}
        data-mobile-list-mode={mode ?? "browse"}
        className={cn(
          !hosted &&
            "mobile-keyboard-layout @container shrink-0 border-t bg-background pb-[env(safe-area-inset-bottom,0px)]",
        )}
      >
        <Row
          className={
            hosted
              ? "flex h-11 items-center gap-[var(--mobile-toolbar-gap)]"
              : undefined
          }
        >
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
            <MobileSearchRow
              rowRef={search.rowRef}
              onClose={search.closeSearch}
            >
              <SearchInput
                inputRef={search.inputRef}
                mobile
                value={filter.searchTerm}
                onChange={onSearch}
                className="min-w-0 flex-1"
              />
            </MobileSearchRow>
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
              <MobileSearchButton
                buttonRef={search.buttonRef}
                inputRef={search.inputRef}
                query={filter.searchTerm}
                onOpen={search.openSearch}
                onClear={() => onSearch("")}
              />
              <Button
                variant={activeFilterCount > 0 ? "secondary" : "ghost"}
                size="icon-lg"
                className="relative size-11 shrink-0"
                onClick={() => runAction(openFilterSidebar)}
                aria-label={intl.formatMessage({
                  id: "search_filter.edit_filter",
                  defaultMessage: "Filters",
                })}
              >
                <Funnel />
                {activeFilterCount > 0 && (
                  <Badge className="absolute top-0 right-0 h-4 min-w-4 px-1 tabular-nums">
                    {activeFilterCount}
                  </Badge>
                )}
              </Button>
              <Button
                variant="ghost"
                size="icon-lg"
                className="size-11 shrink-0"
                onClick={() => runAction(onViewOptions)}
                aria-label={intl.formatMessage({
                  id: "view_options",
                  defaultMessage: "View options",
                })}
              >
                <Settings2 />
              </Button>
              {!hosted && onTaggerMode && (
                <Button
                  variant="ghost"
                  size="icon-lg"
                  className="size-11 shrink-0"
                  onClick={() => runAction(onTaggerMode)}
                  aria-label={taggerLabel}
                >
                  <Tags />
                </Button>
              )}
            </>
          )}
        </Row>
      </div>
    </>
  );
}
