// @vitest-environment jsdom
import { act, memo } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { createListItemsContext } from "@/components/list/list-items-context";
import { useListSelect } from "@/components/list/use-list-select";
import { useBulkCardActions } from "./use-bulk-card-actions";

type Item = { id: string; title: string };

it("resolves bulk actions across pages without subscribing every card to selection", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const root = createRoot(document.createElement("div"));
  const Items = createListItemsContext<Item>();
  let selection: ReturnType<typeof useListSelect<Item>>;
  let actions: ReturnType<typeof useBulkCardActions<Item>>;
  let menuRenders = 0;
  const Menu = memo(function Menu() {
    menuRenders++;
    actions = useBulkCardActions(Items.useItems(), Items.useSelectionGetter());
    return null;
  });
  function Page({ items, page }: { items: Item[]; page: string }) {
    selection = useListSelect(items, { pageKey: page });
    return (
      <Items.Provider
        items={items}
        getSelectedItems={selection.getSelectedItems}
      >
        <Menu />
      </Items.Provider>
    );
  }
  try {
    const first = { id: "1", title: "first" };
    const second = { id: "2", title: "second" };
    await act(async () => root.render(<Page items={[first]} page="1" />));
    const initialRenders = menuRenders;
    await act(async () => selection.onSelectAll());
    expect(menuRenders).toBe(initialRenders);
    await act(async () => root.render(<Page items={[second]} page="2" />));
    await act(async () => selection.onSelectAll());
    await act(async () => actions.onContextMenuOpen());
    expect(actions!.selectedItems).toEqual([first, second]);
    expect(actions!.showBulkActions).toBe(true);
    await act(async () =>
      root.render(<Page items={[{ ...second, title: "edited" }]} page="2" />),
    );
    expect(actions!.selectedItems).toEqual([
      first,
      { ...second, title: "edited" },
    ]);
    await act(async () => selection.onRemoveSelection([first.id]));
    await act(async () => actions.onContextMenuOpen());
    expect(actions!.bulkCount).toBe(1);
    await act(async () => selection.onSelectNone());
    await act(async () => actions.onContextMenuOpen());
    expect(actions!.selectedItems).toEqual([]);
  } finally {
    await act(async () => root.unmount());
    vi.unstubAllGlobals();
  }
});
