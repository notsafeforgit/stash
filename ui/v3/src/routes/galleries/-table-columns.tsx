import { useMemo } from "react";
import { useIntl } from "react-intl";
import type { ColumnDef } from "@tanstack/react-table";
import type * as GQL from "src/core/generated-graphql";
import { galleryLabel } from "src/lib/gallery-utils";
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

type GalleryItem = GQL.FindGalleriesQuery["findGalleries"]["galleries"][number];

export function useGalleryTableColumns(): ColumnDef<GalleryItem>[] {
  const intl = useIntl();
  return useMemo(
    () => [
      selectionColumn<GalleryItem>(),

      thumbnailColumn<GalleryItem>(
        (g) => g.paths.cover,
        (g) => `/galleries/${g.id}`,
      ),

      titleColumn<GalleryItem>({
        id: "path",
        header: intl.formatMessage({ id: "title" }),
        getTitle: galleryLabel,
        getHref: (g) => `/galleries/${g.id}`,
      }),

      textColumn<GalleryItem>({
        id: "date",
        header: intl.formatMessage({ id: "date" }),
        getValue: (g) => g.date,
        className:
          "tabular-nums text-muted-foreground text-xs whitespace-nowrap",
      }),

      studioColumn<GalleryItem>({
        getStudio: (g) => g.studio,
        header: intl.formatMessage({ id: "studio" }),
      }),

      numberColumn<GalleryItem>({
        id: "images_count",
        header: intl.formatMessage({ id: "image_count" }),
        getValue: (g) => g.image_count,
      }),

      ratingColumn<GalleryItem>({
        getRating: (g) => g.rating100,
        header: intl.formatMessage({ id: "rating" }),
      }),

      tagsColumn<GalleryItem>({
        getTags: (g) => g.tags,
        header: intl.formatMessage({ id: "tags" }),
      }),
    ],
    [intl],
  );
}
