import * as GQL from "@/core/generated-graphql";
import type { ListFilterModel } from "@/models/list-filter/filter";

export interface ShareSelection {
  kind: GQL.ShareEntityKind;
  id: string;
  name: string;
}

export interface MatchingShareSelection {
  kind: GQL.ShareEntityKind;
  filter: ListFilterModel;
}

export interface ShareEntitySource {
  kind: GQL.ShareLibraryKind;
  id: string;
  name: string;
}

export const allShareMediaTypes = [
  GQL.ShareEntityKind.Scene,
  GQL.ShareEntityKind.Image,
  GQL.ShareEntityKind.Gallery,
];

export function matchingShareSource(
  matching: MatchingShareSelection,
  mode: GQL.ShareMembership,
): GQL.MediaShareSourceInput {
  return {
    mode,
    filter: {
      kind: matching.kind,
      q: matching.filter.searchTerm,
      filter_ast: matching.filter.makeFilterAST(),
    },
  };
}
