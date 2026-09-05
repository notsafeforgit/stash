import { useEffect } from "react";
import type { ListFilterModel } from "@/models/list-filter/filter";
import { shouldPreserveListScrollDuringRefill } from "./list-scroll-state";

/** Refill a shortened Apollo page after deletion, retaining its scroll position.
 * Local lists already page the complete in-memory collection. */
export function useListPageRefill({
  remote,
  filter,
  setFilter,
  count,
  items,
  isLoading,
  error,
  refetch,
}: {
  remote: boolean;
  filter: ListFilterModel;
  setFilter: (
    next: ListFilterModel | ((prev: ListFilterModel) => ListFilterModel),
  ) => void;
  count: number;
  items: readonly unknown[];
  isLoading: boolean;
  error?: Error;
  refetch: () => Promise<unknown>;
}) {
  const totalPagesAfterDataChange = Math.max(
    1,
    Math.ceil(count / filter.itemsPerPage),
  );
  const preserveScrollDuringRefill =
    remote &&
    shouldPreserveListScrollDuringRefill(
      filter.currentPage,
      filter.itemsPerPage,
      count,
      items.length,
    );
  useEffect(() => {
    if (isLoading || error || !remote) return;

    if (filter.currentPage > totalPagesAfterDataChange) {
      setFilter((f) => f.changePage(totalPagesAfterDataChange));
      return;
    }

    if (preserveScrollDuringRefill) {
      void refetch().catch(() => {});
    }
  }, [
    filter,
    isLoading,
    remote,
    preserveScrollDuringRefill,
    refetch,
    setFilter,
    totalPagesAfterDataChange,
    error,
  ]);

  return preserveScrollDuringRefill;
}
