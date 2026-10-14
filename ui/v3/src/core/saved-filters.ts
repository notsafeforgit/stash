import { useQuery, useMutation } from "@apollo/client/react";
import type { Reference } from "@apollo/client";
import * as GQL from "./generated-graphql";
import type { ListFilterModel } from "src/models/list-filter/filter";
import { evictQueries } from "./client";

export const useFindSavedFilters = (mode?: GQL.FilterMode) =>
  useQuery<GQL.FindSavedFiltersQuery, GQL.FindSavedFiltersQueryVariables>(
    GQL.FindSavedFiltersDocument,
    { variables: { mode } },
  );

export const useDestroySavedFilter = () =>
  useMutation<
    GQL.DestroySavedFilterMutation,
    GQL.DestroySavedFilterMutationVariables
  >(GQL.DestroySavedFilterDocument, {
    update(cache, result, { variables }) {
      const id = variables?.input.id;
      if (!id || result.data?.destroySavedFilter !== true) return;
      // Update every cached mode (including the unfiltered picker) only after
      // the server confirms deletion. No refetch is needed to hide the preset.
      cache.modify({
        fields: {
          findSavedFilters(existing: readonly Reference[], { readField }) {
            return existing.filter((item) => readField("id", item) !== id);
          },
        },
      });
      cache.evict({ id: cache.identify({ __typename: "SavedFilter", id }) });
      cache.gc();
    },
  });

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
