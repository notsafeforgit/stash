import type { Page } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import { ids, preview, receipt } from "../fixtures/metadata-review";
import type {
  EditApply,
  EditReceipt,
} from "../../src/core/native-archive/metadata-review-api";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 404"],
});

async function archive(
  page: Page,
  options: {
    image?: boolean;
    names?: boolean;
    missingName?: boolean;
    stale?: boolean;
    loseResponse?: boolean;
  } = {},
) {
  let committed: EditReceipt | null = null;
  const bodies: EditApply[] = [];
  await page.route("**/api/v3/archive/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    let result: unknown;
    if (path.endsWith("/entity-identities/performer/12"))
      result = {
        uuid: ids.thirdPerformer,
        kind: "performer",
        revision: 3,
        local_id: 12,
      };
    else if (path.includes("/entity-identities/"))
      result = {
        uuid: ids.entity,
        kind: options.image ? "image" : "scene",
        revision: 2,
        local_id: 7,
      };
    else if (path.endsWith("/metadata-fields"))
      result = {
        entity: {
          uuid: ids.entity,
          kind: options.image ? "image" : "scene",
          revision: 2,
          local_id: 7,
        },
        fields: [
          {
            definition: { name: "title", type: "string", clear_value: "" },
            value:
              committed && !committed.kept_current
                ? "Retained title"
                : "Library title",
            mode: "set",
            origin: "library",
            protected: true,
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
              {
                uuid: ids.performer,
                kind: "performer",
                revision: 1,
                local_id: 10,
              },
            ],
          },
        ],
      };
    else if (path.endsWith("/file-edits"))
      result = [
        {
          history_uuid: ids.history,
          match_uuid: ids.match,
          collection_uuid: ids.collection,
          file_uuid: ids.file,
          relative_path: "Purchased collection/example.mp4",
          source_time: "",
        },
      ];
    else if (path.includes("/file-history/"))
      result = {
        uuid: ids.history,
        kind: "metadata_edit",
        collection_uuid: ids.collection,
        source_time: "",
        edits: [
          {
            field: options.names ? "actors" : "title",
            target_field: options.names ? "performers" : "title",
            mode: "set",
            value_type: options.names ? "names" : "string",
            value: options.names ? ["River"] : "Retained title",
          },
        ],
      };
    else if (path.endsWith("/preview")) {
      const input = route.request().postDataJSON();
      const chosen = input.selections?.River;
      result = {
        ...preview(),
        input,
        ...(options.names
          ? {
              field: "performers",
              current_value: [ids.performer],
              value: chosen ? [chosen.uuid] : undefined,
              status: chosen ? "ready" : "unresolved_names",
              names: [
                {
                  name: "River",
                  more: false,
                  candidates: options.missingName
                    ? []
                    : [
                        {
                          uuid: ids.performer,
                          local_id: 10,
                          revision: 1,
                          name: "River",
                        },
                        {
                          uuid: ids.otherPerformer,
                          local_id: 11,
                          revision: 1,
                          name: "River",
                        },
                      ],
                  ...(chosen
                    ? {
                        selected: {
                          ...chosen,
                          local_id:
                            chosen.uuid === ids.thirdPerformer
                              ? 12
                              : chosen.uuid === ids.performer
                                ? 10
                                : 11,
                          name:
                            chosen.uuid === ids.thirdPerformer
                              ? "Cloud"
                              : "River",
                        },
                      }
                    : {}),
                },
              ],
            }
          : {}),
      };
    } else if (path.includes("/requests/")) {
      await route.fulfill({
        status: committed ? 200 : 404,
        json: committed ?? { error: "not_found" },
      });
      return;
    } else if (path.endsWith("/apply")) {
      const body: EditApply = route.request().postDataJSON();
      bodies.push(body);
      if (options.stale) {
        await route.fulfill({
          status: 409,
          json: { error: "preview_changed" },
        });
        return;
      }
      committed = receipt(body);
      if (options.loseResponse) {
        options.loseResponse = false;
        await route.abort("failed");
        return;
      }
      result = { review: committed, replayed: false };
    } else throw new Error(`Unexpected archive request: ${path}`);
    await route.fulfill({ json: result });
  });
  return bodies;
}

