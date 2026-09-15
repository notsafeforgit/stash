import type { Locator, Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test.beforeEach(async ({ page }) => {
  await serveSceneMedia(page, "portrait");
});
async function open(page: Page, query = "") {
  await page.goto(`/tv-fixture/tv?paused&portrait&${query}`);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
}
async function unfocus(page: Page) {
  await page.evaluate(() => {
    if (document.activeElement instanceof HTMLElement)
      document.activeElement.blur();
  });
}
async function openRating(page: Page) {
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await page.getByRole("menuitem", { name: "Rating", exact: true }).click();
  return page.getByRole("dialog", { name: "Rating", exact: true });
}
async function expectRating(page: Page, rating100: number | null) {
  await expect
    .poll(() =>
      page.evaluate(() =>
        window.tvFixtureRequests
          .filter((request) => request.name === "SceneUpdate")
          .at(-1),
      ),
    )
    .toMatchObject({ variables: { input: { id: "1", rating100 } } });
}

// Sample painted frames: checking only settled bounds misses the loading row
// briefly moving the rail (and its anchored popover) during a mutation refresh.
async function watchCounterLayout(page: Page) {
  return page.evaluateHandle(() => {
    const dock = document.querySelector("[data-tv-dock]");
    if (!dock) throw new Error("Missing TV dock");
    const dockBox = dock.getBoundingClientRect();
    let popup: Element | null = null;
    let popupBox: DOMRect | null = null;
    const observed = {
      frames: 0,
      dockShift: 0,
      popupShift: 0,
      popupReplaced: false,
      addOpacity: 1,
      feedLoadingFrames: 0,
    };
    const delta = (before: DOMRect, after: DOMRect) =>
      Math.max(
        ...(["x", "y", "width", "height"] as const).map((key) =>
          Math.abs(before[key] - after[key]),
        ),
      );
    let frameId: number;
    const frame = () => {
      if (dock.querySelector('[role="status"]')) observed.feedLoadingFrames++;
      observed.dockShift = Math.max(
        observed.dockShift,
        delta(dockBox, dock.getBoundingClientRect()),
      );
      const current = document.querySelector("[data-tv-counter]");
      if (current && Number(getComputedStyle(current).opacity) > 0) {
        observed.frames++;
        if (popup && popup !== current) observed.popupReplaced = true;
        popup = current;
        const bounds = current.getBoundingClientRect();
        popupBox ??= bounds;
        observed.popupShift = Math.max(
          observed.popupShift,
          delta(popupBox, bounds),
        );
        const add = current.querySelector('[aria-label="Add O"]');
        if (add)
          observed.addOpacity = Math.min(
            observed.addOpacity,
            Number(getComputedStyle(add).opacity),
          );
      }
      frameId = requestAnimationFrame(frame);
    };
    frameId = requestAnimationFrame(frame);
    return {
      stop() {
        cancelAnimationFrame(frameId);
        return observed;
      },
    };
  });
}

for (const device of ["mobile", "desktop"] as const) {
  test.describe(`TV metadata on ${device}`, () => {
    test.use(
      device === "mobile"
        ? {
            viewport: { width: 390, height: 844 },
            isMobile: true,
            hasTouch: true,
          }
        : {
            viewport: { width: 1280, height: 800 },
            isMobile: false,
            hasTouch: false,
          },
    );
    for (const placement of ["rail", "pinned", "folder"] as const) {
      test(`counter ${placement} anchors its popover and keeps four-digit badges inside controls`, async ({
        page,
      }, testInfo) => {
        await open(
          page,
          `count=9999&counter=${placement}&slow-counter&slow-feed`,
        );
        const activate = (control: Locator) =>
          device === "mobile" ? control.tap() : control.click();
        if (placement === "folder")
          await activate(
            page.getByRole("button", { name: "Edit", exact: true }),
          );
        const trigger = page.getByRole(
          placement === "folder" ? "menuitem" : "button",
          { name: "O-counter", exact: true },
        );
        const badge = trigger.locator("[data-tv-counter-badge]");
        await expect(badge).toHaveText("9999");
        expect(
          await badge.evaluate((element) => {
            const parent = element.closest('button,[role="menuitem"]');
            if (!parent) return false;
            const box = element.getBoundingClientRect();
            const control = parent.getBoundingClientRect();
            return (
              element.scrollWidth <= element.clientWidth &&
              box.left >= control.left &&
              box.right <= control.right &&
              box.bottom <= control.bottom
            );
          }),
        ).toBe(true);
        const initialDock = await page.locator("[data-tv-dock]").boundingBox();
        await page.screenshot({
          path: testInfo.outputPath(`counter-badge-${placement}.png`),
        });
        const layout = await watchCounterLayout(page);
        await activate(trigger);
        const popup = page.locator("[data-tv-counter]");
        await expect(popup).toBeVisible();
        await expect(popup).not.toHaveAttribute("aria-modal", "true");
        await expect(page.locator('[data-slot="dialog-content"]')).toHaveCount(
          0,
        );
        const anchor =
          placement === "folder"
            ? page.getByRole("button", { name: "Edit", exact: true })
            : trigger;
        const anchorBox = await anchor.boundingBox();
        const popupBox = await popup.boundingBox();
        expect(anchorBox).not.toBeNull();
        expect(popupBox).not.toBeNull();
        if (!anchorBox || !popupBox) throw new Error("Missing popover bounds");
        expect(popupBox.x).toBeGreaterThanOrEqual(0);
        expect(popupBox.x + popupBox.width).toBeLessThanOrEqual(
          page.viewportSize()?.width ?? 0,
        );
        expect(popupBox.y + popupBox.height).toBeLessThanOrEqual(
          anchorBox.y + anchorBox.height + 10,
        );
        const add = popup.getByRole("button", { name: "Add O", exact: true });
        await activate(add);
        await expect(add).toBeDisabled();
        await expect(popup).toHaveAttribute("aria-busy", "true");
        if (device === "desktop") await expect(add).toBeFocused();
        // Disabled controls still own focus, but cannot submit duplicate writes.
        await add.dispatchEvent("click");
        await expect(popup).toContainText("10,000");
        const feedLoading = page.locator("[data-tv-dock]").getByRole("status");
        await expect(feedLoading).toBeHidden();
        if (placement !== "folder") {
          await expect(badge).toHaveText("9999+");
          expect(
            await badge.evaluate(
              (element) => element.scrollWidth <= element.clientWidth,
            ),
          ).toBe(true);
        }
        const subtract = popup.getByRole("button", {
          name: "Decrement O",
          exact: true,
        });
        if (device === "desktop") {
          await subtract.focus();
          await page.keyboard.press("Enter");
          await expect(subtract).toBeDisabled();
          await expect(subtract).toBeFocused();
        } else await activate(subtract);
        await expect(popup).toContainText("9,999");
        await expect(feedLoading).toBeHidden();
        expect(await page.locator("[data-tv-dock]").boundingBox()).toEqual(
          initialDock,
        );
        await page.screenshot({
          path: testInfo.outputPath(`counter-popover-${placement}.png`),
        });
        await activate(
          popup.getByRole("button", { name: "Reset", exact: true }),
        );
        await expect(
          popup.getByRole("button", { name: "Decrement O", exact: true }),
        ).toBeDisabled();
        await expect(
          popup.getByRole("button", { name: "Add O", exact: true }),
        ).toBeEnabled();
        await expect(feedLoading).toBeHidden();
        if (device === "desktop")
          await expect(
            popup.getByRole("button", { name: "Reset", exact: true }),
          ).toBeFocused();
        const observed = await layout.evaluate((observer) => observer.stop());
        await layout.dispose();
        expect(observed.frames).toBeGreaterThan(0);
        // The count assertion can finish polling after loading has disappeared.
        // Observe the transient state in the browser, alongside its bounds.
        expect(observed.feedLoadingFrames).toBeGreaterThan(0);
        expect(observed.dockShift, "dock moves during a save").toBeLessThan(1);
        expect(
          observed.popupShift,
          "popover moves during opening or saving",
        ).toBeLessThan(1);
        expect(observed.popupReplaced, "popover remounts during a save").toBe(
          false,
        );
        expect(observed.addOpacity, "controls flash during a save").toBe(1);
        await page.keyboard.press("Escape");
        await expect(popup).toHaveCount(0);
        await expect(anchor).toBeFocused();
        if (placement === "folder") await anchor.click();
        await expect(badge).toHaveText("0");
        expect(
          await page.evaluate(() =>
            window.tvFixtureRequests.filter((request) =>
              ["SceneAddO", "SceneDeleteO", "SceneResetO"].includes(
                request.name,
              ),
            ),
          ),
        ).toMatchObject([
          { name: "SceneAddO", variables: { id: "1" } },
          { name: "SceneDeleteO", variables: { id: "1" } },
          { name: "SceneResetO", variables: { id: "1" } },
        ]);
      });
    }
    test("information lets playback and navigation continue and follows the selected video", async ({
      page,
    }, testInfo) => {
      await open(page);
      const infoButton = page.getByRole("button", {
        name: "Information",
        exact: true,
      });
      await infoButton.click();
      const info = page.getByRole("region", {
        name: "Information",
        exact: true,
      });
      await expect(
        info.getByRole("link", { name: "Scene 1", exact: true }),
      ).toBeVisible();
      await page
        .locator("[data-tv-play-surface]")
        .click({ position: { x: 150, y: 180 } });
      await expect(page.locator("video")).toHaveJSProperty("paused", false);
      await unfocus(page);
      await page.keyboard.press("ArrowDown");
      await expect(
        info.getByRole("link", { name: "Scene 2", exact: true }),
      ).toBeVisible();
      await expect(page.locator("video")).toHaveCount(1);
      await page.screenshot({
        path: testInfo.outputPath("tv-inline-info.png"),
      });
      await page
        .getByRole("button", { name: "Show or hide controls", exact: true })
        .click();
      await expect(info).toHaveCount(0);
    });
    test("whole-star ratings use the shared controls and clear to unrated", async ({
      page,
    }) => {
      await open(page);
      const dialog = await openRating(page);
      await expect(dialog.getByRole("slider")).toHaveCount(0);
      await dialog
        .getByRole("button", { name: "4 stars", exact: true })
        .click();
      await expectRating(page, 80);
      await dialog
        .getByRole("button", { name: "Clear rating", exact: true })
        .click();
      await expectRating(page, null);
    });
    for (const [query, step, max, expected] of [
      ["precision=half", "0.5", "5", 10],
      ["precision=quarter", "0.25", "5", 5],
      ["precision=tenth", "0.1", "5", 2],
      ["decimal", "0.1", "10", 1],
    ] as const) {
      test(`rating precision ${query} writes the configured step`, async ({
        page,
      }) => {
        await open(page, query);
        const dialog = await openRating(page);
        const slider = dialog.getByRole("slider");
        await expect(slider).toHaveCount(1);
        await expect(slider).toHaveAttribute("step", step);
        await expect(slider).toHaveAttribute("max", max);
        await slider.focus();
        await page.keyboard.press("ArrowRight");
        await expectRating(page, expected);
        await expect(slider).toBeEnabled();
        await dialog
          .getByRole("button", { name: "Clear rating", exact: true })
          .click();
        await expectRating(page, null);
      });
    }
  });
}

test("a failed counter mutation leaves its count unchanged and can be retried", async ({
  page,
}) => {
  await open(page, "count=1234&counter-error");
  const trigger = page.getByRole("button", { name: "O-counter", exact: true });
  await trigger.click();
  const popup = page.locator("[data-tv-counter]");
  await popup.getByRole("button", { name: "Add O", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          window.tvFixtureRequests.filter(
            (request) => request.name === "SceneAddO",
          ).length,
      ),
    )
    .toBe(1);
  await expect(
    popup.getByRole("button", { name: "Add O", exact: true }),
  ).toBeEnabled();
  await expect(trigger.locator("[data-tv-counter-badge]")).toHaveText("1234");
  await popup.getByRole("button", { name: "Add O", exact: true }).click();
  await expect(trigger.locator("[data-tv-counter-badge]")).toHaveText("1235");
});

test("the shared decimal slider previews locally and persists one value on release", async ({
  page,
}) => {
  await page.goto("/tv-fixture/rating?decimal");
  await expect
    .poll(() =>
      page.evaluate(() =>
        window.tvFixtureRequests.some(
          (request) => request.name === "FindScene",
        ),
      ),
    )
    .toBe(true);
  const slider = page.getByRole("slider");
  const track = page.locator('[data-slot="slider-track"]');
  await expect(slider).toHaveCount(1);
  const box = await track.boundingBox();
  if (!box) throw new Error("Missing rating track");
  await page.mouse.move(box.x + 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.8, box.y + box.height / 2, {
    steps: 12,
  });
  expect(
    await page.evaluate(() =>
      window.tvFixtureRequests.filter(
        (request) => request.name === "SceneUpdate",
      ),
    ),
  ).toEqual([]);
  await page.mouse.up();
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          window.tvFixtureRequests.filter(
            (request) => request.name === "SceneUpdate",
          ).length,
      ),
    )
    .toBe(1);
  const value = Number(await slider.getAttribute("aria-valuenow"));
  expect(value).toBeGreaterThan(7);
  expect(value).toBeLessThan(9);
});

