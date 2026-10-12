import * as GQL from "./generated-graphql";
import { getClient } from "./client";

export const queryFindSubFolders = (id: string, excludeZipFolders?: boolean) =>
  getClient().query<GQL.FindFoldersForQueryQuery>({
    query: GQL.FindFoldersForQueryDocument,
    variables: {
      folder_filter_ast: {
        root: {
          group: {
            operator: GQL.FilterGroupOperator.And,
            children: [
              {
                condition: {
                  field: "parent_folder",
                  value: {
                    value: [id],
                    modifier: GQL.CriterionModifier.Equals,
                  },
                },
              },
              ...(excludeZipFolders
                ? [
                    {
                      condition: {
                        field: "zip_file",
                        value: { modifier: GQL.CriterionModifier.IsNull },
                      },
                    },
                  ]
                : []),
            ],
          },
        },
      },
      filter: {
        per_page: -1,
        sort: "path",
        direction: GQL.SortDirectionEnum.Asc,
      },
    },
  });
