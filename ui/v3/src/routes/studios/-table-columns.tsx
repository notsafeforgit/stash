import { useMemo } from "react";
import { useIntl } from "react-intl";
import type { ColumnDef } from "@tanstack/react-table";
import type * as GQL from "src/core/generated-graphql";
import {
  selectionColumn,
  thumbnailColumn,
  titleColumn,
  numberColumn,
  ratingColumn,
  tagsColumn,
} from "src/components/list/table-columns";

type StudioItem = GQL.FindStudiosQuery["findStudios"]["studios"][number];

export function useStudioTableColumns(): ColumnDef<StudioItem>[] {
  const intl = useIntl();
  return useMemo(
    () => [
      selectionColumn<StudioItem>(),

      thumbnailColumn<StudioItem>(
        (s) => s.image_path,
        (s) => `/studios/${s.id}`,
      ),

      titleColumn<StudioItem>({
        id: "name",
        header: intl.formatMessage({ id: "name" }),
        getTitle: (s) => s.name,
        getHref: (s) => `/studios/${s.id}`,
      }),

      numberColumn<StudioItem>({
        id: "scenes_count",
        header: intl.formatMessage({ id: "scene_count" }),
        getValue: (s) => s.scene_count,
      }),

      numberColumn<StudioItem>({
        id: "child_count",
        header: intl.formatMessage({ id: "subsidiary_studios" }),
        getValue: (s) => s.child_studios.length,
        sortable: false,
      }),

      ratingColumn<StudioItem>({
        getRating: (s) => s.rating100,
        header: intl.formatMessage({ id: "rating" }),
      }),

      tagsColumn<StudioItem>({
        getTags: (s) => s.tags,
        header: intl.formatMessage({ id: "tags" }),
      }),
    ],
    [intl],
  );
}
