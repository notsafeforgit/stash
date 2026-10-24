import { useState, type Dispatch, type SetStateAction } from "react";
import { useIntl } from "react-intl";
import {
  ChevronUp,
  Ellipsis,
  Funnel,
  ListChecks,
  Settings2,
  Tags,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from "@/components/ui/drawer";
import {
  EntityActionsList,
  type EntityActionItem,
} from "@/components/detail/entity-actions-menu";
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { MobileToolbarRow } from "@/components/layout/mobile-toolbar";
import { MobileSearchRow } from "@/components/layout/mobile-search-row";
import {
  MobileDetailChromePortal,
  useMobileDetailChrome,
  useMobileDetailInteraction,
} from "@/components/layout/mobile-detail-chrome";
import { useMobileKeyboardLayout } from "@/hooks/use-mobile-keyboard-layout";
import { useMobileSearch } from "@/hooks/use-mobile-search";
import { usePopoverOutsideTap } from "@/hooks/use-outside-tap";
import { cn } from "@/lib/utils";
import type { ListFilterModel } from "@/models/list-filter/filter";
import { SearchInput } from "./search-input";
import {
  CountUnavailable,
  MobileListPagePicker,
} from "./mobile-list-pagination";
import { MobileSearchButton } from "./mobile-search-button";

export interface MobileListControlsProps {
  filter: ListFilterModel;
  setFilter: Dispatch<SetStateAction<ListFilterModel>>;
  totalCount: number | undefined;
  countError?: boolean;
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
  selectionActions?: React.ReactNode;
  pageActions?: readonly EntityActionItem[];
}

/** One row, with explicit replacement modes instead of stacked toolbars. */
export function MobileListControls({
  filter,
  setFilter,
  totalCount,
  countError,
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
  selectionActions,
  pageActions = [],
}: MobileListControlsProps) {
  const intl = useIntl();
  const chrome = useMobileDetailChrome();
  const hosted = chrome?.mobile ?? false;
  const search = useMobileSearch();
  const [pagesOpen, setPagesOpen] = useState(false);
  const [actionsOpen, setActionsOpen] = useState(false);
  const { triggerRef, popupRef } = usePopoverOutsideTap(pagesOpen, () =>
    setPagesOpen(false),
  );
  const keyboardRef = useMobileKeyboardLayout<HTMLDivElement>();
  const mode =
    selecting || hasSelection ? "selection" : search.isOpen ? "search" : null;
  useMobileDetailInteraction(mode);
  const pageCount =
    totalCount === undefined ? 0 : Math.ceil(totalCount / filter.itemsPerPage);

  function closeActions() {
    setPagesOpen(false);
    setActionsOpen(false);
    chrome?.setPanel(null);
  }

  function runAction(action: () => void) {
    closeActions();
    action();
  }

  const pagePicker = (
    <MobileListPagePicker
      currentPage={filter.currentPage}
      itemsPerPage={filter.itemsPerPage}
      totalItems={totalCount}
      countError={countError}
      onChangePage={(page) => {
        setFilter(filter.changePage(page));
      }}
    />
  );
  const selectedLabel = intl.formatMessage(
    { id: "list.selected_count", defaultMessage: "{count} selected" },
    { count: selectedCount },
  );
  const paginationControl = (
    <Popover open={pagesOpen} onOpenChange={setPagesOpen}>
      <PopoverTrigger
        ref={triggerRef}
        render={
          <Button
            variant="ghost"
            className={cn(
              "h-11 gap-1 px-2",
              mode === "selection"
                ? "min-w-0 flex-1 justify-between"
                : "w-full justify-between",
            )}
            aria-label={intl.formatMessage({
              id: "pagination.pages",
              defaultMessage: "Pages",
            })}
          />
        }
      >
        <span className="truncate tabular-nums">
          {mode === "selection"
            ? selectedLabel
            : `${intl.formatNumber(filter.currentPage)} / ${intl.formatNumber(pageCount)}`}
        </span>
        <ChevronUp data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent
        ref={popupRef}
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
  );
  const actionsLabel = intl.formatMessage({
    id: "actions.list_actions",
    defaultMessage: "List actions",
  });
  const actions: EntityActionItem[] = [...pageActions];
  if (onTaggerMode) {
    actions.push({
      key: "tagger",
      icon: Tags,
      label: intl.formatMessage({
        id: "actions.tagger",
        defaultMessage: "Tagger",
      }),
      onSelect: onTaggerMode,
    });
  }
  const Row = hosted ? "div" : MobileToolbarRow;

  return (
    <>
      {hosted && (
        <>
          <MobileDetailChromePortal slot="pagination">
            {pagePicker}
          </MobileDetailChromePortal>
          {actions.length > 0 && (
            <MobileDetailChromePortal slot="list-actions">
              <Separator className="my-1" />
              <EntityActionsList items={actions} onAction={closeActions} />
            </MobileDetailChromePortal>
          )}
        </>
      )}
      <div
        ref={hosted ? undefined : keyboardRef}
        data-mobile-list-mode={mode ?? "browse"}
        className={cn(
          !hosted &&
            "mobile-safe-footer mobile-keyboard-layout @container shrink-0 border-t bg-background",
        )}
      >
        <Row
          {...(hosted ? {} : { onNavigate: closeActions })}
          className={
            hosted
              ? "flex h-11 items-center gap-[var(--mobile-toolbar-gap)]"
              : undefined
          }
        >
          {mode === "selection" ? (
            <>
              {pageCount > 1 ? (
                paginationControl
              ) : (
                <span className="min-w-0 flex-1 truncate text-sm tabular-nums">
                  {selectedLabel}
                </span>
              )}
              {selectionActions}
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
                <div className="min-w-0 flex-1">
                  {pageCount > 1 ? (
                    paginationControl
                  ) : (
                    <span className="text-sm text-muted-foreground tabular-nums">
                      {countError ? (
                        <CountUnavailable />
                      ) : totalCount === undefined ? (
                        <Skeleton
                          className="h-4 w-10"
                          aria-label={intl.formatMessage({
                            id: "loading.generic",
                          })}
                        />
                      ) : (
                        intl.formatNumber(totalCount)
                      )}
                    </span>
                  )}
                </div>
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
              {!hosted && actions.length > 0 && (
                <Drawer
                  open={actionsOpen}
                  onOpenChange={(open) => {
                    setPagesOpen(false);
                    setActionsOpen(open);
                  }}
                >
                  <DrawerTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-lg"
                        className="size-11 shrink-0"
                        aria-label={actionsLabel}
                      />
                    }
                  >
                    <Ellipsis />
                  </DrawerTrigger>
                  <DrawerContent>
                    <DrawerHeader className="shrink-0 py-2">
                      <DrawerTitle>{actionsLabel}</DrawerTitle>
                    </DrawerHeader>
                    <div className="min-h-0 overflow-y-auto overscroll-contain px-3 pb-2">
                      <EntityActionsList
                        items={actions}
                        onAction={closeActions}
                      />
                    </div>
                  </DrawerContent>
                </Drawer>
              )}
            </>
          )}
        </Row>
      </div>
    </>
  );
}
