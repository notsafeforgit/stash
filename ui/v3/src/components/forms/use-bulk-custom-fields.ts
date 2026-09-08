import { skipToken, useQuery } from "@apollo/client/react";
import type { BulkApplyTarget } from "@/components/list/list-provider";
import {
  BulkCustomFieldSummaryDocument,
  type FilterMode,
} from "@/core/generated-graphql";
import { bulkCustomFieldsSchema } from "./bulk-custom-fields";

interface BulkCustomFieldsTarget {
  open: boolean;
  mode: FilterMode;
  items: readonly { id: string }[];
  matching?: BulkApplyTarget;
}

export function useBulkCustomFields({
  open,
  mode,
  items,
  matching,
}: BulkCustomFieldsTarget) {
  const { data, loading, error, refetch } = useQuery(
    BulkCustomFieldSummaryDocument,
    open
      ? {
          variables: {
            input: matching
              ? {
                  mode,
                  find_filter: matching.findFilter,
                  filter_ast: matching.filterAST,
                }
              : { mode, ids: items.map(({ id }) => id) },
          },
          // Reopening or changing scope must use the current complete target.
          fetchPolicy: "network-only",
        }
      : skipToken,
  );
  const summary =
    open && !loading && !error ? data?.bulkCustomFieldSummary : undefined;
  return {
    summary,
    loading,
    error,
    refetch,
    schema: bulkCustomFieldsSchema(summary),
  };
}
