import { useCallback, useEffect, useMemo } from "react";
import { skipToken, useApolloClient, useQuery } from "@apollo/client/react";
import type { OperationVariables } from "@apollo/client";
import { gql } from "graphql-tag";
import type { ListFilterModel } from "@/models/list-filter/filter";
import type { IHasID } from "@/utils/data";
import type { ListCountData, ListDataSource } from "./entity-list-types";
import { useCachedQueryResult } from "./use-cached-query-result";
import { retainListQuery } from "@/core/list-query-cache";

// Hook order stays stable for local lists, which never issue this query.
const NOOP_QUERY = gql`query EntityListNoop { __typename }`;

function useRetainedListQuery(
  query: typeof NOOP_QUERY,
  variables: OperationVariables | undefined,
) {
  const client = useApolloClient();
  const key = JSON.stringify(variables);
  useEffect(() => {
    if (!key) return;
    return retainListQuery(
      client.cache,
      query,
      JSON.parse(key) as OperationVariables,
    );
  }, [client, query, key]);
}

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
  const query = source.kind === "graphql" ? source.query : NOOP_QUERY;
  useRetainedListQuery(query, variables);
  const raw = useQuery<TData, TVariables>(query, options);
  const countQuery = source.kind === "graphql" ? source.countQuery : undefined;
  // Totals depend on membership, not ordering, page number or page size.
  const countVariables = useMemo(() => {
    if (!countQuery || source.kind !== "graphql") return undefined;
    const scope = source.makeVariables(filter);
    return { ...scope, filter: { q: filter.searchTerm } } as TVariables;
  }, [source, filter, countQuery]);
  useRetainedListQuery(countQuery ?? NOOP_QUERY, countVariables);
  const total = useQuery<ListCountData, TVariables>(
    countQuery ?? NOOP_QUERY,
    countVariables ? { variables: countVariables } : skipToken,
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
    countError:
      source.kind === "graphql"
        ? !!(countQuery ? total.error : raw.error)
        : !!source.error,
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
