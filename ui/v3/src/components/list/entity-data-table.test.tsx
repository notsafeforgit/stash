// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { FilterMode, SortDirectionEnum } from "@/core/generated-graphql";
import { ListFilterModel } from "@/models/list-filter/filter";
import { EntityDataTable } from "./entity-data-table";
import { selectionColumn } from "./selection-column";
import { DataTableColumnHeader } from "./data-table-column-header";
import type { EntityColumnDef } from "./entity-table";
import { useListSelect } from "./use-list-select";

vi.mock("./use-list-scroll-restoration", () => ({
  useListScrollRestoration: vi.fn(),
}));

type Item = { id: string; title: string };
const items: Item[] = [
  { id: "2", title: "Zulu" },
  { id: "1", title: "Alpha" },
];
const columns: EntityColumnDef<Item>[] = [
  selectionColumn<Item>(),
  {
    id: "title",
    accessorKey: "title",
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="Title" />
    ),
  },
  { id: "id", accessorKey: "id", header: "ID" },
];
let root: Root;
let container: HTMLDivElement;
let selection: ReturnType<typeof useListSelect<Item>>;
let currentFilter: ListFilterModel;

function Harness() {
  const [filter, setFilter] = useState(
    () =>
      new ListFilterModel(FilterMode.Scenes, undefined, {
        defaultSortBy: "title",
        defaultSortDir: SortDirectionEnum.Asc,
      }),
  );
  currentFilter = filter;
  selection = useListSelect(items);
  return (
    <IntlProvider
      locale="en"
      messages={{ columns: "Columns", columns_toggle_label: "Show columns" }}
    >
      <EntityDataTable
        items={items}
        columns={columns}
        filter={filter}
        setFilter={setFilter}
        listSelect={selection}
        visibilityKey="regression"
        totalCount={100}
        preserveScrollDuringRefill={false}
      />
    </IntlProvider>
  );
}

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  localStorage.clear();
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  localStorage.clear();
  vi.unstubAllGlobals();
});

function element(selector: string) {
  const found = container.querySelector<HTMLElement>(selector);
  if (!found) throw new Error(`Missing ${selector}`);
  return found;
}

it("selects visible entity IDs and sends sorting to the server filter", async () => {
  await act(async () => root.render(<Harness />));
  await act(async () => selection.onEnterSelect());
  await act(async () => element('[aria-label="Select all"]').click());
  expect(selection.getSelectedIds()).toEqual(new Set(["2", "1"]));
  await act(async () => element('[aria-label="Select row"]').click());
  expect([...selection.getSelectedIds()]).toEqual(["1"]);
  expect(
    element('[aria-label="Select all"]').getAttribute("aria-checked"),
  ).toBe("mixed");

  await act(async () => element("thead button:not([role=checkbox])").click());
  expect(currentFilter.sortBy).toBe("title");
  expect(currentFilter.sortDirection).toBe(SortDirectionEnum.Desc);
  // The backend owns row order, including when only one page is present.
  expect(element("tbody tr:first-child").textContent).toContain("Zulu");
});

it("restores saved column visibility and order with selection pinned first", async () => {
  localStorage.setItem("table-col-order:regression", '["id","title"]');
  localStorage.setItem("table-cols:regression", '{"title":false}');
  await act(async () => root.render(<Harness />));
  expect(
    [...container.querySelectorAll("thead th")].map((th) => th.textContent),
  ).toEqual(["ID"]);
  await act(async () => selection.onEnterSelect());
  const headers = [...container.querySelectorAll("thead th")];
  expect(headers).toHaveLength(2);
  expect(headers[0]?.querySelector('[aria-label="Select all"]')).not.toBeNull();
  expect(headers[1]?.textContent).toBe("ID");
});
