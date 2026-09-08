import React from "react";

import type * as GQL from "src/core/generated-graphql";

/** Target for "apply to all items matching filter" bulk operations. */
export type BulkApplyTarget = {
  findFilter: GQL.FindFilterType;
  filterAST?: GQL.FilterAstInput;
};

/**
 * Context shape for list pages. Intentionally omits reactive `selectedIds` /
 * `selectedItems` so that selection changes do NOT trigger re-renders in every
 * card. Cards that need selection state (e.g. for bulk context-menu items)
 * read it imperatively via the stable getter functions.
 */
export type ListContextState = {
  selectable: boolean;
  totalCount: number;
  applyToAllTarget?: BulkApplyTarget;
  /** Stable getter — returns the current selected-id set without subscribing. */
  getSelectedIds: () => Set<string>;
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

const emptyState: ListContextState = {
  selectable: false,
  totalCount: 0,
  applyToAllTarget: undefined,
  getSelectedIds: () => new Set(),
  onSelectAll: () => {},
  onSelectNone: () => {},
};

export function useListContextOptional() {
  return React.useContext(ListStateContext) ?? emptyState;
}
