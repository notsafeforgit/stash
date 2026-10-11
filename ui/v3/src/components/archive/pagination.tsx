import {
  Children,
  createContext,
  useCallback,
  useContext,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";
import { useMsg } from "@/hooks/message";
import { Button } from "@/components/ui/button";
import {
  Pagination,
  PaginationContent,
  PaginationItem,
} from "@/components/ui/pagination";
import { PageSummary } from "@/components/list/page-summary";
import {
  archivePageInfoPath,
  createArchivePaginationAPI,
  type ArchivePageQuery,
} from "@/core/native-archive/pagination-api";
import { useActivityRead } from "./activity/shared";

const LastPage = createContext(false);

/** Counts are independent of the list request; navigation stays usable while
 * counting. Scope changes discard the old count, including late responses. */
export function ArchivePagination({
  query,
  pageSize = 25,
  page,
  loaded,
  refresh = 0,
  children,
}: {
  query: ArchivePageQuery;
  pageSize?: number;
  page?: number;
  loaded: number | undefined;
  refresh?: number;
  children: ReactNode;
}) {
  const msg = useMsg();
  const [api] = useState(() => createArchivePaginationAPI());
  const [retry, setRetry] = useState(0);
  const path = archivePageInfoPath(query);
  const load = useCallback(
    (signal: AbortSignal) => api(path, signal),
    [api, path],
  );
  const read = useActivityRead(path, load, refresh + retry);
  const info = read.current;
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      {info && read.error === undefined ? (
        <PageSummary
          total={info.total}
          page={page ?? Math.floor(info.offset / pageSize) + 1}
          pageSize={pageSize}
        />
      ) : (
        <div
          className="flex items-center gap-2 text-sm text-muted-foreground"
          role="status"
        >
          {read.error === undefined ? (
            msg("pagination.counting", "Counting items…")
          ) : (
            <>
              {msg("pagination.count_failed", "Could not load item count")}
              <Button
                variant="outline"
                size="sm"
                onClick={() => setRetry((value) => value + 1)}
              >
                {msg("actions.retry", "Retry")}
              </Button>
            </>
          )}
        </div>
      )}
      <LastPage
        value={
          !!info &&
          read.error === undefined &&
          loaded !== undefined &&
          info.offset + loaded >= info.total
        }
      >
        <Pagination className="mx-0 w-auto">
          <PaginationContent className="flex-wrap gap-2">
            {Children.map(
              children,
              (child) => child && <PaginationItem>{child}</PaginationItem>,
            )}
          </PaginationContent>
        </Pagination>
      </LastPage>
    </div>
  );
}

export function ArchiveNextPage({
  disabled,
  ...props
}: ComponentProps<typeof Button>) {
  const last = useContext(LastPage);
  return <Button {...props} disabled={disabled || last} />;
}
