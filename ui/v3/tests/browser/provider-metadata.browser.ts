import type { Page } from "@playwright/test";
import { test, expect } from "./test";

async function applyProvider(page: Page, kind: "scene" | "performer") {
  await page.getByRole("button", { name: /Scrape with/ }).click();
  await page.getByRole("menuitem", { name: "Fixture provider" }).click();
  const search = page.getByRole("dialog", {
    name: "Search with Fixture provider",
  });
  await search.getByPlaceholder(/Search/).fill("Fixture");
  await search
    .getByRole("button", { name: new RegExp(`Imported ${kind}`) })
    .click();
  const review = page.getByRole("dialog", { name: "Review scraped results" });
  await review.getByRole("button", { name: "Apply", exact: true }).click();
  await expect(review).not.toBeVisible();
}

for (const width of [390, 1280]) {
  test(`scene Apply, manual edit and Save keep only accepted provider fields (${width})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/provider-metadata");
    await applyProvider(page, "scene");
    expect(await page.evaluate(() => window.providerFixture.scenes)).toEqual(
      [],
    );
    expect(await page.evaluate(() => window.providerFixture.tags)).toEqual([
      {
        name: "Provider tag",
        stash_ids: [
          { endpoint: "https://provider.test/graphql", stash_id: "tag-9" },
        ],
        provider_metadata: [
          {
            endpoint: "https://provider.test/graphql",
            remote_id: "tag-9",
            fields: ["name"],
          },
        ],
      },
    ]);
    await page.getByLabel("Title", { exact: true }).fill("My title");
    // Returning to the imported string is still a manual choice.
    await page.getByLabel("Title", { exact: true }).fill("Imported scene");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect
      .poll(() => page.evaluate(() => window.providerFixture.scenes.length))
      .toBe(1);
    expect(
      await page.evaluate(
        () => window.providerFixture.scenes[0]?.provider_metadata,
      ),
    ).toEqual([
      {
        endpoint: "https://provider.test/graphql",
        remote_id: "scene-7",
        fields: ["code", "tags"],
      },
    ]);
    await page.getByLabel("Title", { exact: true }).fill("Later manual title");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect
      .poll(() => page.evaluate(() => window.providerFixture.scenes.length))
      .toBe(2);
    expect(
      await page.evaluate(
        () => window.providerFixture.scenes[1]?.provider_metadata,
      ),
    ).toEqual([]);
  });
}

test("performer draft retains date precision, maps height and clears on discard", async ({
  page,
}) => {
  await page.goto("/provider-metadata?performer");
  await applyProvider(page, "performer");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => window.providerFixture.performers.length))
    .toBe(1);
  expect(
    await page.evaluate(
      () => window.providerFixture.performers[0]?.provider_metadata,
    ),
  ).toEqual([
    {
      endpoint: "https://provider.test/graphql",
      remote_id: "performer-7",
      fields: ["aliases", "birthdate", "height", "name"],
    },
  ]);
  expect(
    await page.evaluate(() => window.providerFixture.performers[0]?.birthdate),
  ).toBe("1990");
  await applyProvider(page, "performer");
  await page.getByRole("button", { name: "Discard", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Canonical name", exact: true })
    .fill("Manual name");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => window.providerFixture.performers.length))
    .toBe(2);
  expect(
    await page.evaluate(
      () => window.providerFixture.performers[1]?.provider_metadata,
    ),
  ).toEqual([]);
});

test("history reads only expanded current or retired identities and pages by sequence", async ({
  page,
}) => {
  const current = "11111111-1111-4111-8111-111111111111";
  const retired = "33333333-3333-4333-8333-333333333333";
  const requests: string[] = [];
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    requests.push(url.pathname + url.search);
    if (url.pathname.includes("entity-identities")) {
      await route.fulfill({
        json: { uuid: current, kind: "scene", revision: 1, local_id: 1 },
      });
      return;
    }
    const old = url.pathname.includes(retired);
    const after = Number(url.searchParams.get("after"));
    const count = old || after ? 1 : 25;
    await route.fulfill({
      json: Array.from({ length: count }, (_, index) => ({
        sequence: after + index + 1,
        uuid: `22222222-2222-4222-8222-${String(after + index + 1).padStart(12, "0")}`,
        entity_uuid: old ? retired : current,
        original_entity_uuid: old ? retired : current,
        entity_kind: old ? "performer" : "scene",
        entity_revision: 1,
        endpoint: "https://provider.test/graphql",
        remote_id: old ? "old-performer" : "scene-7",
        operation: "review",
        values: old
          ? { name: "Before merge" }
          : { title: `Saved title ${after + index + 1}` },
        signature: "a".repeat(64),
        created_at: "2026-10-08T00:00:00Z",
      })),
    });
  });
  await page.goto("/provider-metadata?history");
  expect(requests).toEqual([]);
  const section = page.getByRole("region", {
    name: "Scene import history",
    exact: true,
  });
  await section
    .getByRole("button", { name: "Provider import history", exact: true })
    .click();
  await expect(section.getByText(/Later edits may differ/)).toBeVisible();
  await section
    .getByRole("button", { name: /provider.test/ })
    .first()
    .click();
  await expect(
    section.getByText("Saved title 1", { exact: true }),
  ).toBeVisible();
  await section.getByRole("button", { name: "Next", exact: true }).click();
  await section.getByRole("button", { name: /provider.test/ }).click();
  await expect(
    section.getByText("Saved title 26", { exact: true }),
  ).toBeVisible();
  expect(requests.some((url) => url.includes("after=25&limit=25"))).toBe(true);
  const old = page.getByRole("region", {
    name: "Retired performer import history",
    exact: true,
  });
  await old
    .getByRole("button", { name: "Provider import history", exact: true })
    .click();
  await old.getByRole("button", { name: /provider.test/ }).click();
  await expect(old.getByText("Before merge", { exact: true })).toBeVisible();
  expect(requests.filter((url) => url.includes(retired))).toHaveLength(1);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
});
