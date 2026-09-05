import type React from "react";
import type { OperationVariables } from "@apollo/client";
import type { TypedDocumentNode } from "@graphql-typed-document-node/core";
import type { ColumnDef } from "@tanstack/react-table";
import type * as GQL from "@/core/generated-graphql";
import type { IHasID } from "@/utils/data";
import type { ListFilterModel } from "@/models/list-filter/filter";
import type { ISortByOption } from "@/models/list-filter/filter-options";
import type { View } from "./views";

/**
 * Imperative handle for page navigation, populated by EntityListPage on a ref
 * provided through EntityListPageConfig.pageNavRef.
 */
export interface PageNavHandle {
  currentPage: number;
  totalPages: number;
  itemsPerPage: number;
  totalCount: number;
  nextPage: () => void;
  prevPage: () => void;
}

export interface LocalDataSource<TItem extends IHasID> {
  kind: "local";
  /** Raw items prior to filter / sort / pagination. */
  items: TItem[];
  /**
   * Project the raw items down to the current page using the live
   * `ListFilterModel`. Implementations apply search, sort, and
   * pagination locally — the chrome calls this once per debounced
   * filter change.
   */
  filter: (
    items: TItem[],
    filter: ListFilterModel,
  ) => { count: number; items: TItem[] };
  /** Optional loading flag (e.g. while the initial IDB scan settles). */
  loading?: boolean;
}

export interface GraphQLDataSource<
  TData,
  TItem extends IHasID,
  TVariables extends OperationVariables,
> {
  kind: "graphql";
  query: TypedDocumentNode<TData, TVariables>;
  makeVariables: (filter: ListFilterModel) => TVariables;
  extractResult: (data: TData | undefined) => { count: number; items: TItem[] };
}

export type ListDataSource<
  TData,
  TItem extends IHasID,
  TVariables extends OperationVariables,
> = GraphQLDataSource<TData, TItem, TVariables> | LocalDataSource<TItem>;

export interface EntityListPageConfig<
  TData,
  TItem extends IHasID,
  TVariables extends OperationVariables,
> {
  filterMode: GQL.FilterMode;
  view?: View;
  defaultSort?: string;
  source: ListDataSource<TData, TItem, TVariables>;
  /**
   * Sidebar override. When set, replaces the default GraphQL
   * `FilterBuilder`. The offline view passes a custom sidebar with
   * the small subset of filters that apply to local data (status,
   * studio, performers, tags) instead of the full criterion set.
   */
  sidebarContent?: React.ReactNode;
  /**
   * Sort dropdown override. When set, the toolbar shows these sort
   * options instead of `ListFilterModel.options.sortByOptions`.
   * Used by the offline list to expose only the sort keys it can
   * actually compute locally (downloaded_at, title, date, duration,
   * filesize, resolution).
   */
  sortOptions?: ISortByOption[];
  /**
   * Override the empty-state UI shown when the data source returns
   * zero items. Defaults to a generic "No results" `<Empty>` block.
   * Pages with a meaningfully different empty case (e.g. offline:
   * "no scenes downloaded yet") supply their own.
   */
  emptyState?: React.ReactNode;
  renderCard: (
    item: TItem,
    isMobile: boolean,
    selected: boolean,
    onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    onPreviewClick?: () => void,
  ) => React.ReactNode;
  /** Called when the preview area of a card is clicked/tapped. Receives the
   *  clicked item, all currently-displayed items, and the item's index. */
  onCardPreviewClick?: (item: TItem, allItems: TItem[], index: number) => void;
  zoomable?: boolean;
  /** When true, skeleton cards use portrait (2:3) aspect ratio instead of landscape (16:9). */
  cardIsPortrait?: boolean;
  /** Column definitions for the Table display mode. When provided, Table mode is available. */
  tableColumns?: ColumnDef<TItem>[];
  /** localStorage key for persisting column visibility (e.g. "scenes"). Defaults to filterMode. */
  tableVisibilityKey?: string;
  /**
   * Optional per-row wrapper for Table display mode. Receives the item, the
   * default `<TableRow>` JSX, and a per-row select callback (so the wrapped
   * context menu's "Select" item can put the user into select mode without
   * the table having to render the checkbox column up-front). Returns a
   * wrapped node — typically a `ContextMenu` matching the card view.
   */
  renderTableRow?: (
    item: TItem,
    defaultRow: React.ReactElement,
    onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
  ) => React.ReactNode;
  /**
   * Show a portrait/landscape/auto aspect-ratio toggle in grid mode.
   * Defaults to `true`. Set to `false` to suppress the toggle for a specific
   * entity list.
   */
  supportsCardAspect?: boolean;
  /**
   * When provided, a tagger mode button appears in the mobile bar and tagger
   * panel is shown when active. Receives the current page's items.
   */
  renderTagger?: (items: TItem[]) => React.ReactNode;
  /**
   * Optional ref that EntityListPage will populate with page-navigation helpers.
   * Use this to advance pages imperatively (e.g. from a lightbox).
   */
  pageNavRef?: { current: PageNavHandle | null };
  /**
   * Called whenever a new batch of card items arrives from the server
   * (fires on every data change except the initial load). Use this together
   * with pageNavRef to update a lightbox's slides after a page advance.
   */
  onItemsChanged?: (items: TItem[]) => void;
  /**
   * Returns the natural pixel dimensions of an item for wall layout.
   * Used by react-photo-album to compute justified row heights.
   * When omitted, wall mode falls back to 16:9.
   */
  getWallDimensions?: (item: TItem) => { width: number; height: number };
  /**
   * Page-level action(s) rendered at the right end of the desktop chrome
   * bar (e.g. a "+ New" button). Hidden in selection mode.
   */
  pageActions?: React.ReactNode;
}
