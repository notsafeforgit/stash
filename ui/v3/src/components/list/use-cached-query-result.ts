import { useEffect, useRef, useState } from "react";
import type { ListFilterModel } from "src/models/list-filter/filter";

// Returns true if changing from `oldFilter` to `newFilter` would affect the
// total result count (i.e. filter criteria changed, not just page/sort).
function totalCountImpacted(
  oldFilter: ListFilterModel,
  newFilter: ListFilterModel,
) {
  if (oldFilter.criteria.length !== newFilter.criteria.length) return true;

  return oldFilter.criteria.some((c) => {
    const nc = newFilter.criteria.find((x) => x.getId() === c.getId());
    return !nc || JSON.stringify(c) !== JSON.stringify(nc);
  });
}

/**
 * Caches a query result so that pagination / sort changes don't cause the
 * result count and content to flash to zero while the new page loads.
 *
 * The cached result is only replaced immediately (without waiting for loading
 * to finish) when the filter criteria change in a way that impacts the count.
 *
 * Also returns `isPending: true` whenever `filter` has changed since the last
 * time a completed (non-loading) result was stored — i.e. the displayed data
 * is stale relative to the current filter. Callers can use this to show a
 * loading indicator even when the cached result has `loading: false`.
 */
export function useCachedQueryResult<T extends { loading: boolean }>(
  filter: ListFilterModel,
  result: T,
): T & { isPending: boolean } {
  const [cachedResult, setCachedResult] = useState(result);
  // Tracks the filter object whose data is currently displayed.
  const [displayedFilter, setDisplayedFilter] = useState(filter);
  const lastFilterRef = useRef(filter);

  useEffect(() => {
    if (!result.loading) {
      setCachedResult(result);
      setDisplayedFilter(filter);
    } else {
      if (totalCountImpacted(lastFilterRef.current, filter)) {
        setCachedResult(result);
      }
    }
    lastFilterRef.current = filter;
  }, [filter, result]);

  return { ...cachedResult, isPending: filter !== displayedFilter };
}