for (const rotated of [false, true]) {
  test(`compact left-handed TV metadata${rotated ? " with rotation" : ""} keeps controls on screen`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 320, height: 568 });
    await open(page, "left-handed&count=1234&metadata&decimal");
    if (rotated) {
      await page.getByRole("button", { name: "Playback", exact: true }).click();
      await page
        .getByRole("menuitem", { name: "Rotate presentation", exact: true })
        .click();
    }
    const infoButton = page.getByRole("button", {
      name: "Information",
      exact: true,
    });
    await infoButton.click();
    const info = page.getByRole("region", { name: "Information", exact: true });
    await expect(info).toBeVisible();
    expect(
      await info.evaluate(
        (element) => element.scrollWidth <= element.clientWidth,
      ),
    ).toBe(true);
    await page.screenshot({ path: testInfo.outputPath("tv-info-compact.png") });
    await page.getByRole("button", { name: "O-counter", exact: true }).click();
    const counter = page.locator("[data-tv-counter]");
    await expect(counter).toBeVisible();
    const box = await counter.boundingBox();
    if (!box) throw new Error("Missing counter bounds");
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.y).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(320);
    expect(box.y + box.height).toBeLessThanOrEqual(568);
    await page.keyboard.press("Escape");
    const rating = await openRating(page);
    const slider = rating.getByRole("slider");
    await slider.focus();
    await page.keyboard.press("End");
    await expectRating(page, 100);
    await page.screenshot({
      path: testInfo.outputPath("tv-rating-compact.png"),
    });
  });
}
