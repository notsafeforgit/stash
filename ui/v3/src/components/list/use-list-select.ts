import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { IHasID } from "src/utils/data";

export function useListSelect<T extends IHasID = IHasID>(items: T[]) {
  const [itemsSelected, setItemsSelected] = useState<T[]>([]);
  const [lastClickedId, setLastClickedId] = useState<string>();
  const [selecting, setSelecting] = useState(false);

  // Prune selections that are no longer present in `items`. Two paths
  // hit this: (1) bulk-delete shrinks the page, leaving the toolbar's
  // "N selected" counter stale until something forces a re-render; (2)
  // paging swaps the visible items entirely, and cross-page selection
  // isn't supported here (the `applyToAllTarget` pattern covers
  // operations on everything matching the filter). Identity check on
  // the resulting array avoids a setState loop when nothing changes.
  // Also drops `selecting` mode when pruning leaves nothing selected,
  // so the toolbar's bulk-action chrome dismisses cleanly post-delete.
  useEffect(() => {
    const present = new Set(items.map((i) => i.id));
    setItemsSelected((prev) => {
      if (prev.length === 0) return prev;
      const kept = prev.filter((i) => present.has(i.id));
      if (kept.length === prev.length) return prev;
      if (kept.length === 0) setSelecting(false);
      return kept;
    });
  }, [items]);

  const selectedIds = useMemo(() => {
    const set = new Set<string>();
    itemsSelected.forEach((item) => {
      set.add(item.id);
    });
    return set;
  }, [itemsSelected]);

  const selectedIdsRef = useRef(selectedIds);
  selectedIdsRef.current = selectedIds;
  const getSelectedIds = useCallback(() => selectedIdsRef.current, []);

  const itemsSelectedRef = useRef(itemsSelected);
  itemsSelectedRef.current = itemsSelected;
  const getSelectedItems = useCallback(() => itemsSelectedRef.current, []);

  function singleSelect(id: string, selected: boolean) {
    setLastClickedId(id);

    setItemsSelected((prev) => {
      if (selected) {
        if (prev.some((v) => v.id === id)) return prev;
        const item = items.find((i) => i.id === id);
        if (item) return [...prev, item];
        return prev;
      } else {
        return prev.filter((item) => item.id !== id);
      }
    });
  }

  function selectRange(startIndex: number, endIndex: number) {
    let start = startIndex;
    let end = endIndex;
    if (start > end) {
      [start, end] = [end, start];
    }
    const subset = items.slice(start, end + 1);
    const toAdd = subset.filter((item) => !selectedIds.has(item.id));
    setItemsSelected((prev) => prev.concat(toAdd));
  }

  function multiSelect(id: string) {
    let startIndex = 0;
    if (lastClickedId) {
      const idx = items.findIndex((item) => item.id === lastClickedId);
      if (idx !== -1) startIndex = idx;
    }
    const thisIndex = items.findIndex((item) => item.id === id);
    selectRange(startIndex, thisIndex);
  }

  // Stable reference so React.memo children don't re-render on every filter
  // change — the ref always delegates to the latest multiSelect/singleSelect.
  const onSelectChangeImpl =
    useRef<(id: string, selected: boolean, shiftKey: boolean) => void>(
      undefined,
    );
  onSelectChangeImpl.current = (id, selected, shiftKey) => {
    if (shiftKey) {
      multiSelect(id);
    } else {
      singleSelect(id, selected);
    }
  };
  const onSelectChange = useCallback(
    (id: string, selected: boolean, shiftKey: boolean) =>
      onSelectChangeImpl.current!(id, selected, shiftKey),
    [],
  );

  // Same ref-stable pattern as `onSelectChange` above: callers (the
  // list provider, context-menu items) reference these in deps without
  // re-memoising on every render, but the impl always sees the latest
  // `items` snapshot via the ref dance.
  const onSelectAllImpl = useRef<() => void>(undefined);
  onSelectAllImpl.current = () => {
    setItemsSelected((prev) => {
      const selectedSet = new Set(prev.map((item) => item.id));
      const toAdd = items.filter((item) => !selectedSet.has(item.id));
      return [...prev, ...toAdd];
    });
    setLastClickedId(undefined);
  };
  const onSelectAll = useCallback(() => onSelectAllImpl.current!(), []);

  const onSelectNoneImpl = useRef<() => void>(undefined);
  onSelectNoneImpl.current = () => {
    setItemsSelected([]);
    setLastClickedId(undefined);
    setSelecting(false);
  };
  const onSelectNone = useCallback(() => onSelectNoneImpl.current!(), []);

  const onInvertSelectionImpl = useRef<() => void>(undefined);
  onInvertSelectionImpl.current = () => {
    setItemsSelected((prev) => {
      const selectedSet = new Set(prev.map((item) => item.id));
      return items.filter((item) => !selectedSet.has(item.id));
    });
    setLastClickedId(undefined);
  };
  const onInvertSelection = useCallback(
    () => onInvertSelectionImpl.current!(),
    [],
  );

  function onEnterSelect() {
    setSelecting(true);
  }

  const getSelected = useCallback(() => itemsSelected, [itemsSelected]);
  const hasSelection = itemsSelected.length > 0;

  return {
    selectedItems: itemsSelected,
    selectedIds,
    getSelected,
    getSelectedIds,
    getSelectedItems,
    onSelectChange,
    onSelectAll,
    onSelectNone,
    onInvertSelection,
    onEnterSelect,
    hasSelection,
    selecting: selecting || hasSelection,
  };
}

export type IListSelect<T extends IHasID = IHasID> = ReturnType<
  typeof useListSelect<T>
>;
