import type { Page } from "@playwright/test";
import { expect, test as fixtureTest } from "./test";

declare global {
  interface Window {
    coverPlayback: { video: HTMLVideoElement; events: string[] };
  }
}

async function action(page: Page, mobile: boolean, name: string) {
  await page
    .getByRole("button", {
      name: mobile ? "Entity actions" : "Operations",
      exact: true,
    })
    .click();
  await page
    .getByRole(mobile ? "button" : "menuitem", { name, exact: true })
    .click();
}

fixtureTest.beforeEach(async ({ page }) => {
  await page.route("**/scene/1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/stream")) {
      await route.fulfill({
        response: await route.fetch({
          url: new URL("/media/audio.mp4", url).href,
        }),
      });
    } else if (/\/streams\.(stop|keepalive)$/.test(url.pathname))
      await route.fulfill({ status: 204 });
    else throw new Error(`Unexpected media request: ${url.pathname}`);
  });
  await page.route("**/covers/*", async (route) => {
    await route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="90"><rect width="160" height="90" fill="teal"/></svg>',
    });
  });
});

for (const width of [390, 1280]) {
  const mobile = width < 1024;
  fixtureTest.describe(
    `${mobile ? "mobile" : "desktop"} generated covers`,
    () => {
      fixtureTest.use({ viewport: { width, height: 844 } });

      for (const kind of ["current", "default", "dialog"] as const) {
        fixtureTest(
          `${kind} cover updates the cached list after leaving details`,
          async ({ page }) => {
            await page.goto("/scene-cover-fixture/scenes");
            const card = page.getByTestId("scene-card");
            await expect(card.locator("img")).toHaveAttribute(
              "src",
              /old\.jpg/,
            );
            await card.getByText("Scene 1", { exact: true }).click();
            await expect(page.locator("video")).toHaveCount(1);
            await expect
              .poll(() =>
                page
                  .locator("video")
                  .evaluate((video: HTMLVideoElement) => video.readyState),
              )
              .toBeGreaterThanOrEqual(2);
            if (kind === "dialog") {
              await action(page, mobile, "Generate…");
              const dialog = page.getByRole("dialog", {
                name: "Generate",
                exact: true,
              });
              await dialog
                .getByRole("checkbox", { name: "Scene covers", exact: true })
                .check();
              await dialog
                .getByRole("button", { name: "Generate", exact: true })
                .click();
              await expect
                .poll(() =>
                  page.evaluate(() => window.coverFixture.generations.length),
                )
                .toBe(1);
            } else {
              await action(
                page,
                mobile,
                kind === "current"
                  ? "Generate thumbnail from current"
                  : "Generate default thumbnail",
              );
              await expect
                .poll(() =>
                  page.evaluate(() => window.coverFixture.screenshots.length),
                )
                .toBe(1);
              const variables = await page.evaluate(
                () => window.coverFixture.screenshots[0],
              );
              if (kind === "current")
                expect(variables?.at).toEqual(expect.any(Number));
              else expect(variables?.at).toBeUndefined();
            }
            await page
              .getByRole("button", { name: "Back", exact: true })
              .click();
            await expect(card.locator("img")).toHaveAttribute(
              "src",
              /old\.jpg/,
            );
            const listRequests = await page.evaluate(
              () =>
                window.coverFixture.requests.filter(
                  (name) => name === "FindScenesMobile",
                ).length,
            );
            await page.evaluate(() => {
              window.coverFixture.finished = true;
            });
            await expect(card.locator("img")).toHaveAttribute(
              "src",
              /new\.jpg/,
            );
            await expect(card.locator("source").first()).toHaveAttribute(
              "srcset",
              /new\.avif/,
            );
            expect(
              await page.evaluate(
                () =>
                  window.coverFixture.requests.filter(
                    (name) => name === "FindScenesMobile",
                  ).length,
              ),
            ).toBe(listRequests);
            expect(
              await page.evaluate(
                () =>
                  window.coverFixture.requests.filter(
                    (name) => name === "FindSceneCovers",
                  ).length,
              ),
            ).toBe(1);
          },
        );
      }

      fixtureTest(
        "finishing a cover preserves playback and an in-progress edit",
        async ({ page }) => {
          await page.goto("/scene-cover-fixture/scenes/1");
          const video = page.locator("video");
          const player = page.locator("[data-scene-player]");
          await player
            .getByRole("button", { name: "Play", exact: true })
            .first()
            .click();
          await expect
            .poll(() =>
              video.evaluate(
                (element: HTMLVideoElement) => element.currentTime,
              ),
            )
            .toBeGreaterThan(0.5);
          await action(page, mobile, "Generate thumbnail from current");
          await expect
            .poll(() =>
              page.evaluate(() => window.coverFixture.screenshots.length),
            )
            .toBe(1);
          if (mobile) await action(page, true, "Edit");
          else
            await page
              .getByRole("button", { name: "Edit", exact: true })
              .click();
          const title = page.getByRole("textbox", {
            name: "Title",
            exact: true,
          });
          await title.fill("My unfinished edit");
          const before = await video.evaluate((element: HTMLVideoElement) => {
            window.coverPlayback = { video: element, events: [] };
            for (const name of ["emptied", "loadstart", "pause", "seeking"])
              element.addEventListener(name, () =>
                window.coverPlayback.events.push(name),
              );
            window.coverFixture.finished = true;
            return {
              time: element.currentTime,
              src: element.currentSrc,
              requests: window.coverFixture.requests.filter(
                (name) => name === "FindScene",
              ).length,
            };
          });
          await expect
            .poll(() =>
              page.evaluate(
                () =>
                  window.coverFixture.requests.filter(
                    (name) => name === "FindSceneCovers",
                  ).length,
              ),
            )
            .toBe(1);
          await expect
            .poll(() =>
              video.evaluate(
                (element: HTMLVideoElement) => element.currentTime,
              ),
            )
            .toBeGreaterThan(before.time + 1.5);
          expect(
            await video.evaluate((element: HTMLVideoElement) => ({
              same: window.coverPlayback.video === element,
              events: window.coverPlayback.events,
              src: element.currentSrc,
              paused: element.paused,
              requests: window.coverFixture.requests.filter(
                (name) => name === "FindScene",
              ).length,
            })),
          ).toEqual({
            same: true,
            events: [],
            src: before.src,
            paused: false,
            requests: before.requests,
          });
          await expect(title).toHaveValue("My unfinished edit");
          await expect(title).toBeFocused();
        },
      );
    },
  );
}
