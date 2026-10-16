import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

// A tiny PNG is also decoded by browsers in an image/jpeg data URL; tests
// exercise interaction and ownership, while Go tests exercise real JPEGs.
const image =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3ioAAAAASUVORK5CYII=";
const times = [1.125, 1.168331, 1.201664, 1.268331];

for (const width of [390, 1280]) {
  const mobile = width < 1024;
  test.describe(`${mobile ? "mobile" : "desktop"} thumbnail picker`, () => {
    test.use({ viewport: { width, height: 844 } });
    test.beforeEach(async ({ page }) => {
      await serveSceneMedia(page);
      await page.route("**/covers/*", (route) =>
        route.fulfill({
          contentType: "image/png",
          body: Buffer.from(image, "base64"),
        }),
      );
    });
    test("uses decoded frame times without seeking the player or saving previews", async ({
      page,
    }) => {
      const requests: URL[] = [];
      await page.route("**/frame-previews?*", async (route) => {
        const url = new URL(route.request().url());
        requests.push(url);
        await route.fulfill({ json: times.map((at) => ({ at, jpeg: image })) });
      });
      await page.goto("/scene-cover-fixture/scenes/1?hls");
      const video = page.locator("video");
      await expect(video).toHaveCount(1);
      await page
        .locator("[data-scene-player]")
        .getByRole("button", { name: "Play", exact: true })
        .first()
        .click();
      await expect
        .poll(() =>
          video.evaluate((element: HTMLVideoElement) => element.currentTime),
        )
        .toBeGreaterThan(0.1);
      await video.evaluate((element: HTMLVideoElement) => {
        window.coverPlayback = { video: element, events: [] };
        for (const event of ["emptied", "loadstart", "seeking"]) {
          element.addEventListener(event, () =>
            window.coverPlayback.events.push(event),
          );
        }
      });
      async function open() {
        await page
          .getByRole("button", {
            name: mobile ? "Entity actions" : "Operations",
            exact: true,
          })
          .click();
        await page
          .getByRole(mobile ? "button" : "menuitem", {
            name: "Choose thumbnail",
            exact: true,
          })
          .click();
      }
      await open();
      const dialog = page.getByRole("dialog", {
        name: "Choose thumbnail",
        exact: true,
      });
      const strip = dialog.getByRole("group", { name: "Frame previews" });
      await expect(strip.getByRole("button")).toHaveCount(4);
      await expect(dialog.getByAltText("Selected frame preview")).toBeVisible();
      expect(requests[0]?.searchParams.get("mode")).toBe("frames");
      await strip.getByRole("button").nth(2).click();
      await dialog.getByRole("slider").press("ArrowRight");
      await expect(strip.getByRole("button").nth(3)).toHaveAttribute(
        "aria-pressed",
        "true",
      );
      // Picking within a decoded batch needs no further HTTP work.
      expect(requests).toHaveLength(1);
      expect(
        await page.evaluate(() => window.coverFixture.screenshots),
      ).toEqual([]);
      await dialog
        .getByRole("button", { name: "Wider view", exact: true })
        .click();
      await expect.poll(() => requests.length).toBe(2);
      await expect(strip.getByRole("button")).toHaveCount(4);
      expect(requests[1]?.searchParams.get("mode")).toBe("samples");
      await dialog
        .getByRole("button", { name: "Zoom in", exact: true })
        .click();
      await expect.poll(() => requests.length).toBe(3);
      await expect(strip.getByRole("button")).toHaveCount(4);
      await strip.getByRole("button").nth(2).click();
      await dialog
        .getByRole("button", { name: "Use this frame", exact: true })
        .click();
      await expect(dialog).toBeHidden();
      await expect
        .poll(() => page.evaluate(() => window.coverFixture.screenshots))
        .toEqual([{ id: "1", at: times[2] }]);
      expect(
        await page.evaluate(
          () => document.querySelector("video") === window.coverPlayback.video,
        ),
      ).toBe(true);
      expect(await page.evaluate(() => window.coverPlayback.events)).toEqual(
        [],
      );
      // No old preview batch survives closing, even if the lazy dialog stays loaded.
      await page.evaluate(() => {
        window.coverFixture.finished = true;
      });
      await expect(
        page.getByText("Screenshot generated", { exact: true }),
      ).toBeVisible();
      await open();
      await expect.poll(() => requests.length).toBe(4);
      await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
      await expect(dialog).toBeHidden();
      expect(
        await page.evaluate(() => window.coverFixture.screenshots),
      ).toHaveLength(1);
    });

    test("cancel aborts pending extraction and cannot publish a late result", async ({
      page,
    }) => {
      let release: (() => void) | undefined;
      let started = false;
      await page.route("**/frame-previews?*", async (route) => {
        started = true;
        await new Promise<void>((resolve) => {
          release = resolve;
        });
        await route.fulfill({ json: times.map((at) => ({ at, jpeg: image })) });
      });
      await page.goto("/scene-cover-fixture/scenes/1");
      await page
        .getByRole("button", {
          name: mobile ? "Entity actions" : "Operations",
          exact: true,
        })
        .click();
      await page
        .getByRole(mobile ? "button" : "menuitem", {
          name: "Choose thumbnail",
          exact: true,
        })
        .click();
      await expect.poll(() => started).toBe(true);
      const dialog = page.getByRole("dialog", {
        name: "Choose thumbnail",
        exact: true,
      });
      await expect(
        dialog.getByRole("button", { name: "Use this frame", exact: true }),
      ).toBeDisabled();
      const failed = page.waitForEvent("requestfailed", (request) =>
        request.url().includes("frame-previews"),
      );
      await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
      await failed;
      release?.();
      await expect(dialog).toBeHidden();
      expect(
        await page.evaluate(() => window.coverFixture.screenshots),
      ).toEqual([]);
    });
  });
}
