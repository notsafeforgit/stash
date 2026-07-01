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
import { useConfigurationContextOptional } from "src/hooks/config";

type StudioItem = GQL.FindStudiosQuery["findStudios"]["studios"][number];

export function useStudioTableColumns(): ColumnDef<StudioItem>[] {
  const intl = useIntl();
  const showChildStudioContent =
    useConfigurationContextOptional()?.configuration.ui
      .showChildStudioContent ?? false;
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
        getValue: (s) =>
          showChildStudioContent ? s.scene_count_all : s.scene_count,
      }),

      numberColumn<StudioItem>({
        id: "scene_markers_count",
        header: intl.formatMessage({
          id: "scene_marker_count",
          defaultMessage: "Scene Marker Count",
        }),
        getValue: (s) => s.scene_marker_count,
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

      numberColumn<StudioItem>({
        id: "o_counter",
        header: intl.formatMessage({
          id: "o_count",
          defaultMessage: "O Count",
        }),
        getValue: (s) =>
          showChildStudioContent ? s.o_counter_all : s.o_counter,
      }),

      numberColumn<StudioItem>({
        id: "performer_count",
        header: intl.formatMessage({
          id: "performer_count",
          defaultMessage: "Performer Count",
        }),
        getValue: (s) =>
          showChildStudioContent ? s.performer_count_all : s.performer_count,
      }),

      tagsColumn<StudioItem>({
        getTags: (s) => s.tags,
        header: intl.formatMessage({ id: "tags" }),
      }),
    ],
    [intl, showChildStudioContent],
  );
}
