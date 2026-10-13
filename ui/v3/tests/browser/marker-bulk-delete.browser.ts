import { type Page } from "@playwright/test";
import { test, expect, holdForContextMenu } from "./test";
import { DisplayMode } from "../../src/models/list-filter/types";
import { encodeURLJSON } from "../../src/utils/url-json";

const markerID = (id: number) =>
  `00000000-0000-4000-8000-${String(id).padStart(12, "0")}`;
const filterURL = `/scenes/markers?perPage=40&p=2&q=clip&fa=${encodeURLJSON({
  k: 0,
  o: 0,
  c: [{ k: 1, f: "duration", m: 2, v: { value: 1 } }],
})}`;

async function selectPage(page: Page, layout: "grid" | "table" = "grid") {
  const item = page
    .locator(layout === "grid" ? ".marker-card" : "tbody tr")
    .first();
  const menu = page.getByRole("menu");
  await holdForContextMenu(item, menu);
  await menu.getByRole("menuitem", { name: "Select All", exact: true }).tap();
  await expect(menu).toBeHidden();
}

for (const layout of ["grid", "table"] as const) {
  test(`marker ${layout} menu deletes the selection, not the item used to open it`, async ({
    page,
  }) => {
    if (layout === "table") {
      await page.addInitScript((mode) => {
        localStorage.setItem("list-display-mode:scene_markers", String(mode));
      }, DisplayMode.Table);
    }
    await page.goto("/scenes/markers");
    const items = page.locator(layout === "grid" ? ".marker-card" : "tbody tr");
    await expect(items).toHaveCount(3);
    await selectPage(page, layout);
    // Keep only the first two selected; open the menu on the unselected third.
    if (layout === "table") await items.nth(2).getByRole("checkbox").uncheck();
    else
      await items
        .nth(2)
        .getByRole("button", { name: "Select Marker 3", exact: true })
        .tap();
    const menu = page.getByRole("menu");
    await holdForContextMenu(items.nth(2), menu);
    await menu.getByRole("menuitem", { name: /Delete 2 markers/i }).tap();
    const dialog = page.getByRole("dialog");
    await expect(
      dialog.getByRole("heading", { name: "Delete 2 markers?" }),
    ).toBeVisible();
    await expect(dialog.getByRole("checkbox")).toHaveCount(0);
    await dialog.getByRole("button", { name: "Cancel", exact: true }).tap();
    expect(await page.evaluate(() => window.markerListMutations)).toEqual([]);
    await page
      .getByRole("button", { name: "Delete selected", exact: true })
      .tap();
    await dialog.getByRole("button", { name: "Delete", exact: true }).tap();
    await expect(dialog).toBeHidden();
    await expect(items).toHaveCount(1);
    expect(await page.evaluate(() => window.markerListMutations)).toEqual([
      { operation: "SceneMarkersDestroy", ids: [markerID(1), markerID(2)] },
    ]);
    expect(await page.evaluate(() => window.markerListRemaining)).toEqual([
      markerID(3),
    ]);
    expect(
      await page.evaluate(() =>
        window.markerListQueries.some(
          (query) => query.operation === "FindSceneMarkerIDs",
        ),
      ),
    ).toBe(false);
  });
}

for (const width of [390, 1280]) {
  test(`marker deletion includes every filtered page only when requested at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.addInitScript(() => {
      window.markerListBulk = true;
    });
    await page.goto(filterURL);
    await selectPage(page);
    await page
      .getByRole("button", { name: "Delete selected", exact: true })
      .tap();
    const dialog = page.getByRole("dialog");
    await expect(
      dialog.getByRole("heading", { name: "Delete 23 markers?" }),
    ).toBeVisible();
    const matching = dialog.getByRole("switch", {
      name: "Apply to all 63 matching",
    });
    await expect(matching).not.toBeChecked();
    await matching.check();
    await expect(
      dialog.getByRole("heading", { name: "Delete 63 markers?" }),
    ).toBeVisible();
    await expect(
      dialog.getByRole("button", { name: "Delete", exact: true }),
    ).toBeEnabled();
    await dialog.getByRole("button", { name: "Cancel", exact: true }).tap();
    expect(await page.evaluate(() => window.markerListMutations)).toEqual([]);
    await page
      .getByRole("button", { name: "Delete selected", exact: true })
      .tap();
    await expect(matching).not.toBeChecked();
    await expect(
      dialog.getByRole("heading", { name: "Delete 23 markers?" }),
    ).toBeVisible();
    await matching.check();
    await dialog.getByRole("button", { name: "Delete", exact: true }).tap();
    await expect(dialog).toBeHidden();
    const mutations = await page.evaluate(() => window.markerListMutations);
    expect(mutations).toEqual([
      {
        operation: "SceneMarkersDestroy",
        ids: Array.from({ length: 50 }, (_, i) => markerID(i + 1)),
      },
      {
        operation: "SceneMarkersDestroy",
        ids: Array.from({ length: 13 }, (_, i) => markerID(i + 51)),
      },
    ]);
    expect(await page.evaluate(() => window.markerListRemaining)).toEqual([
      markerID(64),
      markerID(65),
    ]);
    const queries = await page.evaluate(() => window.markerListQueries);
    const lookup = queries.find(
      (query) => query.operation === "FindSceneMarkerIDs",
    );
    expect(lookup?.variables).toMatchObject({
      filter: { q: "clip", page: 1, per_page: -1 },
      scene_marker_filter_ast: {
        root: {
          group: {
            operator: "AND",
            children: [
              {
                condition: {
                  field: "duration",
                  value: { modifier: "GREATER_THAN", value: 1 },
                },
              },
            ],
          },
        },
      },
    });
    await expect(
      page.getByText("No results found", { exact: true }),
    ).toBeVisible();
  });
}

test("a failed bulk marker batch stops and reports confirmed progress", async ({
  page,
}) => {
  await page.addInitScript(() => {
    window.markerListBulk = true;
    window.markerListFailBatch = 2;
  });
  await page.goto(filterURL);
  await selectPage(page);
  await page
    .getByRole("button", { name: "Delete selected", exact: true })
    .tap();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("switch").check();
  await dialog.getByRole("button", { name: "Delete", exact: true }).tap();
  await expect(dialog).toBeHidden();
  await expect(
    page.getByText(
      "Deletion stopped: 50 of 63 confirmed. Refresh the list before retrying.",
      { exact: true },
    ),
  ).toBeVisible();
  expect(await page.evaluate(() => window.markerListMutations.length)).toBe(2);
  expect(await page.evaluate(() => window.markerListRemaining)).toEqual(
    Array.from({ length: 15 }, (_, i) => markerID(i + 51)),
  );
});

test("an incomplete matching marker lookup cannot be confirmed", async ({
  page,
}) => {
  await page.addInitScript(() => {
    window.markerListBulk = true;
    window.markerListIncomplete = true;
  });
  await page.goto(filterURL);
  await selectPage(page);
  await page
    .getByRole("button", { name: "Delete selected", exact: true })
    .tap();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("switch").check();
  await expect(dialog.getByRole("alert")).toHaveText(
    "The matching marker list is incomplete. Refresh before deleting.",
  );
  await expect(
    dialog.getByRole("button", { name: "Delete", exact: true }),
  ).toBeDisabled();
  await dialog.getByRole("button", { name: "Cancel", exact: true }).tap();
  expect(await page.evaluate(() => window.markerListMutations)).toEqual([]);
});
