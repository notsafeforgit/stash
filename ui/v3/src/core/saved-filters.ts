import { useQuery, useMutation } from "@apollo/client/react";
import * as GQL from "./generated-graphql";
import type { ListFilterModel } from "src/models/list-filter/filter";
import { evictQueries } from "./client";

export const useFindSavedFilters = (mode?: GQL.FilterMode) =>
  useQuery<GQL.FindSavedFiltersQuery, GQL.FindSavedFiltersQueryVariables>(
    GQL.FindSavedFiltersDocument,
    { variables: { mode } },
  );

export const useSaveFilter = () => {
  const [saveFilterMutation] = useMutation<
    GQL.SaveFilterMutation,
    GQL.SaveFilterMutationVariables
  >(GQL.SaveFilterDocument, {
    update(cache, result) {
      if (!result.data?.saveFilter) return;
      evictQueries(cache, [GQL.FindSavedFiltersDocument]);
    },
  });

  function saveFilter(filter: ListFilterModel, name: string, id?: string) {
    const filterCopy = filter.clone();
    return saveFilterMutation({
      variables: {
        input: {
          id,
          mode: filter.mode,
          name,
          find_filter: filterCopy.makeFindFilter(),
          filter_ast: filterCopy.makeFilterAst(),
          ui_options: filterCopy.makeSavedUIOptions(),
        },
      },
    });
  }

  return saveFilter;
};
