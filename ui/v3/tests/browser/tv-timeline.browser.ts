import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

const timeline = (page: Page) =>
  page.locator("[data-tv-dock]").getByRole("slider");
const time = (seconds: number) =>
  `0:${String(Math.floor(seconds)).padStart(2, "0")}`;

async function open(page: Page, query: string) {
  await page.goto(`/tv-fixture/tv?paused&${query}`);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expect(page.locator("video")).toHaveJSProperty("paused", true);
}

async function expectSceneTime(page: Page, seconds: number) {
  await expect
    .poll(async () =>
      Math.abs(
        (await page
          .locator("video")
          .evaluate((video: HTMLVideoElement) => video.currentTime)) - seconds,
      ),
    )
    .toBeLessThan(0.15);
}

async function clickTimeline(page: Page, fraction: number) {
  const bounds = await page
    .locator("[data-tv-dock] [data-position-scrubber-track]")
    .boundingBox();
  if (!bounds) throw new Error("Missing TV timeline");
  await page.mouse.click(
    bounds.x + bounds.width * fraction,
    bounds.y + bounds.height / 2,
  );
}

test.beforeEach(async ({ page }) => {
  await serveSceneMedia(page);
});

// Seed 37 in the fixture selects marker 8s, position 5.501747s, and a
// 2.877530s random window in its real 12-second scene. Keep these expectations
// independent of the production policy so a full-scene fallback cannot pass.
const segments = [
  { name: "marker", query: "markers", start: 6, end: 8 },
  { name: "resume to scene end", query: "resume", start: 4, end: 12 },
  {
    name: "random marker to scene end",
    query: "start=random-marker",
    start: 8,
    end: 12,
  },
  {
    name: "random position to scene end",
    query: "start=random-position",
    start: 5.501747074536979,
    end: 12,
  },
  { name: "fixed window", query: "resume&window=fixed", start: 4, end: 7 },
  {
    name: "random window",
    query: "resume&window=random",
    start: 4,
    end: 6.8775301550049335,
  },
];

for (const mobile of [true, false]) {
  test.describe(
    mobile ? "mobile segment timeline" : "desktop segment timeline",
    () => {
      test.use({
        isMobile: mobile,
        hasTouch: mobile,
        viewport: mobile
          ? { width: 390, height: 844 }
          : { width: 1280, height: 800 },
      });

      for (const segment of segments) {
        test(`${segment.name} displays and seeks only the selected segment`, async ({
          page,
          browserName,
        }) => {
          await open(page, segment.query);
          const duration = segment.end - segment.start;
          const slider = timeline(page);
          const dock = page.locator("[data-tv-dock]");
          await expectSceneTime(page, segment.start);
          await expect(slider).toHaveAttribute("aria-valuemin", "0");
          await expect
            .poll(async () =>
              Number(await slider.getAttribute("aria-valuemax")),
            )
            .toBeCloseTo(duration, 5);
          await expect(
            dock.getByText(`0:00 / ${time(duration)}`, { exact: true }),
          ).toBeVisible();

          await clickTimeline(page, 0.5);
          await expectSceneTime(page, segment.start + duration / 2);
          await expect
            .poll(async () =>
              Number(await slider.getAttribute("aria-valuenow")),
            )
            .toBeCloseTo(duration / 2, 0);

          if (browserName === "webkit" && segment.end === 12) {
            // Linux WebKit resets a paused MP4 seek at exact EOF to zero,
            // reproducible in plain HTML without the player. Exercise its
            // upper part of the track; exact marker/window ends run in both
            // engines. Leave room for the thumb's touch target at the edge.
            await clickTimeline(page, 0.9);
            const elapsed = Number(await slider.getAttribute("aria-valuenow"));
            expect(elapsed).toBeGreaterThan(duration * 0.8);
            expect(elapsed).toBeLessThan(duration);
            await expectSceneTime(page, segment.start + elapsed);
          } else {
            await slider.press("End");
            await expectSceneTime(page, segment.end);
            await expect(
              dock.getByText(`${time(duration)} / ${time(duration)}`, {
                exact: true,
              }),
            ).toBeVisible();
          }
          await slider.press("Home");
          await slider.press("ArrowLeft");
          await expectSceneTime(page, segment.start);
          await expect(
            dock.getByText(`0:00 / ${time(duration)}`, { exact: true }),
          ).toBeVisible();
          await expect(page.locator("video")).toHaveJSProperty("paused", true);
        });
      }
    },
  );
}

test("a scene segment survives quality changes and a settings round trip", async ({
  page,
}) => {
  await open(page, "resume");
  await clickTimeline(page, 0.5);
  await expectSceneTime(page, 8);
  const elapsed = Number(await timeline(page).getAttribute("aria-valuenow"));
  await page.locator("video").evaluate((video: HTMLVideoElement) => {
    window.tvFixtureVideo = video;
  });
  await page.getByRole("button", { name: "Playback", exact: true }).click();
  await page.getByRole("menuitem", { name: "Quality", exact: true }).click();
  await page.getByRole("combobox", { name: "Current item quality" }).click();
  await page.getByRole("option", { name: "HLS (240p)", exact: true }).click();
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expect
    .poll(async () =>
      Number(await timeline(page).getAttribute("aria-valuenow")),
    )
    .toBeCloseTo(elapsed, 0);
  await expect(timeline(page)).toHaveAttribute("aria-valuemax", "8");
  expect(
    await page
      .locator("video")
      .evaluate((video) => video === window.tvFixtureVideo),
  ).toBe(true);

  await page.getByRole("button", { name: "Navigation", exact: true }).click();
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  await page
    .getByRole("main")
    .getByRole("link", { name: "TV", exact: true })
    .click();
  await page.getByRole("link", { name: "Return to TV" }).click();
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expect(timeline(page)).toHaveAttribute("aria-valuemin", "0");
  await expect(timeline(page)).toHaveAttribute("aria-valuemax", "8");
  await expect
    .poll(async () =>
      Number(await timeline(page).getAttribute("aria-valuenow")),
    )
    .toBeCloseTo(elapsed, 0);
  await timeline(page).press("Home");
  await expectSceneTime(page, 4);
});

test("marker navigation resets the timeline and replay returns to segment start", async ({
  page,
}) => {
  await open(page, "markers");
  await page.keyboard.press("ArrowDown");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    /marker:11$/,
  );
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expectSceneTime(page, 8);
  await expect(
    page.locator("[data-tv-dock]").getByText("0:00 / 0:02", { exact: true }),
  ).toBeVisible();
  await timeline(page).press("End");
  await expectSceneTime(page, 10);
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect(page.locator("video")).toHaveJSProperty("paused", false);
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeLessThan(9);
  await page.getByRole("button", { name: "Pause", exact: true }).click();
});