async function openReview(page: Page, desktop = false, image = false) {
  await page.setViewportSize(
    desktop ? { width: 1280, height: 900 } : { width: 390, height: 844 },
  );
  await page.goto(`/metadata-review${image ? "?image" : ""}`);
  if (desktop)
    await page
      .getByRole("tab", { name: "Metadata review", exact: true })
      .click();
  else await chooseSection(page, "Metadata review");
  await page
    .getByRole("button", { name: "Review retained fields", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Preview choice", exact: true })
    .click();
}

for (const image of [false, true]) {
  for (const desktop of [false, true]) {
    test(`${image ? "image" : "scene"} review is available on ${desktop ? "desktop" : "mobile"} and applies only after preview`, async ({
      page,
    }) => {
      const bodies = await archive(page, { image });
      await openReview(page, desktop, image);
      const panel = page.getByRole("region", { name: "Metadata review" });
      await expect(
        panel.getByText("Library title", { exact: true }),
      ).toBeVisible();
      await expect(
        panel.getByText("Retained title", { exact: true }),
      ).toBeVisible();
      await expect(
        panel.getByText("Replaces a protected choice"),
      ).toBeVisible();
      expect(bodies).toHaveLength(0);
      await page.screenshot({
        path: test
          .info()
          .outputPath(
            `${image ? "image" : "scene"}-${desktop ? "desktop" : "mobile"}-preview.png`,
          ),
      });
      await panel.getByRole("button", { name: "Apply this choice" }).click();
      await expect(
        panel.getByText("Choice applied", { exact: true }),
      ).toBeVisible();
      expect(bodies).toHaveLength(1);
      expect(Object.keys(bodies[0] ?? {}).sort()).toEqual([
        "digest",
        "entity_uuid",
        "history_uuid",
        "match_uuid",
        "request_uuid",
        "source_field",
      ]);
      await panel
        .getByRole("button", { name: "Current field choices", exact: true })
        .click();
      await expect(
        panel.getByText("Retained title", { exact: true }),
      ).toBeVisible();
      await expect(
        panel.getByText("River", { exact: true }).first(),
      ).toBeVisible();
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

test("ambiguous canonical/alias candidates must be resolved before Apply appears", async ({
  page,
}) => {
  const bodies = await archive(page, { names: true });
  await openReview(page);
  await expect(
    page.getByText(
      "Choose an existing entry for every unresolved name. Nothing is partially applied.",
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Apply this choice" }),
  ).toHaveCount(0);
  await page.getByRole("combobox", { name: "River", exact: true }).click();
  await page.getByRole("option", { name: "River (#11)", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Apply this choice" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Apply this choice" }).click();
  await expect(page.getByText("Choice applied", { exact: true })).toBeVisible();
  expect(bodies[0]?.selections).toEqual({
    River: { uuid: ids.otherPerformer, revision: 1 },
  });
});

for (const image of [false, true]) {
  test(`keeps the current ${image ? "image" : "scene"} value without replacing metadata`, async ({
    page,
  }) => {
    const bodies = await archive(page, { image });
    await openReview(page, false, image);
    const panel = page.getByRole("region", { name: "Metadata review" });
    expect(bodies).toHaveLength(0);
    await panel
      .getByRole("button", { name: "Keep current value", exact: true })
      .click();
    await expect(
      panel.getByText("Current value kept", { exact: true }),
    ).toBeVisible();
    await panel
      .getByRole("button", { name: "Current field choices", exact: true })
      .click();
    await expect(
      panel.getByText("Library title", { exact: true }),
    ).toBeVisible();
    await expect(
      panel.getByText("Choice applied", { exact: true }),
    ).toHaveCount(0);
    expect(bodies).toHaveLength(1);
    expect(bodies[0]?.keep_current).toBe(true);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}

test("can decline an ambiguous retained performer name without selecting a candidate", async ({
  page,
}) => {
  const bodies = await archive(page, { names: true });
  await openReview(page);
  await expect(
    page.getByRole("button", { name: "Apply this choice" }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Keep current value", exact: true })
    .click();
  await expect(
    page.getByText("Current value kept", { exact: true }),
  ).toBeVisible();
  expect(bodies).toHaveLength(1);
  expect(bodies[0]?.keep_current).toBe(true);
  expect(bodies[0]?.selections).toBeUndefined();
});

test.describe("response recovery", () => {
  test.use({
    expectedConsoleErrors: ["Failed to load resource", "Load failed"],
  });
  test("recovers a lost Keep reply after reload without treating it as Apply", async ({
    page,
  }) => {
    const bodies = await archive(page, { loseResponse: true });
    await openReview(page);
    await page
      .getByRole("button", { name: "Keep current value", exact: true })
      .click();
    await expect(
      page.getByText("Could not complete this step", { exact: true }),
    ).toBeVisible();
    expect(bodies).toHaveLength(1);
    await page.reload();
    await chooseSection(page, "Metadata review");
    await page
      .getByRole("button", { name: "Check and retry saved change" })
      .click();
    await expect(
      page.getByText("Current value kept", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Choice applied", { exact: true })).toHaveCount(
      0,
    );
    await page
      .getByRole("button", { name: "Current field choices", exact: true })
      .click();
    await expect(
      page.getByText("Library title", { exact: true }),
    ).toBeVisible();
    expect(bodies).toHaveLength(1);
    expect(bodies[0]?.keep_current).toBe(true);
  });
  test("a reload recovers the committed receipt without applying twice", async ({
    page,
  }) => {
    const bodies = await archive(page, { loseResponse: true });
    await openReview(page);
    await page.getByRole("button", { name: "Apply this choice" }).click();
    // The pending banner appears as soon as the request is saved locally.
    // Wait for the committed response to be lost before interrupting delivery.
    await expect(
      page.getByText("Could not complete this step", { exact: true }),
    ).toBeVisible();
    expect(bodies).toHaveLength(1);
    await expect(
      page.getByText("A saved change needs confirmation", { exact: true }),
    ).toBeVisible();
    await page.reload();
    await chooseSection(page, "Metadata review");
    await page
      .getByRole("button", { name: "Check and retry saved change" })
      .click();
    await expect(
      page.getByText("Choice applied", { exact: true }),
    ).toBeVisible();
    expect(bodies).toHaveLength(1);
    await expect(
      page.getByText("A saved change needs confirmation", { exact: true }),
    ).toHaveCount(0);
  });

  test("a stale preview is not retried as a different edit", async ({
    page,
  }) => {
    const bodies = await archive(page, { stale: true });
    await openReview(page);
    await page.getByRole("button", { name: "Apply this choice" }).click();
    await expect(
      page.getByText("Saved choice needs a new preview", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Apply this choice" }),
    ).toHaveCount(0);
    expect(bodies).toHaveLength(1);
    await page
      .getByRole("button", { name: "Review again", exact: true })
      .click();
    await expect(
      page.getByText("Saved choice needs a new preview", { exact: true }),
    ).toHaveCount(0);
    expect(bodies).toHaveLength(1);
  });
});

test("browser storage coordinates tabs and separates two deployments on one origin", async ({
  page,
  context,
}) => {
  await page.goto("/metadata-review");
  const other = await context.newPage();
  try {
    await other.goto("/metadata-review");
    const [one, same] = await Promise.all([
      page.evaluate(() => window.metadataReviewStorage.prepare("/one/")),
      other.evaluate(() => window.metadataReviewStorage.prepare("/one/")),
    ]);
    expect(one.body).toBe(same.body);
    expect(
      await other.evaluate(() => window.metadataReviewStorage.read("/two/")),
    ).toBeNull();
    const separate = await other.evaluate(() =>
      window.metadataReviewStorage.prepare("/two/"),
    );
    expect(separate.body).not.toBe(one.body);
    await page.reload();
    expect(
      await page.evaluate(() => window.metadataReviewStorage.read("/one/")),
    ).toEqual(one);
    expect(
      await page.evaluate(() => window.metadataReviewStorage.read("/two/")),
    ).toEqual(separate);
  } finally {
    await other.close();
  }
});

test("an unmatched source name can be explicitly associated with a differently named existing performer", async ({
  page,
}) => {
  const bodies = await archive(page, { names: true, missingName: true });
  await openReview(page);
  await expect(
    page.getByRole("button", { name: "Apply this choice" }),
  ).toHaveCount(0);
  await page
    .getByRole("combobox", {
      name: "Choose another existing entry",
      exact: true,
    })
    .fill("Cloud");
  await page.getByRole("option", { name: "Cloud (#12)", exact: true }).click();
  await expect(page.getByText("Cloud", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Apply this choice" }).click();
  await expect(page.getByText("Choice applied", { exact: true })).toBeVisible();
  expect(bodies[0]?.selections).toEqual({
    River: { uuid: ids.thirdPerformer, revision: 3 },
  });
});
