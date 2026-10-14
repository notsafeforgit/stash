import { ApolloLink, Observable } from "@apollo/client";
import {
  CriterionModifier,
  type DestroySavedFilterMutationVariables,
  FilterMode,
  type SaveFilterMutationVariables,
  type SavedFilterDataFragment,
} from "@/core/generated-graphql";

declare global {
  interface Window {
    savedFiltersFixture?: boolean;
    savedFilterFailure?: "false" | "network";
    holdSavedFilterDelete?: boolean;
    releaseSavedFilterDelete?: () => void;
    savedFilterMutations: { operation: string; id?: string; name?: string }[];
  }
}

export const savedFilterIds = [
  "00000000-0000-4000-8000-000000009001",
  "00000000-0000-4000-8000-000000009002",
];

export function savedFiltersLink(mode: FilterMode) {
  let filters: SavedFilterDataFragment[] = [
    "Favourite clips",
    "Short clips",
  ].map((name, index) => ({
    __typename: "SavedFilter",
    id: savedFilterIds[index]!,
    name,
    mode,
    find_filter: null,
    filter_ast: {
      root: {
        condition: {
          field: mode === FilterMode.SceneMarkers ? "duration" : "title",
          value:
            mode === FilterMode.SceneMarkers
              ? { modifier: CriterionModifier.GreaterThan, value: { value: 1 } }
              : { modifier: CriterionModifier.Includes, value: name },
        },
      },
    },
    ui_options: null,
  }));
  window.savedFilterMutations = [];
  return new ApolloLink((operation, forward) => {
    if (
      !window.savedFiltersFixture ||
      !["FindSavedFilters", "SaveFilter", "DestroySavedFilter"].includes(
        operation.operationName ?? "",
      )
    )
      return forward(operation);
    return new Observable((observer) => {
      const name = operation.operationName ?? "";
      if (name === "DestroySavedFilter") {
        const { input } =
          operation.variables as DestroySavedFilterMutationVariables;
        window.savedFilterMutations.push({ operation: name, id: input.id });
      }
      const respond = () => {
        let data: object;
        if (name === "FindSavedFilters") {
          data = { findSavedFilters: filters };
        } else if (name === "DestroySavedFilter") {
          if (window.savedFilterFailure === "network") {
            observer.error(new Error("Saved filter service unavailable"));
            return;
          }
          const { input } =
            operation.variables as DestroySavedFilterMutationVariables;
          const success = window.savedFilterFailure !== "false";
          if (success)
            filters = filters.filter((filter) => filter.id !== input.id);
          data = { destroySavedFilter: success };
        } else {
          const { input } = operation.variables as SaveFilterMutationVariables;
          window.savedFilterMutations.push({
            operation: name,
            id: input.id ?? undefined,
            name: input.name,
          });
          const filter: SavedFilterDataFragment = {
            __typename: "SavedFilter",
            id: input.id ?? "00000000-0000-4000-8000-000000009003",
            mode: input.mode,
            name: input.name,
            find_filter: input.find_filter
              ? {
                  __typename: "SavedFindFilterType",
                  q: input.find_filter.q ?? null,
                  page: input.find_filter.page ?? null,
                  per_page: input.find_filter.per_page ?? null,
                  sort: input.find_filter.sort ?? null,
                  direction: input.find_filter.direction ?? null,
                }
              : null,
            filter_ast: input.filter_ast ?? null,
            ui_options: input.ui_options ?? null,
          };
          filters = [...filters.filter((f) => f.id !== filter.id), filter];
          data = { saveFilter: filter };
        }
        observer.next({ data });
        observer.complete();
      };
      if (name === "DestroySavedFilter" && window.holdSavedFilterDelete) {
        window.releaseSavedFilterDelete = respond;
        return () => {
          window.releaseSavedFilterDelete = undefined;
        };
      }
      const timer = setTimeout(respond, 0);
      return () => clearTimeout(timer);
    });
  });
}
