import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test("saves an optional scene filter and plays whole scenes inside the marker feed", async ({
  page,
}) => {
  await serveSceneMedia(page);
  await page.goto(
    "/tv-fixture/settings/tv?paused&resume&start=resume&window=fixed",
  );
  const selector = page.getByRole("combobox", {
    name: "Extra scenes in marker mode",
    exact: true,
  });
  await expect(selector).toContainText("None — markers only");
  await selector.click();
  await expect(
    page.getByRole("option", { name: "Marker picks", exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("option", { name: "Short clip performers", exact: true })
    .click();
  await expect
    .poll(() => page.evaluate(() => window.tvFixtureSaveAttempts.at(-1)))
    .toMatchObject({
      key: "tv",
      value: {
        markerSceneFilter: {
          kind: "saved",
          id: "00000000-0000-4000-8000-000000000003",
        },
      },
    });
  await page
    .getByRole("combobox", { name: "Default feed", exact: true })
    .click();
  await page.getByRole("option", { name: "Markers", exact: true }).click();
  await page.getByRole("link", { name: "Return to TV", exact: true }).click();
  const player = page.locator("[data-scene-player]");
  await expect(player).toHaveAttribute("data-playback-key", /marker:10$/);
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  await page.keyboard.press("ArrowDown");
  await expect(player).toHaveAttribute("data-playback-key", /marker:11$/);
  await page.keyboard.press("ArrowDown");
  await expect(player).toHaveAttribute("data-playback-key", /scene:3$/);
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  await expect(page.locator("video[data-playback-video]")).toHaveJSProperty(
    "currentTime",
    0,
  );
  const timeline = page.locator("[data-tv-dock]").getByRole("slider");
  await expect(timeline).toHaveAttribute("aria-valuemax", "12");
  await expect(page.locator("video[data-playback-video]")).toHaveCount(1);
  await page.screenshot({ path: "/tmp/stash-tv-extra-scenes-playback.png" });

  await page.getByRole("button", { name: "Navigation", exact: true }).click();
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  await page
    .getByRole("main")
    .getByRole("link", { name: "TV", exact: true })
    .click();
  await expect(selector).toContainText("Short clip performers");
  await page.screenshot({ path: "/tmp/stash-tv-extra-scenes-settings.png" });
  await selector.click();
  await page
    .getByRole("option", { name: "None — markers only", exact: true })
    .click();
  await expect
    .poll(() => page.evaluate(() => window.tvFixtureSaveAttempts.at(-1)))
    .toMatchObject({ key: "tv", value: { markerSceneFilter: null } });
  await page.getByRole("link", { name: "Return to TV", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(() =>
        window.tvFixtureRequests.some(
          (request) => request.name === "TvMarkers",
        ),
      ),
    )
    .toBe(true);
});
