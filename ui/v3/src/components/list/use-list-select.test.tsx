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
  options,
}: {
  items: Item[];
  suspended?: Promise<void>;
  options?: Parameters<typeof useListSelect<Item>>[1];
}) {
  selection = useListSelect(items, options);
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
async function render(
  items: Item[],
  suspended?: Promise<void>,
  options?: Parameters<typeof useListSelect<Item>>[1],
) {
  await act(async () => {
    const update = () =>
      root.render(
        <StrictMode>
          <Suspense>
            <Probe items={items} suspended={suspended} options={options} />
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

it("supports range, invert, all and none in an unpaginated list", async () => {
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

it("accumulates pages, keeps off-page items when inverting, and refreshes selected labels", async () => {
  const scope = "performer:one";
  const first = [
    { id: "1", title: "first" },
    { id: "2", title: "second" },
  ];
  const second = [
    { id: "3", title: "third" },
    { id: "4", title: "fourth" },
  ];
  await render(first, undefined, { scope, pageKey: "1" });
  await act(async () => selection.onSelectAll());
  await render(first, undefined, { scope, pageKey: "2", loading: true });
  await act(async () => selection.onSelectAll());
  expect([...selection.getSelectedIds()]).toEqual(["1", "2"]);
  await render(second, undefined, { scope, pageKey: "2" });
  await act(async () => selection.onSelectAll());
  expect([...selection.getSelectedIds()]).toEqual(["1", "2", "3", "4"]);
  await act(async () => selection.onInvertSelection());
  expect([...selection.getSelectedIds()]).toEqual(["1", "2"]);
  await render([{ id: "1", title: "renamed" }, first[1]!], undefined, {
    scope,
    pageKey: "1",
  });
  expect(selection.getSelectedItems()).toEqual([
    { id: "1", title: "renamed" },
    first[1],
  ]);
  await render([first[1]!], undefined, { scope, pageKey: "1" });
  expect([...selection.getSelectedIds()]).toEqual(["2"]);
  await act(async () => selection.onSelectNone());
  expect(selection.selecting).toBe(false);
});

it("clears selection when performer or filter scope changes, even while loading", async () => {
  const items = [{ id: "1", title: "first" }];
  await render(items, undefined, { scope: "performer:one", pageKey: "1" });
  await act(async () => selection.onSelectAll());
  await render(items, undefined, {
    scope: "performer:two",
    pageKey: "1",
    loading: true,
  });
  expect(selection.getSelectedItems()).toEqual([]);
  await act(async () => selection.onSelectAll());
  expect(selection.getSelectedItems()).toEqual([]);
  await render(items, undefined, { scope: "performer:two", pageKey: "1" });
  await act(async () => selection.onSelectAll());
  await render(items, undefined, {
    scope: "performer:two?q=other",
    pageKey: "1",
  });
  expect(selection.hasSelection).toBe(false);
});

it("keeps selection through page-size and sort changes without pruning off-page IDs", async () => {
  await render([{ id: "1", title: "first" }], undefined, {
    pageKey: "1:40:asc",
  });
  await act(async () => selection.onSelectAll());
  await render([{ id: "2", title: "second" }], undefined, {
    pageKey: "1:20:desc",
  });
  expect([...selection.getSelectedIds()]).toEqual(["1"]);
  await act(async () => selection.onSelectAll());
  expect([...selection.getSelectedIds()]).toEqual(["1", "2"]);
});

it("removes confirmed deletions across pages while retaining unsuccessful items", async () => {
  await render([{ id: "1", title: "first" }], undefined, { pageKey: "1" });
  await act(async () => selection.onSelectAll());
  await render([{ id: "2", title: "second" }], undefined, { pageKey: "2" });
  await act(async () => selection.onSelectAll());
  await act(async () => selection.onRemoveSelection(["1"]));
  expect([...selection.getSelectedIds()]).toEqual(["2"]);
  await act(async () => selection.onRemoveSelection(["2"]));
  expect(selection.selecting).toBe(false);
});
