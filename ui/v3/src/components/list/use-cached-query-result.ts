import { useEffect, useState } from "react";
import type { ListFilterModel } from "src/models/list-filter/filter";

interface QueryResult<TData> {
  loading: boolean;
  data?: TData;
  error?: Error;
}

/** Preserve usable data on a failed refresh, but never show another filter's
 * results as a successful (or empty) response to the current filter. */
export function displayedQueryData<T>(
  current: { data?: T; loading: boolean; error?: Error },
  key: string,
  cached: { data?: T; key: string },
): T | undefined {
  if (current.data !== undefined) return current.data;
  if (current.loading || (current.error && key === cached.key))
    return cached.data;
  return undefined;
}

export function useCachedQueryResult<TData>(
  filter: ListFilterModel,
  result: QueryResult<TData>,
  key = filter.makeQueryParameters(),
) {
  const [cached, setCached] = useState({ data: result.data, key });
  useEffect(() => {
    if (!result.loading && !result.error && result.data !== undefined) {
      setCached((previous) =>
        previous.data === result.data && previous.key === key
          ? previous
          : { data: result.data, key },
      );
    }
  }, [key, result.data, result.loading, result.error]);

  return {
    ...result,
    data: displayedQueryData(result, key, cached),
    isPending: result.loading && key !== cached.key,
  };
}
