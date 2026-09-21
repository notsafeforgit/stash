import type { Page } from "@playwright/test";
import { expect, holdForContextMenu, test as fixtureTest } from "./test";

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
              /new-thumbnail\.jpg/,
            );
            await expect(card.locator("source").first()).toHaveAttribute(
              "srcset",
              /new-thumbnail\.avif/,
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
        "performer frame preserves playback, captures the selected time and keeps unsaved edits",
        async ({ page }) => {
          await page.goto("/scene-cover-fixture/scenes/1");
          const video = page.locator("video");
          await page
            .locator("[data-scene-player]")
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
          if (!mobile)
            await page
              .getByRole("button", { name: "Edit", exact: true })
              .click();
          const title = page.getByRole("textbox", {
            name: "Title",
            exact: true,
          });
          if (!mobile) await title.fill("Unfinished title");
          const before = await video.evaluate((element: HTMLVideoElement) => {
            window.coverPlayback = { video: element, events: [] };
            for (const name of ["emptied", "loadstart", "pause", "seeking"]) {
              element.addEventListener(name, () =>
                window.coverPlayback.events.push(name),
              );
            }
            return {
              time: element.currentTime,
              src: element.currentSrc,
              requests: window.coverFixture.requests.filter(
                (name) => name === "FindScene",
              ).length,
            };
          });
          await action(page, mobile, "Generate performer image from current");
          const dialog = page.getByRole("dialog", {
            name: "Set as performer image",
          });
          await expect(dialog).toBeVisible();
          const selectedBefore = await video.evaluate(
            (element: HTMLVideoElement) => element.currentTime,
          );
          // Choosing a performer later must not change which frame is generated.
          await expect
            .poll(() =>
              video.evaluate(
                (element: HTMLVideoElement) => element.currentTime,
              ),
            )
            .toBeGreaterThan(selectedBefore + 0.75);
          await dialog
            .getByRole("button", { name: "Performer 2", exact: true })
            .click();
          await expect(dialog).not.toBeVisible();
          const updates = await page.evaluate(
            () => window.coverFixture.performerUpdates,
          );
          expect(updates).toHaveLength(1);
          expect(updates[0]?.id).toBe("2");
          expect(updates[0]?.image.scene?.id).toBe("1");
          expect(updates[0]?.image.scene?.at).toBeGreaterThanOrEqual(
            before.time,
          );
          expect(updates[0]?.image.scene?.at).toBeLessThanOrEqual(
            selectedBefore,
          );
          await expect
            .poll(() =>
              video.evaluate(
                (element: HTMLVideoElement) => element.currentTime,
              ),
            )
            .toBeGreaterThan(selectedBefore + 1.5);
          expect(
            await video.evaluate((element: HTMLVideoElement) => ({
              same: window.coverPlayback.video === element,
              events: window.coverPlayback.events,
              src: element.currentSrc,
              paused: element.paused,
              requests: window.coverFixture.requests.filter(
                (name) => name === "FindScene",
              ).length,
              screenshots: window.coverFixture.screenshots.length,
              generations: window.coverFixture.generations.length,
            })),
          ).toEqual({
            same: true,
            events: [],
            src: before.src,
            paused: false,
            requests: before.requests,
            screenshots: 0,
            generations: 0,
          });
          if (!mobile) await expect(title).toHaveValue("Unfinished title");
          await expect(page).toHaveURL(/\/scenes\/1/);
        },
      );

      fixtureTest(
        "performer frame at zero can be retried without resuming paused playback",
        async ({ page }) => {
          await page.goto("/scene-cover-fixture/scenes/1");
          const video = page.locator("video");
          await expect
            .poll(() =>
              video.evaluate((element: HTMLVideoElement) => element.readyState),
            )
            .toBeGreaterThanOrEqual(2);
          await page.evaluate(() => {
            window.coverFixture.failPerformerUpdate = true;
          });
          await action(page, mobile, "Generate performer image from current");
          const dialog = page.getByRole("dialog", {
            name: "Set as performer image",
          });
          await dialog
            .getByRole("button", { name: "Performer 1", exact: true })
            .click();
          await expect(
            page.getByText("Image update failed", { exact: true }),
          ).toBeVisible();
          await expect(dialog).toBeVisible();
          await page.evaluate(() => {
            window.coverFixture.failPerformerUpdate = false;
          });
          await dialog
            .getByRole("button", { name: "Performer 1", exact: true })
            .click();
          await expect(dialog).not.toBeVisible();
          expect(
            await page.evaluate(() => window.coverFixture.performerUpdates),
          ).toEqual([
            { id: "1", image: { scene: { id: "1", at: 0 } } },
            { id: "1", image: { scene: { id: "1", at: 0 } } },
          ]);
          expect(
            await video.evaluate((element: HTMLVideoElement) => ({
              paused: element.paused,
              time: element.currentTime,
            })),
          ).toEqual({ paused: true, time: 0 });
        },
      );

      for (const target of ["choose", "performer"] as const) {
        for (const view of ["card", "row"] as const) {
          fixtureTest(
            `scene ${view} cover updates the ${target} performer in place`,
            async ({ page }) => {
              await page.goto(
                `/scene-cover-fixture/scenes?playing${target === "performer" ? "&performer" : ""}`,
              );
              const video = page.locator("video");
              await page
                .locator("[data-scene-player]")
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
              const before = await video.evaluate(
                (element: HTMLVideoElement) => {
                  window.coverPlayback = { video: element, events: [] };
                  for (const name of [
                    "emptied",
                    "loadstart",
                    "pause",
                    "seeking",
                  ]) {
                    element.addEventListener(name, () =>
                      window.coverPlayback.events.push(name),
                    );
                  }
                  return {
                    time: element.currentTime,
                    src: element.currentSrc,
                    requests: window.coverFixture.requests.filter(
                      (name) => name === "FindScene",
                    ).length,
                  };
                },
              );
              const source =
                view === "card"
                  ? page
                      .getByTestId("scene-card")
                      .locator('[data-slot="context-menu-trigger"]')
                  : page.getByTestId("scene-row");
              const firstImage = page.getByTestId("performer-image-1");
              const secondImage = page.getByTestId("performer-image-2");
              await expect(firstImage).toHaveAttribute(
                "src",
                /performer-1-old/,
              );
              const menu = page.getByRole("menu");
              if (mobile) await holdForContextMenu(source, menu);
              else await source.click({ button: "right" });
              await menu
                .getByRole("menuitem", {
                  name: "Set as performer image",
                  exact: true,
                })
                .click();
              const dialog = page.getByRole("dialog", {
                name: "Set as performer image",
              });
              if (target === "choose") {
                await dialog
                  .getByRole("button", { name: "Performer 2", exact: true })
                  .click();
              } else {
                await expect(dialog).not.toBeVisible();
              }
              const id = target === "choose" ? "2" : "1";
              await expect(
                page.getByTestId(`performer-image-${id}`),
              ).toHaveAttribute("src", new RegExp(`performer-${id}-new`));
              await expect(
                target === "choose" ? firstImage : secondImage,
              ).toHaveAttribute("src", /old\.jpg/);
              expect(
                await page.evaluate(() => window.coverFixture.performerUpdates),
              ).toEqual([{ id, image: { scene: { id: "1" } } }]);
              expect(
                await page.evaluate(() =>
                  window.coverFixture.requests.filter(
                    (name) => name === "FindScenesMobile",
                  ),
                ),
              ).toHaveLength(1);
              await expect(
                page.getByTestId("scene-card").locator("img"),
              ).toHaveAttribute("src", /old\.jpg/);
              await expect
                .poll(() =>
                  video.evaluate(
                    (element: HTMLVideoElement) => element.currentTime,
                  ),
                )
                .toBeGreaterThan(before.time + 1);
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
              await expect(page).toHaveURL(
                /\/scenes\?playing=?(?:&performer=?)?$/,
              );
            },
          );
        }
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
