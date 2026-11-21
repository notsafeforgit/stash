import { useCallback } from "react";
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

function useSaveFilterMutation() {
  return useMutation<GQL.SaveFilterMutation, GQL.SaveFilterMutationVariables>(
    GQL.SaveFilterDocument,
    {
      update(cache, result) {
        if (!result.data?.saveFilter) return;
        evictQueries(cache, [GQL.FindSavedFiltersDocument]);
      },
    },
  );
}

export const useSaveFilter = () => {
  const [saveFilterMutation] = useSaveFilterMutation();

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

// The fragment selects every find-filter field the API exposes. Dropping the
// Apollo `__typename` is the only change, because GraphQL rejects it as input.
function storedFindFilter(
  find: GQL.SavedFilterDataFragment["find_filter"],
): GQL.FindFilterType | null {
  if (!find) return null;
  const { q, page, per_page, sort, direction } = find;
  return { q, page, per_page, sort, direction };
}

/**
 * Builds the saveFilter input that renames an existing saved filter. The stored
 * criteria are echoed exactly: re-serialising through the list model would drop
 * criteria it does not understand, and omitted or null criteria clear the filter.
 */
export function buildRenameSavedFilterInput(
  saved: GQL.SavedFilterDataFragment,
  name: string,
): GQL.SaveFilterInput {
  return {
    id: saved.id,
    mode: saved.mode,
    name,
    find_filter: storedFindFilter(saved.find_filter),
    filter_ast: saved.filter_ast,
    ui_options: saved.ui_options,
  };
}

export const useRenameSavedFilter = () => {
  const [saveFilterMutation] = useSaveFilterMutation();

  return useCallback(
    (saved: GQL.SavedFilterDataFragment, name: string) =>
      saveFilterMutation({
        variables: { input: buildRenameSavedFilterInput(saved, name) },
      }),
    [saveFilterMutation],
  );
};
