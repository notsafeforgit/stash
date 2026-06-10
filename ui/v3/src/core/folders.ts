import * as GQL from "./generated-graphql";
import { getClient } from "./client";

export const queryFindSubFolders = (id: string, excludeZipFolders?: boolean) =>
  getClient().query<GQL.FindFoldersForQueryQuery>({
    query: GQL.FindFoldersForQueryDocument,
    variables: {
      folder_filter: {
        parent_folder: { value: id, modifier: GQL.CriterionModifier.Equals },
        zip_file: excludeZipFolders
          ? { modifier: GQL.CriterionModifier.IsNull }
          : undefined,
      },
      filter: {
        per_page: -1,
        sort: "basename",
        direction: GQL.SortDirectionEnum.Asc,
      },
    },
  });
