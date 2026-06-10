import { useEffect, useMemo, useRef } from "react";
import type { DocumentNode, OperationVariables } from "@apollo/client";
import { useApolloClient } from "@apollo/client/react";
import type { ListFilterModel } from "src/models/list-filter/filter";

export interface IListPrefetchConfig<
  TVariables extends OperationVariables = OperationVariables,
> {
  query: DocumentNode;
  /** The GraphQL field name on ROOT_QUERY — used for cache eviction (e.g. "findScenes") */
  fieldName: string;
  /** Pure function: given a filter, return the query variables for that page */
  makeVariables: (filter: ListFilterModel) => TVariables;
}

/**
 * Maintains a 3-page sliding window in Apollo's cache (prev, current, next).
 * After each settled page load, prefetches adjacent pages in the background
 * and evicts pages that have scrolled out of the window.
 *
 * @param config  Static (module-level) config for the query type. Pass undefined to disable.
 * @param filter  The settled (debounced) filter, including current page.
 * @param totalPages  Total number of pages — prevents prefetching beyond the last page.
 * @param enabled  Set to false while the current page is still loading.
 */
export function useListPrefetch<TVariables extends OperationVariables>(
  config: IListPrefetchConfig<TVariables> | undefined,
  filter: ListFilterModel,
  totalPages: number,
  enabled: boolean,
): void {
  const client = useApolloClient();

  // page number → variables used when caching that page
  const windowRef = useRef<Map<number, TVariables>>(new Map());

  // Stable key excluding page — changes when criteria or sort change, not when page changes
  const filterKey = useMemo(() => {
    const f = filter.clone();
    f.currentPage = 1;
    return f.makeQueryParameters();
  }, [filter]);

  const prevFilterKey = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!config || !enabled || totalPages === 0) return;

    const { query, fieldName, makeVariables } = config;
    const { cache } = client;
    const { currentPage } = filter;

    // When filter criteria/sort change (not just page), evict all cached pages and reset
    if (
      prevFilterKey.current !== undefined &&
      prevFilterKey.current !== filterKey
    ) {
      for (const [, vars] of windowRef.current) {
        cache.evict({ id: "ROOT_QUERY", fieldName, args: vars });
      }
      windowRef.current.clear();
    }
    prevFilterKey.current = filterKey;

    const newWindow = new Set(
      [currentPage - 1, currentPage, currentPage + 1].filter(
        (p) => p >= 1 && p <= totalPages,
      ),
    );

    // Evict pages that have slid out of the window
    for (const [page, vars] of windowRef.current) {
      if (!newWindow.has(page)) {
        cache.evict({ id: "ROOT_QUERY", fieldName, args: vars });
        windowRef.current.delete(page);
      }
    }
    cache.gc();

    // Prefetch pages newly inside the window
    for (const page of newWindow) {
      if (!windowRef.current.has(page)) {
        const variables = makeVariables(filter.changePage(page));
        windowRef.current.set(page, variables);
        client.query({ query, variables, fetchPolicy: "network-only" });
      }
    }
  }, [client, config, filter, filterKey, enabled, totalPages]);
}
