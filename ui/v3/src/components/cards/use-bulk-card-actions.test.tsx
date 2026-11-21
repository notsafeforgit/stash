// @vitest-environment jsdom
import { act, memo, useMemo, type PropsWithChildren } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { createListItemsContext } from "@/components/list/list-items-context";
import { ListStateContext } from "@/components/list/list-provider";
import {
  BulkSelectionActionsProvider,
  useSelectionDialogs,
} from "@/components/list/selection-actions";
import { useListSelect } from "@/components/list/use-list-select";
import { useBulkCardActions } from "./use-bulk-card-actions";

type Item = { id: string; title: string };

it("resolves bulk actions across pages without subscribing every card to selection", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const root = createRoot(document.createElement("div"));
  const Items = createListItemsContext<Item>();
  let selection: ReturnType<typeof useListSelect<Item>>;
  let dialogs: ReturnType<typeof useSelectionDialogs<Item>>;
  let card: ReturnType<typeof useBulkCardActions>;
  let cardRenders = 0;
  const Card = memo(function Card() {
    cardRenders++;
    card = useBulkCardActions();
    return null;
  });
  function Actions({ children }: PropsWithChildren) {
    const result = useSelectionDialogs(
      Items.useItems(),
      Items.useSelectionGetter(),
    );
    dialogs = result;
    const { open } = result;
    const actions = useMemo(
      () => ({
        noun: "items",
        onEdit: () => open("edit"),
        onDelete: () => open("delete"),
      }),
      [open],
    );
    return (
      <BulkSelectionActionsProvider value={actions}>
        {children}
      </BulkSelectionActionsProvider>
    );
  }
  function Page({ items, page }: { items: Item[]; page: string }) {
    const select = useListSelect(items, { pageKey: page });
    selection = select;
    const { getSelectedIds, onSelectAll, onSelectNone, onRemoveSelection } =
      select;
    const list = useMemo(
      () => ({
        selectable: true,
        totalCount: undefined,
        getSelectedIds,
        onSelectAll,
        onSelectNone,
        onRemoveSelection,
      }),
      [getSelectedIds, onSelectAll, onSelectNone, onRemoveSelection],
    );
    return (
      <Items.Provider items={items} getSelectedItems={select.getSelectedItems}>
        <ListStateContext.Provider value={list}>
          <Actions>
            <Card />
          </Actions>
        </ListStateContext.Provider>
      </Items.Provider>
    );
  }
  try {
    const first = { id: "1", title: "first" };
    const second = { id: "2", title: "second" };
    await act(async () => root.render(<Page items={[first]} page="1" />));
    const initialRenders = cardRenders;
    await act(async () => selection.onSelectAll());
    expect(cardRenders).toBe(initialRenders);
    await act(async () => card.onContextMenuOpen());
    expect(card!.bulkActions).toBeUndefined();
    await act(async () => root.render(<Page items={[second]} page="2" />));
    await act(async () => selection.onSelectAll());
    await act(async () => card.onContextMenuOpen());
    expect(card!.bulkCount).toBe(2);
    await act(async () => card.bulkActions?.onEdit());
    expect(dialogs!.selectedItems).toEqual([first, second]);
    expect(dialogs!.dialogProps("edit").open).toBe(true);
    await act(async () =>
      root.render(<Page items={[{ ...second, title: "edited" }]} page="2" />),
    );
    expect(dialogs!.selectedItems).toEqual([
      first,
      { ...second, title: "edited" },
    ]);
    // The open dialog keeps its items after the selection empties.
    await act(async () => selection.onSelectNone());
    expect(dialogs!.selectedItems).toHaveLength(2);
    await act(async () => card.onContextMenuOpen());
    expect(card!.bulkActions).toBeUndefined();
  } finally {
    await act(async () => root.unmount());
    vi.unstubAllGlobals();
  }
});
