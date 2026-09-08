// @vitest-environment jsdom
import { act, startTransition, StrictMode, Suspense } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useListSelect } from "./use-list-select";

type Item = { id: string; title: string };
let root: Root;
let container: HTMLDivElement;
let selection: ReturnType<typeof useListSelect<Item>>;
function Probe({
  items,
  suspended,
}: {
  items: Item[];
  suspended?: Promise<void>;
}) {
  selection = useListSelect(items);
  if (suspended) throw suspended;
  return null;
}
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  vi.unstubAllGlobals();
});
async function render(items: Item[], suspended?: Promise<void>) {
  await act(async () => {
    const update = () =>
      root.render(
        <StrictMode>
          <Suspense>
            <Probe items={items} suspended={suspended} />
          </Suspense>
        </StrictMode>,
      );
    if (suspended) startTransition(update);
    else update();
  });
}

it("resolves selected IDs against updated entities and prunes removed rows", async () => {
  await render([{ id: "1", title: "old" }]);
  await act(async () => selection.onSelectChange("1", true, false));
  await render([{ id: "1", title: "edited" }]);
  expect(selection.getSelectedItems()).toEqual([{ id: "1", title: "edited" }]);
  await render([]);
  expect(selection.getSelectedIds().size).toBe(0);
  expect(selection.selecting).toBe(false);
});

it("does not publish suspended list data to committed selection callbacks", async () => {
  const items = [{ id: "1", title: "committed" }];
  await render(items);
  await act(async () => selection.onSelectAll());
  const getSelected = selection.getSelectedItems;
  await render([{ id: "1", title: "abandoned" }], new Promise(() => {}));
  expect(getSelected()).toEqual(items);
  await render(items);
});

it("supports range, invert, all and none without retaining prior pages", async () => {
  await render(["1", "2", "3", "4"].map((id) => ({ id, title: id })));
  await act(async () => selection.onSelectChange("2", true, false));
  await act(async () => selection.onSelectChange("4", true, true));
  expect([...selection.getSelectedIds()]).toEqual(["2", "3", "4"]);
  await act(async () => selection.onInvertSelection());
  expect([...selection.getSelectedIds()]).toEqual(["1"]);
  await act(async () => selection.onSelectAll());
  expect(selection.selectedItems).toHaveLength(4);
  await act(async () => selection.onSelectNone());
  expect(selection.hasSelection).toBe(false);
  await act(async () => selection.onSelectAll());
  await render([{ id: "5", title: "next page" }]);
  expect(selection.selectedItems).toEqual([]);
});
