import type React from "react";
import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import {
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
} from "lucide-react";
import { Button } from "src/components/ui/button";
import { NumberInput } from "src/components/filters/number-input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "src/components/ui/popover";
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
} from "src/components/ui/pagination";

// ── Page-jump popover ────────────────────────────────────────────────────────

const PageJumpForm: React.FC<{
  currentPage: number;
  totalPages: number;
  onChangePage: (page: number) => void;
  onClose: () => void;
}> = ({ currentPage, totalPages, onChangePage, onClose }) => {
  const [value, setValue] = useState(currentPage);

  // Reset the input whenever the popover re-opens against a different page.
  useEffect(() => {
    setValue(currentPage);
  }, [currentPage]);

  function submit() {
    if (value >= 1 && value <= totalPages) {
      onChangePage(value);
    }
    onClose();
  }

  return (
    <form
      className="flex items-center gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
      }}
    >
      <NumberInput
        value={value}
        min={1}
        max={totalPages}
        autoFocus
        onChange={setValue}
        inputClassName="w-16 text-sm tabular-nums"
      />
      <Button type="submit" size="sm">
        Go
      </Button>
    </form>
  );
};

// ── Page window helper ────────────────────────────────────────────────────────

/** Returns the page numbers (and null for ellipsis) to display around currentPage. */
function getPageWindow(
  currentPage: number,
  totalPages: number,
): (number | null)[] {
  if (totalPages <= 7) {
    return Array.from({ length: totalPages }, (_, i) => i + 1);
  }
  const pages: (number | null)[] = [1];
  const left = Math.max(2, currentPage - 1);
  const right = Math.min(totalPages - 1, currentPage + 1);
  if (left > 2) pages.push(null);
  for (let p = left; p <= right; p++) pages.push(p);
  if (right < totalPages - 1) pages.push(null);
  pages.push(totalPages);
  return pages;
}

// ── ListPagination ────────────────────────────────────────────────────────────

interface ListPaginationProps {
  currentPage: number;
  itemsPerPage: number;
  totalItems: number;
  onChangePage: (page: number) => void;
}

export const ListPagination: React.FC<ListPaginationProps> = ({
  currentPage,
  itemsPerPage,
  totalItems,
  onChangePage,
}) => {
  const intl = useIntl();
  const totalPages = Math.ceil(totalItems / itemsPerPage);
  const [showJump, setShowJump] = useState(false);

  if (totalPages <= 1) return null;

  const pageWindow = getPageWindow(currentPage, totalPages);

  return (
    <Pagination className="w-auto">
      <PaginationContent className="gap-0.5">
        {/* First page */}
        <PaginationItem>
          <PaginationLink
            size="icon-sm"
            disabled={currentPage === 1}
            onClick={() => onChangePage(1)}
            aria-label={intl.formatMessage({ id: "pagination.first" })}
          >
            <ChevronsLeft className="size-3.5" />
          </PaginationLink>
        </PaginationItem>

        {/* Previous (icon only) */}
        <PaginationItem>
          <PaginationLink
            size="icon-sm"
            disabled={currentPage === 1}
            onClick={() => onChangePage(currentPage - 1)}
            aria-label={intl.formatMessage({
              id: "pagination.previous",
              defaultMessage: "Previous",
            })}
          >
            <ChevronLeft className="size-3.5" />
          </PaginationLink>
        </PaginationItem>

        {/* Page numbers — auto-width so 4-digit page numbers don't crop into
            the icon-sm fixed square. Min width keeps single-digit buttons
            visually balanced next to the icon arrows. */}
        {pageWindow.map((page, i) =>
          page === null ? (
            <PaginationItem key={`ellipsis-${i}`}>
              <PaginationEllipsis className="size-7" />
            </PaginationItem>
          ) : (
            <PaginationItem key={page}>
              {page === currentPage ? (
                <Popover open={showJump} onOpenChange={setShowJump}>
                  <PopoverTrigger
                    render={
                      <PaginationLink
                        isActive
                        size="sm"
                        className="min-w-7 px-1.5 text-xs tabular-nums"
                      >
                        {page}
                      </PaginationLink>
                    }
                  />
                  <PopoverContent
                    className="w-auto p-2"
                    side="bottom"
                    align="center"
                  >
                    <PageJumpForm
                      currentPage={currentPage}
                      totalPages={totalPages}
                      onChangePage={onChangePage}
                      onClose={() => setShowJump(false)}
                    />
                  </PopoverContent>
                </Popover>
              ) : (
                <PaginationLink
                  size="sm"
                  className="min-w-7 px-1.5 text-xs tabular-nums"
                  onClick={() => onChangePage(page)}
                >
                  {page}
                </PaginationLink>
              )}
            </PaginationItem>
          ),
        )}

        {/* Next (icon only) */}
        <PaginationItem>
          <PaginationLink
            size="icon-sm"
            disabled={currentPage === totalPages}
            onClick={() => onChangePage(currentPage + 1)}
            aria-label={intl.formatMessage({
              id: "pagination.next",
              defaultMessage: "Next",
            })}
          >
            <ChevronRight className="size-3.5" />
          </PaginationLink>
        </PaginationItem>

        {/* Last page */}
        <PaginationItem>
          <PaginationLink
            size="icon-sm"
            disabled={currentPage === totalPages}
            onClick={() => onChangePage(totalPages)}
            aria-label={intl.formatMessage({ id: "pagination.last" })}
          >
            <ChevronsRight className="size-3.5" />
          </PaginationLink>
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  );
};

// ── PaginationMeta ─────────────────────────────────────────────────────────

interface PaginationMetaProps {
  currentPage: number;
  itemsPerPage: number;
  totalItems: number;
  metadataByline?: React.ReactNode;
}

export const PaginationMeta: React.FC<PaginationMetaProps> = ({
  currentPage,
  itemsPerPage,
  totalItems,
  metadataByline,
}) => {
  const intl = useIntl();

  // Hide only when we have nothing to show. During reloads (sort, filter
  // change) the previously-cached totalItems flows through, so keeping the
  // bar mounted avoids a flash — sort in particular never changes the
  // displayed numbers, so disappearing/reappearing was pure jitter.
  if (totalItems === 0) return null;

  const first = Math.min((currentPage - 1) * itemsPerPage + 1, totalItems);
  const last = Math.min(first + itemsPerPage - 1, totalItems);
  const indexText = `${intl.formatNumber(first)}–${intl.formatNumber(last)} of ${intl.formatNumber(totalItems)}`;

  return (
    <span className="inline-flex items-center gap-2">
      <span>{indexText}</span>
      {metadataByline && <span>{metadataByline}</span>}
    </span>
  );
};
