import { useMemo } from "react";
import { useIntl } from "react-intl";
import type { ColumnDef } from "@tanstack/react-table";
import type * as GQL from "src/core/generated-graphql";
import { imageTitle } from "src/core/files";
import {
  selectionColumn,
  thumbnailColumn,
  titleColumn,
  textColumn,
  ratingColumn,
  tagsColumn,
  studioColumn,
} from "src/components/list/table-columns";

type ImageItem = GQL.FindImagesQuery["findImages"]["images"][number];

export function useImageTableColumns(): ColumnDef<ImageItem>[] {
  const intl = useIntl();
  return useMemo(
    () => [
      selectionColumn<ImageItem>(),

      thumbnailColumn<ImageItem>(
        (img) => img.paths.thumbnail,
        (img) => `/images/${img.id}`,
      ),

      titleColumn<ImageItem>({
        id: "title",
        header: intl.formatMessage({ id: "title" }),
        getTitle: imageTitle,
        getHref: (img) => `/images/${img.id}`,
      }),

      textColumn<ImageItem>({
        id: "date",
        header: intl.formatMessage({ id: "date" }),
        getValue: (img) => img.date,
        className:
          "tabular-nums text-muted-foreground text-xs whitespace-nowrap",
      }),

      studioColumn<ImageItem>({
        getStudio: (img) => img.studio,
        header: intl.formatMessage({ id: "studio" }),
      }),

      ratingColumn<ImageItem>({
        getRating: (img) => img.rating100,
        header: intl.formatMessage({ id: "rating" }),
      }),

      tagsColumn<ImageItem>({
        getTags: (img) => img.tags,
        header: intl.formatMessage({ id: "tags" }),
      }),

      textColumn<ImageItem>({
        id: "path",
        header: intl.formatMessage({ id: "path" }),
        getValue: (img) => img.visual_files[0]?.path ?? null,
        className: "text-xs text-muted-foreground font-mono truncate max-w-xs",
      }),
    ],
    [intl],
  );
}
