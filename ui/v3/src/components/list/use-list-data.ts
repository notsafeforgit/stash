import { useCallback, useMemo } from "react";
import { skipToken, useQuery } from "@apollo/client/react";
import type { OperationVariables } from "@apollo/client";
import { gql } from "graphql-tag";
import type { ListFilterModel } from "@/models/list-filter/filter";
import type { IHasID } from "@/utils/data";
import type { ListCountData, ListDataSource } from "./entity-list-types";
import { useCachedQueryResult } from "./use-cached-query-result";

// Hook order stays stable for local lists, which never issue this query.
const NOOP_QUERY = gql`query EntityListNoop { __typename }`;

export function useListData<
  TData,
  TItem extends IHasID,
  TVariables extends OperationVariables,
>(source: ListDataSource<TData, TItem, TVariables>, filter: ListFilterModel) {
  // SearchInput debounces typing before committing the filter. A second delay
  // here held up searches, clear/Enter actions, and ordinary page navigation.
  const options =
    source.kind === "graphql"
      ? { variables: source.makeVariables(filter) }
      : skipToken;
  const variables = options === skipToken ? undefined : options.variables;
  const raw = useQuery<TData, TVariables>(
    source.kind === "graphql" ? source.query : NOOP_QUERY,
    options,
  );
  const countQuery = source.kind === "graphql" ? source.countQuery : undefined;
  const total = useQuery<ListCountData, TVariables>(
    countQuery ?? NOOP_QUERY,
    countQuery ? options : skipToken,
  );
  const refetchPage = raw.refetch;
  const refetchCount = total.refetch;
  const refetch = useCallback(async () => {
    if (source.kind === "local") return source.refresh?.();
    await Promise.all([refetchPage(), ...(countQuery ? [refetchCount()] : [])]);
  }, [source, refetchPage, refetchCount, countQuery]);
  const result = useCachedQueryResult<TData>(
    filter,
    {
      loading: raw.loading,
      error: raw.error,
      data: raw.dataState === "complete" ? raw.data : undefined,
    },
    JSON.stringify(variables),
  );
  const page = useMemo<{ count?: number; items: TItem[] }>(
    () =>
      source.kind === "local"
        ? source.filter(source.items, filter)
        : source.extractResult(result.data),
    [source, filter, result.data],
  );

  return {
    ...page,
    count: countQuery
      ? total.dataState === "complete"
        ? total.data.result.count
        : undefined
      : page.count,
    loading:
      source.kind === "local"
        ? !!source.loading
        : result.isPending || result.loading,
    error:
      source.kind === "graphql"
        ? (raw.error ?? (countQuery ? total.error : undefined))
        : source.error,
    hasData:
      source.kind === "local"
        ? !source.error || source.items.length > 0
        : result.data !== undefined,
    refetch,
    refreshing:
      source.kind === "graphql" &&
      (raw.loading || (!!countQuery && total.loading)),
  };
}
