import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import type * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import { secondsToTimestamp } from "src/utils/duration";
import {
  selectionColumn,
  thumbnailColumn,
  titleColumn,
  tagsColumn,
} from "src/components/list/table-columns";
import { DataTableColumnHeader } from "src/components/list/data-table-column-header";

type MarkerItem =
  GQL.FindSceneMarkersQuery["findSceneMarkers"]["scene_markers"][number];

function markerTitle(m: MarkerItem): string {
  if (m.title) return m.title;
  const sceneTitle = objectTitle(m.scene);
  return sceneTitle
    ? `${sceneTitle} — ${m.primary_tag.name}`
    : m.primary_tag.name;
}

export const markerTableColumns: ColumnDef<MarkerItem>[] = [
  selectionColumn<MarkerItem>(),

  thumbnailColumn<MarkerItem>(
    (m) => m.screenshot,
    (m) => `/scenes/${m.scene.id}?t=${m.seconds}`,
  ),

  titleColumn<MarkerItem>({
    id: "title",
    header: "Title",
    getTitle: markerTitle,
    getHref: (m) => `/scenes/${m.scene.id}?t=${m.seconds}`,
  }),

  {
    id: "scene_id",
    enableSorting: true,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="Scene" />
    ),
    accessorFn: (m) => objectTitle(m.scene) || m.scene.id,
    cell: ({ row }) => {
      const m = row.original;
      return (
        <Link
          to="/scenes/$sceneId"
          params={{ sceneId: m.scene.id }}
          className="text-xs hover:underline line-clamp-1"
        >
          {objectTitle(m.scene) || m.scene.id}
        </Link>
      );
    },
  },

  {
    id: "seconds",
    accessorFn: (m) => m.seconds,
    enableSorting: true,
    size: 80,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="Timestamp" />
    ),
    cell: ({ row }) => (
      <span className="tabular-nums text-muted-foreground text-xs whitespace-nowrap">
        {secondsToTimestamp(row.original.seconds)}
      </span>
    ),
  },

  {
    id: "tag",
    enableSorting: false,
    header: "Primary Tag",
    accessorFn: (m) => m.primary_tag.name,
    cell: ({ row }) => (
      <Link
        to="/tags/$tagId"
        params={{ tagId: row.original.primary_tag.id }}
        className="text-xs bg-muted rounded px-1 py-0.5 hover:bg-muted/80 whitespace-nowrap"
      >
        {row.original.primary_tag.name}
      </Link>
    ),
  },

  tagsColumn<MarkerItem>({ getTags: (m) => m.tags }),
];
