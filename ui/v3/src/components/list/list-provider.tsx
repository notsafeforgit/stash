import React from "react";
import { useListSelect } from "./use-list-select";

import type { IHasID } from "src/utils/data";
import type * as GQL from "src/core/generated-graphql";

interface ListContextOptions<T extends IHasID> {
  selectable?: boolean;
  items: T[];
}

/** Target for "apply to all items matching filter" bulk operations. */
export type BulkApplyTarget = {
  findFilter: GQL.FindFilterType;
  objectFilter: unknown;
};

/**
 * Context shape for list pages. Intentionally omits reactive `selectedIds` /
 * `selectedItems` so that selection changes do NOT trigger re-renders in every
 * card. Cards that need selection state (e.g. for bulk context-menu items)
 * read it imperatively via the stable getter functions.
 */
export type ListContextState<T extends IHasID = IHasID> = {
  selectable: boolean;
  items: T[];
  totalCount: number;
  applyToAllTarget?: BulkApplyTarget;
  /** Stable getter — returns the current selected-id set without subscribing. */
  getSelectedIds: () => Set<string>;
  /** Stable getter — returns the current selected-items array without subscribing. */
  getSelectedItems: () => T[];
  /** Select every item on the current page. Stable identity (ref-backed
   *  in useListSelect) so consuming menus can include it in deps
   *  without re-memoising on every render. */
  onSelectAll: () => void;
  /** Clear all selections. Stable identity. */
  onSelectNone: () => void;
};

export const ListStateContext = React.createContext<ListContextState | null>(
  null,
);

export const ListContext = <T extends IHasID = IHasID>(
  props: ListContextOptions<T> & {
    children?: ((props: ListContextState) => React.ReactNode) | React.ReactNode;
  },
) => {
  const { selectable = false, items, children } = props;

  const listSelect = useListSelect(items);

  const state: ListContextState<T> = {
    selectable,
    items,
    totalCount: items.length,
    getSelectedIds: listSelect.getSelectedIds,
    getSelectedItems: listSelect.getSelectedItems,
    onSelectAll: listSelect.onSelectAll,
    onSelectNone: listSelect.onSelectNone,
  };

  return (
    <ListStateContext.Provider value={state}>
      {typeof children === "function"
        ? (children as (props: ListContextState) => React.ReactNode)(state)
        : children}
    </ListStateContext.Provider>
  );
};

export function useListContext<T extends IHasID = IHasID>() {
  const context = React.useContext(ListStateContext);

  if (context === null) {
    throw new Error("useListContext must be used within a ListStateContext");
  }

  return context as ListContextState<T>;
}

const emptyState: ListContextState = {
  selectable: false,
  items: [],
  totalCount: 0,
  applyToAllTarget: undefined,
  getSelectedIds: () => new Set(),
  getSelectedItems: () => [],
  onSelectAll: () => {},
  onSelectNone: () => {},
};

export function useListContextOptional<T extends IHasID = IHasID>() {
  const context = React.useContext(ListStateContext);

  if (context === null) {
    return emptyState as ListContextState<T>;
  }

  return context as ListContextState<T>;
}
