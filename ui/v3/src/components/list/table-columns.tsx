import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import type { IHasID } from "src/utils/data";
import { DataTableColumnHeader } from "./data-table-column-header";
import { selectionColumn } from "./entity-data-table";

// ── Re-export selection column for convenience ────────────────────────────────
export { selectionColumn };

// ── Tiny thumbnail ────────────────────────────────────────────────────────────

export function thumbnailColumn<T extends IHasID>(
  getImagePath: (row: T) => string | null | undefined,
  getHref: (row: T) => string,
): ColumnDef<T> {
  return {
    id: "thumbnail",
    header: "Thumbnail",
    meta: { label: "Thumbnail" },
    enableSorting: false,
    size: 56,
    minSize: 56,
    maxSize: 56,
    cell: ({ row }) => {
      const src = getImagePath(row.original);
      return (
        <Link to={getHref(row.original) as never} className="block shrink-0">
          {src ? (
            <img
              src={src}
              alt=""
              className="h-10 w-10 rounded object-cover"
              loading="lazy"
            />
          ) : (
            <div className="h-10 w-10 rounded bg-muted" />
          )}
        </Link>
      );
    },
  };
}

// ── Name / Title with link ────────────────────────────────────────────────────

export function titleColumn<T extends IHasID>(opts: {
  id: string;
  header: string;
  getTitle: (row: T) => string;
  getHref: (row: T) => string;
  sortable?: boolean;
}): ColumnDef<T> {
  const { id, header, getTitle, getHref, sortable = true } = opts;
  return {
    id,
    accessorFn: getTitle,
    enableSorting: sortable,
    meta: { label: header },
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={header} />
    ),
    cell: ({ row }) => (
      // line-clamp lives on the wrapper so the link stays inline — without
      // this the link's hover box would span the full cell width (line-clamp
      // applies `display: -webkit-box`, which is block-level).
      <div className="line-clamp-2">
        <Link
          to={getHref(row.original) as never}
          className="font-medium hover:underline"
        >
          {getTitle(row.original)}
        </Link>
      </div>
    ),
  };
}

// ── Simple text column ────────────────────────────────────────────────────────

export function textColumn<T extends IHasID>(opts: {
  id: string;
  header: string;
  getValue: (row: T) => string | null | undefined;
  sortable?: boolean;
  className?: string;
  /** Opt in for copyable values such as file paths. */
  selectableText?: boolean;
}): ColumnDef<T> {
  const {
    id,
    header,
    getValue,
    sortable = true,
    className,
    selectableText = false,
  } = opts;
  return {
    id,
    accessorFn: (row) => getValue(row) ?? "",
    enableSorting: sortable,
    meta: { label: header },
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={header} />
    ),
    cell: ({ row }) => {
      const val = getValue(row.original);
      if (!val) return null;
      return (
        <span
          className={className}
          data-selectable-text={selectableText || undefined}
        >
          {val}
        </span>
      );
    },
  };
}

// ── Number column ─────────────────────────────────────────────────────────────

export function numberColumn<T extends IHasID>(opts: {
  id: string;
  header: string;
  getValue: (row: T) => number | null | undefined;
  format?: (n: number) => string;
  sortable?: boolean;
}): ColumnDef<T> {
  const { id, header, getValue, format, sortable = true } = opts;
  return {
    id,
    accessorFn: (row) => getValue(row) ?? null,
    enableSorting: sortable,
    meta: { label: header },
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={header} />
    ),
    cell: ({ row }) => {
      const val = getValue(row.original);
      if (val == null) return null;
      return (
        <span className="tabular-nums text-muted-foreground text-xs">
          {format ? format(val) : val}
        </span>
      );
    },
  };
}

// ── Rating column ─────────────────────────────────────────────────────────────

export function ratingColumn<T extends IHasID>(opts: {
  getRating: (row: T) => number | null | undefined;
  id?: string;
  header?: string;
}): ColumnDef<T> {
  const { getRating, id = "rating100", header = "Rating" } = opts;
  return {
    id,
    accessorFn: (row) => getRating(row) ?? null,
    enableSorting: true,
    size: 70,
    meta: { label: header },
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={header} />
    ),
    cell: ({ row }) => {
      const rating = getRating(row.original);
      if (rating == null) return null;
      const stars = (rating / 20).toFixed(1);
      return (
        <span
          className="text-xs font-medium text-amber-500"
          title={`${rating}/100`}
        >
          {stars}★
        </span>
      );
    },
  };
}

// ── Tags column ───────────────────────────────────────────────────────────────

export function tagsColumn<T extends IHasID>(opts: {
  getTags: (row: T) => Array<{ id: string; name: string }> | null | undefined;
  header?: string;
}): ColumnDef<T> {
  const { getTags, header = "Tags" } = opts;
  return {
    id: "tags",
    enableSorting: false,
    meta: { label: header },
    header,
    cell: ({ row }) => {
      const tags = getTags(row.original);
      if (!tags?.length) return null;
      return (
        <div className="flex flex-wrap gap-1">
          {tags.slice(0, 5).map((tag) => (
            <Link
              key={tag.id}
              to="/tags/$tagId"
              params={{ tagId: tag.id }}
              className="text-xs bg-muted rounded px-1 py-0.5 hover:bg-muted/80 whitespace-nowrap"
            >
              {tag.name}
            </Link>
          ))}
          {tags.length > 5 && (
            <span className="text-xs text-muted-foreground">
              +{tags.length - 5}
            </span>
          )}
        </div>
      );
    },
  };
}

// ── Performers column ─────────────────────────────────────────────────────────

export function performersColumn<T extends IHasID>(opts: {
  getPerformers: (
    row: T,
  ) => Array<{ id: string; name: string }> | null | undefined;
  header?: string;
}): ColumnDef<T> {
  const { getPerformers, header = "Performers" } = opts;
  return {
    id: "performers",
    enableSorting: false,
    meta: { label: header },
    header,
    cell: ({ row }) => {
      const performers = getPerformers(row.original);
      if (!performers?.length) return null;
      return (
        <div className="flex flex-wrap gap-1">
          {performers.slice(0, 4).map((p) => (
            <Link
              key={p.id}
              to="/performers/$performerId"
              params={{ performerId: p.id }}
              className="text-xs hover:underline whitespace-nowrap"
            >
              {p.name}
            </Link>
          ))}
          {performers.length > 4 && (
            <span className="text-xs text-muted-foreground">
              +{performers.length - 4}
            </span>
          )}
        </div>
      );
    },
  };
}

// ── Studio column ─────────────────────────────────────────────────────────────

export function studioColumn<T extends IHasID>(opts: {
  getStudio: (row: T) => { id: string; name: string } | null | undefined;
  id?: string;
  header?: string;
}): ColumnDef<T> {
  const { getStudio, id = "studio", header = "Studio" } = opts;
  return {
    id,
    accessorFn: (row) => getStudio(row)?.name ?? "",
    enableSorting: true,
    meta: { label: header },
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={header} />
    ),
    cell: ({ row }) => {
      const studio = getStudio(row.original);
      if (!studio) return null;
      return (
        <Link
          to="/studios/$studioId"
          params={{ studioId: studio.id }}
          className="text-xs hover:underline whitespace-nowrap"
        >
          {studio.name}
        </Link>
      );
    },
  };
}
