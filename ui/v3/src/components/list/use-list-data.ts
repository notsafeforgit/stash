import { useMemo } from "react";
import { skipToken, useQuery } from "@apollo/client/react";
import type { OperationVariables } from "@apollo/client";
import { gql } from "graphql-tag";
import { useDebouncedValue } from "@/hooks/debounce";
import type { ListFilterModel } from "@/models/list-filter/filter";
import type { IHasID } from "@/utils/data";
import type { ListDataSource } from "./entity-list-types";
import { useCachedQueryResult } from "./use-cached-query-result";

// Hook order stays stable for local lists, which never issue this query.
const NOOP_QUERY = gql`query EntityListNoop { __typename }`;

export function useListData<
  TData,
  TItem extends IHasID,
  TVariables extends OperationVariables,
>(source: ListDataSource<TData, TItem, TVariables>, filter: ListFilterModel) {
  const debouncedFilter = useDebouncedValue(filter, 150);
  const options =
    source.kind === "graphql"
      ? { variables: source.makeVariables(debouncedFilter) }
      : skipToken;
  const variables = options === skipToken ? undefined : options.variables;
  const raw = useQuery<TData, TVariables>(
    source.kind === "graphql" ? source.query : NOOP_QUERY,
    options,
  );
  const result = useCachedQueryResult(
    debouncedFilter,
    raw,
    JSON.stringify(variables),
  );
  // Layout preferences do not change query variables and must not flash a spinner.
  const pending =
    source.kind === "graphql"
      ? JSON.stringify(source.makeVariables(filter)) !==
        JSON.stringify(variables)
      : filter.makeQueryParameters() !== debouncedFilter.makeQueryParameters();
  const page = useMemo(
    () =>
      source.kind === "local"
        ? source.filter(source.items, debouncedFilter)
        : source.extractResult(result.data as TData | undefined),
    [source, debouncedFilter, result.data],
  );

  return {
    ...page,
    loading:
      pending ||
      (source.kind === "local"
        ? !!source.loading
        : result.isPending || result.loading),
    error: source.kind === "graphql" ? raw.error : undefined,
    hasData: source.kind === "local" || result.data !== undefined,
    refetch: raw.refetch,
    refreshing: source.kind === "graphql" && raw.loading,
  };
}
