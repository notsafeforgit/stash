import { test, expect } from "./test";
import type {
  FindGalleriesQueryVariables,
  SelectTagDataFragment,
} from "../../src/core/generated-graphql";

test("gallery studio tags preserve tag depth and studio hierarchy on mobile", async ({
  page,
}) => {
  const tagID = "00a0cf9d-4659-4e18-891e-2fc13ee26af0";
  const tag: SelectTagDataFragment = {
    __typename: "Tag",
    id: tagID,
    name: "Studio category",
    sort_name: null,
    favorite: false,
    description: null,
    aliases: [],
    image_path: null,
    parents: [],
    stash_ids: [],
  };
  const filters: FindGalleriesQueryVariables["gallery_filter_ast"][] = [];
  await page.route("**/graphql", async (route) => {
    const body = route.request().postDataJSON();
    switch (body.operationName) {
      case "FindSavedFilters":
        return route.fulfill({ json: { data: { findSavedFilters: [] } } });
      case "FindGalleries":
        filters.push(body.variables.gallery_filter_ast);
        return route.fulfill({
          json: { data: { findGalleries: { count: 0, galleries: [] } } },
        });
      case "FindTagsForSelect":
        return route.fulfill({
          json: {
            data: {
              findTags: {
                __typename: "FindTagsResultType",
                count: 1,
                tags: [tag],
              },
            },
          },
        });
      default:
        throw new Error(`Unexpected filter operation ${body.operationName}`);
    }
  });
  await page.goto("/gallery-list");
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  await page.getByRole("button", { name: "Add first condition" }).tap();
  const condition = page.locator(".filter-condition-shell");
  const field = condition.getByRole("combobox").first();
  await field.tap();
  await field.fill("Studio Tags");
  await page.getByRole("option", { name: /^Studio Tags\b/ }).tap();
  await condition.getByPlaceholder("Search…").fill("Studio category");
  await page
    .getByRole("option", { name: "Studio category", exact: true })
    .tap();
  await page.keyboard.press("Escape");
  await condition
    .getByRole("combobox")
    .filter({ hasText: /^Exact/ })
    .tap();
  await page
    .getByRole("option", {
      name: "Exact studio and ancestor studios",
      exact: true,
    })
    .tap();
  await condition.getByRole("switch", { name: "Include sub-tags" }).tap();
  await expect
    .poll(() => filters.at(-1))
    .toMatchObject({
      root: {
        group: {
          operator: "AND",
          children: [
            {
              condition: {
                field: "studio_tags",
                value: {
                  value: [tagID],
                  depth: -1,
                  hierarchy_mode: "ancestors",
                },
              },
            },
          ],
        },
      },
    });
  const applied = filters.at(-1);
  await page.reload();
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  await expect(field).toHaveValue("Studio Tags");
  await expect(
    condition.getByRole("switch", { name: "Include sub-tags" }),
  ).toBeChecked();
  await expect.poll(() => filters.at(-1)).toEqual(applied);
  await page.screenshot({
    path: test.info().outputPath("gallery-studio-tags.png"),
  });
});
