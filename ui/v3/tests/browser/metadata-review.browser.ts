import type { Page } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import { ids } from "../fixtures/metadata-review";

async function archive(page: Page, image: boolean) {
  const paths: string[] = [];
  await page.route("**/api/v3/archive/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    paths.push(path);
    const kind = image ? "image" : "scene";
    let result: unknown;
    if (path.includes("/entity-identities/"))
      result = { uuid: ids.entity, kind, revision: 2 };
    else if (path.endsWith("/metadata-fields"))
      result = {
        entity: { uuid: ids.entity, kind, revision: 2 },
        fields: [
          {
            definition: { name: "title", type: "string", clear_value: "" },
            value: "Library title",
            mode: "set",
            origin: "review",
            protected: true,
            decision: {
              uuid: ids.decision,
              sequence: 1,
              reason: "Owner choice",
              created_at: "2026-11-19T00:00:00Z",
            },
          },
          {
            definition: {
              name: "performers",
              type: "references",
              reference_kind: "performer",
              clear_value: [],
            },
            value: [ids.performer],
            mode: "set",
            origin: "library",
            protected: true,
            references: [
              { uuid: ids.performer, kind: "performer", revision: 1 },
            ],
          },
        ],
      };
    else throw new Error(`Unexpected archive request: ${path}`);
    await route.fulfill({ json: result });
  });
  return paths;
}

for (const image of [false, true]) {
  for (const desktop of [false, true]) {
    test(`${image ? "image" : "scene"} metadata shows current field choices on ${desktop ? "desktop" : "mobile"}`, async ({
      page,
    }) => {
      const paths = await archive(page, image);
      await page.setViewportSize(
        desktop ? { width: 1280, height: 900 } : { width: 390, height: 844 },
      );
      await page.goto(`/metadata-review${image ? "?image" : ""}`);
      if (desktop)
        await page
          .getByRole("tab", { name: "Metadata review", exact: true })
          .click();
      else await chooseSection(page, "Metadata review");
      const panel = page.getByRole("region", { name: "Metadata review" });
      await panel
        .getByRole("button", { name: "Current field choices", exact: true })
        .click();
      await expect(
        panel.getByText("Library title", { exact: true }),
      ).toBeVisible();
      await expect(
        panel.getByText("Owner choice", { exact: true }),
      ).toBeVisible();
      await expect(panel.getByText("River", { exact: true })).toBeVisible();
      // Historical file edits were retired with their server routes.
      await expect(
        panel.getByRole("button", { name: "Review retained fields" }),
      ).toHaveCount(0);
      expect(
        paths.filter((path) =>
          /file-edits|file-history|metadata-file-edits/.test(path),
        ),
      ).toEqual([]);
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        )
        .toBe(true);
    });
  }
}
