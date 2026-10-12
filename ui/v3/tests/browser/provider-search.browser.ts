import { test, expect } from "./test";

for (const width of [390, 1280]) {
  test(`package search finds studio and pasted URL from the loaded index (${width})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/package-search");
    await page
      .getByRole("button", { name: "Load packages", exact: true })
      .click();
    const search = page.getByRole("textbox", {
      name: "Search packages",
      exact: true,
    });
    await search.fill("  EXAMPLE STUDIO ");
    await expect(
      page.getByRole("cell", { name: /^Network scraper/ }),
    ).toBeVisible();
    await expect(
      page.getByText("Matches: Example Studio", { exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("cell", { name: /^Old scraper/ })).toHaveCount(
      0,
    );
    await search.fill("https://example.test/scenes/123");
    await expect(
      page.getByText("Matches: example.test/scenes/", { exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: test.info().outputPath("package-search.png"),
    });
    await page
      .getByRole("checkbox", { name: "Select Network scraper", exact: true })
      .check();
    await expect(
      page.getByRole("button", { name: /Install selected.*1/ }),
    ).toBeEnabled();
    await search.fill("unrelated");
    await expect(
      page.getByText("No packages match the filter.", { exact: true }),
    ).toBeVisible();
    await search.fill(" ");
    await expect(
      page.getByRole("cell", { name: /^Old scraper/ }),
    ).toBeVisible();
    expect(await page.evaluate(() => window.packageIndexRequests)).toBe(1);
  });

  test(`provider duration matches without fingerprints in search and review (${width})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/provider-metadata");
    await page.getByRole("button", { name: /Scrape with/ }).click();
    await page.getByRole("menuitem", { name: "Fixture provider" }).click();
    const search = page.getByRole("dialog", {
      name: "Search with Fixture provider",
    });
    await search.getByPlaceholder(/Search/).fill("Fixture");
    const result = search.getByRole("button", { name: /Imported scene/ });
    await expect(
      result.getByText("Remote: 0:12 · Local: 0:12", { exact: true }),
    ).toBeVisible();
    await expect(
      result.getByText("Duration matches (within 5 seconds)", { exact: true }),
    ).toBeVisible();
    await expect(result.getByText(/PHash|OSHash/)).toHaveCount(0);
    await page.screenshot({
      path: test.info().outputPath("duration-match.png"),
    });
    await result.click();
    const review = page.getByRole("dialog", { name: "Review scraped results" });
    await expect(
      review.getByText("Remote: 0:12 · Local: 0:12", { exact: true }),
    ).toBeVisible();
    await expect(
      review.getByText("Duration matches (within 5 seconds)", { exact: true }),
    ).toBeVisible();
    expect(await page.evaluate(() => window.providerFixture.scenes)).toEqual(
      [],
    );
  });
}

test("duration mismatch is visible and unknown duration is not presented as a match", async ({
  page,
}) => {
  for (const mode of ["different", "unknown"]) {
    await page.goto(`/provider-metadata?duration=${mode}`);
    await page.getByRole("button", { name: /Scrape with/ }).click();
    await page.getByRole("menuitem", { name: "Fixture provider" }).click();
    const search = page.getByRole("dialog", {
      name: "Search with Fixture provider",
    });
    await search.getByPlaceholder(/Search/).fill("Fixture");
    const result = search.getByRole("button", { name: /Imported scene/ });
    await expect(result).toBeVisible();
    await expect(
      result.getByText("Duration matches (within 5 seconds)", { exact: true }),
    ).toHaveCount(0);
    if (mode === "different") {
      await expect(
        result.getByText("Remote: 2:00 · Local: 0:12", { exact: true }),
      ).toBeVisible();
      await expect(
        result.getByText("Duration differs by 108s", { exact: true }),
      ).toBeVisible();
    } else {
      await expect(result.getByText(/Remote:/)).toHaveCount(0);
    }
  }
});
