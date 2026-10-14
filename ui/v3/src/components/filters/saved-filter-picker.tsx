import { useState } from "react";
import { Check, ChevronDown, Pin, PinOff, Search, Trash2 } from "lucide-react";
import { FormattedMessage, useIntl } from "react-intl";
import { DeleteDialog } from "@/components/detail/delete-dialog";
import { Button } from "@/components/ui/button";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Empty, EmptyHeader, EmptyDescription } from "@/components/ui/empty";
import type { SavedFilterDataFragment } from "@/core/generated-graphql";
import { usePopoverOutsideTap } from "@/hooks/use-outside-tap";

export function SavedFilterPicker({
  filters,
  selectedId,
  label,
  pinnedIds,
  onSelect,
  onTogglePinned,
  onDelete,
}: {
  filters: readonly SavedFilterDataFragment[];
  selectedId: string;
  label: string;
  pinnedIds: readonly string[];
  onSelect: (filter: SavedFilterDataFragment) => void;
  onTogglePinned: (id: string) => void;
  onDelete: (filter: SavedFilterDataFragment) => Promise<void>;
}) {
  const intl = useIntl();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [pendingDelete, setPendingDelete] =
    useState<SavedFilterDataFragment | null>(null);
  const { triggerRef, popupRef } = usePopoverOutsideTap(open, () =>
    setOpen(false),
  );
  const matching = filters
    .filter((filter) =>
      filter.name
        .toLocaleLowerCase()
        .includes(query.trim().toLocaleLowerCase()),
    )
    .sort((a, b) =>
      a.name.localeCompare(b.name, intl.locale, { numeric: true }),
    );
  const groups = [
    {
      label: "search_filter.pinned_saved_filters",
      filters: matching.filter((filter) => pinnedIds.includes(filter.id)),
    },
    {
      label: "search_filter.saved_filters",
      filters: matching.filter((filter) => !pinnedIds.includes(filter.id)),
    },
  ];

  return (
    <>
      {/* Each row has independent actions. A popover keeps all three reachable
          by keyboard and assistive technology, without buttons inside options. */}
      <Popover
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (next) setQuery("");
        }}
      >
        <PopoverTrigger
          ref={triggerRef}
          render={<Button variant="outline" className="min-w-0 max-w-full" />}
        >
          <span className="truncate">{label}</span>
          <ChevronDown data-icon="inline-end" />
        </PopoverTrigger>
        <PopoverContent
          ref={popupRef}
          align="start"
          className="w-80 max-w-(--available-width) max-h-(--available-height)"
          finalFocus={pendingDelete ? false : undefined}
        >
          <PopoverTitle className="sr-only">
            <FormattedMessage id="search_filter.saved_filters" />
          </PopoverTitle>
          <InputGroup>
            <InputGroupAddon>
              <Search />
            </InputGroupAddon>
            <InputGroupInput
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
              aria-label={intl.formatMessage({
                id: "search_filter.search_saved_filters",
              })}
              placeholder={intl.formatMessage({
                id: "search_filter.search_saved_filters",
              })}
            />
          </InputGroup>
          <div className="min-h-0 max-h-72 overflow-y-auto overscroll-contain touch-pan-y">
            <ul>
              {groups.flatMap((group) =>
                group.filters.length > 0
                  ? [
                      <li
                        key={group.label}
                        className="px-2 py-1 text-xs text-muted-foreground"
                      >
                        <FormattedMessage id={group.label} />
                      </li>,
                      ...group.filters.map((filter) => {
                        const pinned = pinnedIds.includes(filter.id);
                        const pinLabel = intl.formatMessage(
                          {
                            id: pinned
                              ? "search_filter.unpin_saved_filter"
                              : "search_filter.pin_saved_filter",
                          },
                          { name: filter.name },
                        );
                        const deleteLabel = intl.formatMessage(
                          { id: "search_filter.delete_saved_filter" },
                          { name: filter.name },
                        );
                        return (
                          <li
                            key={filter.id}
                            className="flex items-center gap-1"
                          >
                            <Button
                              variant="ghost"
                              className="h-11 min-w-0 flex-1 justify-start"
                              aria-pressed={filter.id === selectedId}
                              title={filter.name}
                              onClick={() => {
                                setOpen(false);
                                onSelect(filter);
                              }}
                            >
                              {filter.id === selectedId && (
                                <Check data-icon="inline-start" />
                              )}
                              <span className="truncate">{filter.name}</span>
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="size-11"
                              aria-label={pinLabel}
                              title={pinLabel}
                              aria-pressed={pinned}
                              onClick={() => onTogglePinned(filter.id)}
                            >
                              {pinned ? <PinOff /> : <Pin />}
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="size-11"
                              aria-label={deleteLabel}
                              title={deleteLabel}
                              onClick={() => {
                                setOpen(false);
                                setPendingDelete(filter);
                              }}
                            >
                              <Trash2 />
                            </Button>
                          </li>
                        );
                      }),
                    ]
                  : [],
              )}
            </ul>
            {matching.length === 0 && (
              <Empty>
                <EmptyHeader>
                  <EmptyDescription>
                    <FormattedMessage
                      id={
                        filters.length
                          ? "search_filter.no_matching_saved_filters"
                          : "search_filter.no_saved_filters"
                      }
                    />
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
          </div>
        </PopoverContent>
      </Popover>
      <DeleteDialog
        open={pendingDelete !== null}
        onOpenChange={(next) => {
          if (!next) setPendingDelete(null);
        }}
        entityName={pendingDelete?.name}
        description={
          <FormattedMessage id="search_filter.delete_saved_filter_description" />
        }
        finalFocus={triggerRef}
        onConfirm={async () => {
          if (pendingDelete) await onDelete(pendingDelete);
        }}
      />
    </>
  );
}
