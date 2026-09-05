import { useMemo } from "react";
import { useIntl } from "react-intl";
import type { ColumnDef } from "@tanstack/react-table";
import type * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import { secondsToTimestamp } from "src/utils/duration";
import {
  selectionColumn,
  thumbnailColumn,
  titleColumn,
  textColumn,
  numberColumn,
  ratingColumn,
  tagsColumn,
  performersColumn,
  studioColumn,
} from "src/components/list/table-columns";
import { DataTableColumnHeader } from "src/components/list/data-table-column-header";

type SceneItem = GQL.SlimSceneDataFragment;

function formatResolution(width: number, height: number): string {
  if (height >= 2160) return "4K";
  if (height >= 1440) return "2K";
  if (height >= 1080) return "1080p";
  if (height >= 720) return "720p";
  if (height >= 480) return "480p";
  return `${width}×${height}`;
}

function formatFilesize(bytes: number | undefined): string {
  if (!bytes) return "";
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`;
  return `${(bytes / 1e3).toFixed(0)} KB`;
}

export function useSceneTableColumns(): ColumnDef<SceneItem>[] {
  const intl = useIntl();
  return useMemo(
    () => [
      selectionColumn<SceneItem>(),

      thumbnailColumn<SceneItem>(
        (scene) => scene.paths.webp ?? scene.paths.screenshot,
        (scene) => `/scenes/${scene.id}`,
      ),

      titleColumn<SceneItem>({
        id: "title",
        header: intl.formatMessage({ id: "title" }),
        getTitle: (scene) => objectTitle(scene),
        getHref: (scene) => `/scenes/${scene.id}`,
      }),

      textColumn<SceneItem>({
        id: "date",
        header: intl.formatMessage({ id: "date" }),
        getValue: (scene) => scene.date,
        className:
          "tabular-nums text-muted-foreground text-xs whitespace-nowrap",
      }),

      {
        id: "duration",
        accessorFn: (scene) => scene.files[0]?.duration ?? null,
        enableSorting: true,
        size: 80,
        meta: { label: intl.formatMessage({ id: "duration" }) },
        header: ({ column }) => (
          <DataTableColumnHeader
            column={column}
            title={intl.formatMessage({ id: "duration" })}
          />
        ),
        cell: ({ row }) => {
          const duration = row.original.files[0]?.duration;
          if (!duration) return null;
          return (
            <span className="tabular-nums text-muted-foreground text-xs whitespace-nowrap">
              {secondsToTimestamp(duration)}
            </span>
          );
        },
      },

      studioColumn<SceneItem>({
        getStudio: (scene) => scene.studio,
        header: intl.formatMessage({ id: "studio" }),
      }),

      performersColumn<SceneItem>({
        getPerformers: (scene) => scene.performers,
        header: intl.formatMessage({ id: "performers" }),
      }),

      {
        id: "resolution",
        enableSorting: true,
        size: 70,
        meta: { label: intl.formatMessage({ id: "resolution" }) },
        header: ({ column }) => (
          <DataTableColumnHeader
            column={column}
            title={intl.formatMessage({ id: "resolution" })}
          />
        ),
        accessorFn: (scene) => {
          const f = scene.files[0];
          return f?.height ?? null;
        },
        cell: ({ row }) => {
          const f = row.original.files[0];
          if (!f?.width || !f?.height) return null;
          return (
            <span className="text-xs text-muted-foreground whitespace-nowrap">
              {formatResolution(f.width, f.height)}
            </span>
          );
        },
      },

      {
        id: "filesize",
        enableSorting: true,
        size: 80,
        meta: { label: intl.formatMessage({ id: "filesize" }) },
        header: ({ column }) => (
          <DataTableColumnHeader
            column={column}
            title={intl.formatMessage({ id: "filesize" })}
          />
        ),
        accessorFn: (scene) => scene.files[0]?.size ?? null,
        cell: ({ row }) => {
          const size = row.original.files[0]?.size;
          if (!size) return null;
          return (
            <span className="text-xs tabular-nums text-muted-foreground whitespace-nowrap">
              {formatFilesize(size)}
            </span>
          );
        },
      },

      ratingColumn<SceneItem>({
        getRating: (scene) => scene.rating100,
        header: intl.formatMessage({ id: "rating" }),
      }),

      numberColumn<SceneItem>({
        id: "play_count",
        header: intl.formatMessage({ id: "play_count" }),
        getValue: (scene) => scene.play_count ?? null,
      }),

      tagsColumn<SceneItem>({
        getTags: (scene) => scene.tags,
        header: intl.formatMessage({ id: "tags" }),
      }),

      textColumn<SceneItem>({
        id: "path",
        selectableText: true,
        header: intl.formatMessage({ id: "path" }),
        getValue: (scene) => scene.files[0]?.path ?? null,
        className: "text-xs text-muted-foreground font-mono truncate max-w-xs",
      }),
    ],
    [intl],
  );
}
