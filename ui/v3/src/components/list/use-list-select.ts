import { useCallback, useMemo, useState } from "react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import type { IHasID } from "@/utils/data";

interface Selection<T> {
  items: Map<string, T>;
  scope: string;
  pageKey: string;
  pageItems: T[];
  anchor?: string;
  selecting: boolean;
}

interface SelectionOptions {
  /** Membership scope, including locked filters; excludes sort and pagination. */
  scope?: string;
  /** Identifies the displayed page and ordering, so refreshes can prune removals. */
  pageKey?: string;
  loading?: boolean;
}

export function useListSelect<T extends IHasID = IHasID>(
  items: T[],
  { scope = "", pageKey = "", loading = false }: SelectionOptions = {},
) {
  const [saved, setSelection] = useState<Selection<T>>(() => ({
    items: new Map(),
    scope,
    pageKey,
    pageItems: items,
    selecting: false,
  }));
  let selection = saved;
  if (saved.scope !== scope) {
    selection = {
      items: new Map(),
      scope,
      pageKey,
      pageItems: items,
      selecting: false,
    };
    setSelection(selection);
  } else if (
    !loading &&
    (saved.pageKey !== pageKey || saved.pageItems !== items)
  ) {
    const selected = new Map(saved.items);
    const visible = new Map(items.map((item) => [item.id, item]));
    // An authoritative refresh can remove rows from this page. A page/sort
    // change says nothing about the continued existence of off-page selections.
    if (saved.pageKey === pageKey) {
      for (const item of saved.pageItems) {
        if (!visible.has(item.id)) selected.delete(item.id);
      }
    }
    for (const [id, item] of visible) {
      if (selected.has(id)) selected.set(id, item);
    }
    selection = {
      ...saved,
      items: selected,
      pageKey,
      pageItems: items,
      anchor: saved.pageKey === pageKey ? saved.anchor : undefined,
      selecting:
        saved.items.size > 0 && selected.size === 0 ? false : saved.selecting,
    };
    setSelection(selection);
  }
  const selectedItems = useMemo(
    () => [...selection.items.values()],
    [selection.items],
  );
  const selectedIds = useMemo(
    () => new Set(selection.items.keys()),
    [selection.items],
  );
  const currentItems = useCommittedRef(loading ? [] : items);
  const currentSelection = useCommittedRef({ selectedItems, selectedIds });
  const getSelectedIds = useCallback(
    () => currentSelection.current.selectedIds,
    [],
  );
  const getSelectedItems = useCallback(
    () => currentSelection.current.selectedItems,
    [],
  );

  const onSelectChange = useCallback(
    (id: string, selected: boolean, shift: boolean) => {
      const visible = currentItems.current;
      const item = visible.find((value) => value.id === id);
      if (!item) return;
      setSelection((previous) => {
        const next = new Map(previous.items);
        if (shift) {
          const anchor = Math.max(
            0,
            visible.findIndex((value) => value.id === previous.anchor),
          );
          const end = visible.findIndex((value) => value.id === id);
          for (const value of visible.slice(
            Math.min(anchor, end),
            Math.max(anchor, end) + 1,
          ))
            next.set(value.id, value);
        } else if (selected) next.set(id, item);
        else next.delete(id);
        return {
          ...previous,
          items: next,
          anchor: shift ? previous.anchor : id,
        };
      });
    },
    [],
  );

  const onSelectAll = useCallback(() => {
    const visible = currentItems.current;
    setSelection((previous) => ({
      ...previous,
      items: new Map([
        ...previous.items,
        ...visible.map((item) => [item.id, item] as const),
      ]),
      anchor: undefined,
    }));
  }, []);
  const onSelectNone = useCallback(
    () =>
      setSelection((previous) => ({
        ...previous,
        items: new Map(),
        selecting: false,
        anchor: undefined,
      })),
    [],
  );
  const onInvertSelection = useCallback(() => {
    const visible = currentItems.current;
    setSelection((previous) => {
      const next = new Map(previous.items);
      for (const item of visible) {
        if (next.has(item.id)) next.delete(item.id);
        else next.set(item.id, item);
      }
      return { ...previous, items: next, anchor: undefined };
    });
  }, []);
  const onRemoveSelection = useCallback((ids: readonly string[]) => {
    setSelection((previous) => {
      if (!ids.some((id) => previous.items.has(id))) return previous;
      const next = new Map(previous.items);
      for (const id of ids) next.delete(id);
      return {
        ...previous,
        items: next,
        selecting: next.size > 0 && previous.selecting,
      };
    });
  }, []);
  const onEnterSelect = useCallback(
    () => setSelection((previous) => ({ ...previous, selecting: true })),
    [],
  );
  const hasSelection = selectedIds.size > 0;

  return {
    selectedItems,
    selectedIds,
    getSelected: getSelectedItems,
    getSelectedIds,
    getSelectedItems,
    onSelectChange,
    onSelectAll,
    onSelectNone,
    onInvertSelection,
    onRemoveSelection,
    onEnterSelect,
    hasSelection,
    selecting: selection.selecting || hasSelection,
  };
}

export type IListSelect<T extends IHasID = IHasID> = ReturnType<
  typeof useListSelect<T>
>;
