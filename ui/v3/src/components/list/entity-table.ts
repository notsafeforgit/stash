import {
  type Column,
  type ColumnDef,
  type ReactTable,
  type RowData,
  columnOrderingFeature,
  columnSizingFeature,
  columnVisibilityFeature,
  rowSelectionFeature,
  rowSortingFeature,
  tableFeatures,
} from "@tanstack/react-table";

const columnMeta: { label?: string } = {};

// Sorting and pagination happen on the server. Register only the features
// used by the shared entity table instead of loading the v8 compatibility API.
export const entityTableFeatures = tableFeatures({
  columnOrderingFeature,
  columnSizingFeature,
  columnVisibilityFeature,
  rowSelectionFeature,
  rowSortingFeature,
  columnMeta,
});

export type EntityColumnDef<
  TData extends RowData,
  TValue = unknown,
> = ColumnDef<typeof entityTableFeatures, TData, TValue>;
export type EntityColumn<TData extends RowData, TValue = unknown> = Column<
  typeof entityTableFeatures,
  TData,
  TValue
>;
export type EntityTable<TData extends RowData> = ReactTable<
  typeof entityTableFeatures,
  TData
>;
