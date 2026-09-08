import { useCallback, useMemo, useState } from "react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import type { IHasID } from "src/utils/data";

interface Selection {
  ids: Set<string>;
  anchor?: string;
  selecting: boolean;
}

export function useListSelect<T extends IHasID = IHasID>(items: T[]) {
  const [selection, setSelection] = useState<Selection>(() => ({
    ids: new Set(),
    selecting: false,
  }));
  const byId = useMemo(
    () => new Map(items.map((item) => [item.id, item])),
    [items],
  );
  const selectedItems = useMemo(
    () =>
      Array.from(selection.ids).flatMap((id) => {
        const item = byId.get(id);
        return item ? [item] : [];
      }),
    [selection.ids, byId],
  );
  const selectedIds = useMemo(
    () => new Set(selectedItems.map((item) => item.id)),
    [selectedItems],
  );
  if (selectedIds.size !== selection.ids.size) {
    setSelection({
      ...selection,
      ids: selectedIds,
      selecting: selectedIds.size > 0 && selection.selecting,
    });
  }

  const currentItems = useCommittedRef(items);
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
      if (!visible.some((item) => item.id === id)) return;
      setSelection((previous) => {
        const ids = new Set(previous.ids);
        if (shift) {
          const anchor = Math.max(
            0,
            visible.findIndex((item) => item.id === previous.anchor),
          );
          const end = visible.findIndex((item) => item.id === id);
          for (const item of visible.slice(
            Math.min(anchor, end),
            Math.max(anchor, end) + 1,
          ))
            ids.add(item.id);
        } else if (selected) ids.add(id);
        else ids.delete(id);
        return { ...previous, ids, anchor: shift ? previous.anchor : id };
      });
    },
    [],
  );

  const onSelectAll = useCallback(() => {
    const ids = new Set(currentItems.current.map((item) => item.id));
    setSelection((previous) => ({ ...previous, ids, anchor: undefined }));
  }, []);
  const onSelectNone = useCallback(
    () => setSelection({ ids: new Set(), selecting: false }),
    [],
  );
  const onInvertSelection = useCallback(() => {
    const visible = currentItems.current;
    setSelection((previous) => ({
      ...previous,
      anchor: undefined,
      ids: new Set(
        visible
          .filter((item) => !previous.ids.has(item.id))
          .map((item) => item.id),
      ),
    }));
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
    onEnterSelect,
    hasSelection,
    selecting: selection.selecting || hasSelection,
  };
}

export type IListSelect<T extends IHasID = IHasID> = ReturnType<
  typeof useListSelect<T>
>;
