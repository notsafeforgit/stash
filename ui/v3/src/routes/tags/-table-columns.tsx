import { useMemo } from "react";
import { useIntl } from "react-intl";
import type { ColumnDef } from "@tanstack/react-table";
import type * as GQL from "src/core/generated-graphql";
import {
  selectionColumn,
  thumbnailColumn,
  titleColumn,
  numberColumn,
} from "src/components/list/table-columns";

type TagItem = GQL.FindTagsQuery["findTags"]["tags"][number];

export function useTagTableColumns(): ColumnDef<TagItem>[] {
  const intl = useIntl();
  return useMemo(
    () => [
      selectionColumn<TagItem>(),

      thumbnailColumn<TagItem>(
        (t) => t.image_path,
        (t) => `/tags/${t.id}`,
      ),

      titleColumn<TagItem>({
        id: "name",
        header: intl.formatMessage({ id: "name" }),
        getTitle: (t) => t.name,
        getHref: (t) => `/tags/${t.id}`,
      }),

      numberColumn<TagItem>({
        id: "scenes_count",
        header: intl.formatMessage({ id: "scene_count" }),
        getValue: (t) => t.scene_count,
      }),

      numberColumn<TagItem>({
        id: "images_count",
        header: intl.formatMessage({ id: "image_count" }),
        getValue: (t) => t.image_count,
      }),

      numberColumn<TagItem>({
        id: "galleries_count",
        header: intl.formatMessage({ id: "gallery_count" }),
        getValue: (t) => t.gallery_count,
      }),
    ],
    [intl],
  );
}
