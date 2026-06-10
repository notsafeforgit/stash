import { useMemo } from "react";
import { useIntl } from "react-intl";
import type { ColumnDef } from "@tanstack/react-table";
import type * as GQL from "src/core/generated-graphql";
import {
  selectionColumn,
  thumbnailColumn,
  titleColumn,
  textColumn,
  numberColumn,
  ratingColumn,
  tagsColumn,
  studioColumn,
} from "src/components/list/table-columns";

type GroupItem = GQL.FindGroupsQuery["findGroups"]["groups"][number];

export function useGroupTableColumns(): ColumnDef<GroupItem>[] {
  const intl = useIntl();
  return useMemo(
    () => [
      selectionColumn<GroupItem>(),

      thumbnailColumn<GroupItem>(
        (g) => g.front_image_path,
        (g) => `/groups/${g.id}`,
      ),

      titleColumn<GroupItem>({
        id: "name",
        header: intl.formatMessage({ id: "name" }),
        getTitle: (g) => g.name,
        getHref: (g) => `/groups/${g.id}`,
      }),

      textColumn<GroupItem>({
        id: "date",
        header: intl.formatMessage({ id: "date" }),
        getValue: (g) => g.date,
        className:
          "tabular-nums text-muted-foreground text-xs whitespace-nowrap",
      }),

      studioColumn<GroupItem>({
        getStudio: (g) => g.studio,
        header: intl.formatMessage({ id: "studio" }),
      }),

      numberColumn<GroupItem>({
        id: "scenes_count",
        header: intl.formatMessage({ id: "scene_count" }),
        getValue: (g) => g.scene_count,
      }),

      numberColumn<GroupItem>({
        id: "sub_group_count",
        header: intl.formatMessage({ id: "sub_group_count" }),
        getValue: (g) => g.sub_group_count,
      }),

      ratingColumn<GroupItem>({
        getRating: (g) => g.rating100,
        header: intl.formatMessage({ id: "rating" }),
      }),

      tagsColumn<GroupItem>({
        getTags: (g) => g.tags,
        header: intl.formatMessage({ id: "tags" }),
      }),
    ],
    [intl],
  );
}
