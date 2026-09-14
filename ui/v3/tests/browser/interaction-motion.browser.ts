import { test, expect, chooseSection } from "./test";

interface RevealObservation {
  kind: string;
  duration: number;
  finished: boolean;
  height: number;
  contentOpacity: string;
  contentTransform: string;
}
interface LightboxObservation {
  entering: number[];
  exiting: number[];
  removed: boolean;
}
declare global {
  interface Window {
    contentMotion: RevealObservation[];
    lightboxMotion: LightboxObservation[];
    interactionSnapshots: number;
  }
}

test.beforeEach(async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.addInitScript(() => {
    window.contentMotion = [];
    window.lightboxMotion = [];
    window.interactionSnapshots = 0;
    const native = document.startViewTransition?.bind(document);
    if (native)
      document.startViewTransition = (...args) => {
        window.interactionSnapshots++;
        return native(...args);
      };
    const animate = Element.prototype.animate;
    Element.prototype.animate = function (...args) {
      const animation = animate.apply(this, args);
      if (!this.hasAttribute("data-content-reveal")) return animation;
      const parent = this.parentElement;
      if (!parent) throw new Error("Missing content viewport");
      const style = getComputedStyle(parent);
      const observation: RevealObservation = {
        kind: animation.id,
        duration: Number(animation.effect?.getTiming().duration),
        finished: false,
        height: this.getBoundingClientRect().height,
        contentOpacity: style.opacity,
        contentTransform: style.transform,
      };
      window.contentMotion.push(observation);
      const finish = () => {
        observation.finished = true;
      };
      void animation.finished.then(finish, finish);
      return animation;
    };
    const seen = new WeakSet<Element>();
    new MutationObserver(() => {
      for (const portal of document.querySelectorAll(".yarl__portal")) {
        if (seen.has(portal)) continue;
        seen.add(portal);
        const observation: LightboxObservation = {
          entering: [],
          exiting: [],
          removed: false,
        };
        window.lightboxMotion.push(observation);
        const sample = () => {
          if (!portal.isConnected) {
            observation.removed = true;
            return;
          }
          const frames = portal.classList.contains("yarl__portal_open")
            ? observation.entering
            : observation.exiting;
          frames.push(Number(getComputedStyle(portal).opacity));
          requestAnimationFrame(sample);
        };
        requestAnimationFrame(sample);
      }
    }).observe(document, { childList: true, subtree: true });
  });
});

for (const layout of ["collection", "media"] as const) {
  test(`${layout} tabs reveal their own panel and keep visited content`, async ({
    page,
  }) => {
    await page.goto(layout === "media" ? "/?media" : "/");
    const original = await page.getByTestId("scenes-list").elementHandle();
    await chooseSection(page, "Images");
    await expect
      .poll(() => page.evaluate(() => window.contentMotion[0]?.finished))
      .toBe(true);
    const reveal = await page.evaluate(() => window.contentMotion[0]);
    expect(reveal).toMatchObject({
      kind: "detail-tab",
      duration: 140,
      contentOpacity: "1",
      contentTransform: "none",
    });
    expect(reveal?.height).toBeLessThanOrEqual(844);
    expect(reveal?.height).toBeGreaterThan(0);
    await chooseSection(page, "Scenes");
    expect(await original?.evaluate((element) => element.isConnected)).toBe(
      true,
    );
    await page.emulateMedia({ reducedMotion: "reduce" });
    const count = await page.evaluate(() => window.contentMotion.length);
    await chooseSection(page, "Images");
    expect(await page.evaluate(() => window.contentMotion.length)).toBe(count);
  });
}

test("mobile grid columns reveal immediately without closing their options", async ({
  page,
}) => {
  await page.goto("/?standalone");
  const list = await page.getByTestId("standalone-list").elementHandle();
  await page.getByRole("button", { name: "View options", exact: true }).tap();
  const drawer = page.getByRole("dialog", {
    name: "View options",
    exact: true,
  });
  await drawer.getByRole("button", { name: "1 column", exact: true }).tap();
  await expect(drawer).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => window.contentMotion[0]?.finished))
    .toBe(true);
  expect(await page.evaluate(() => window.contentMotion[0])).toMatchObject({
    kind: "list-view",
    duration: 140,
    contentOpacity: "1",
    contentTransform: "none",
  });
  expect(await list?.evaluate((element) => element.isConnected)).toBe(true);
  for (let index = 0; index < 16; index++) {
    await drawer
      .getByRole("button", {
        name: index % 2 === 0 ? "2 columns" : "1 column",
        exact: true,
      })
      .dispatchEvent("click");
  }
  await expect
    .poll(() =>
      page.evaluate(() =>
        window.contentMotion.every((entry) => entry.finished),
      ),
    )
    .toBe(true);
  expect(await page.evaluate(() => window.interactionSnapshots)).toBe(0);
});

test("desktop display modes and zoom reveal only the list", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.goto("/?standalone");
  for (const [index, label] of ["Table", "Grid", "Zoom in"].entries()) {
    const control =
      label === "Zoom in"
        ? page.getByTitle(label, { exact: true })
        : page.getByRole("button", { name: label, exact: true });
    await control.click();
    await expect
      .poll(() =>
        page.evaluate((index) => window.contentMotion[index]?.finished, index),
      )
      .toBe(true);
  }
  const observations = await page.evaluate(() => window.contentMotion);
  expect(observations).toHaveLength(3);
  expect(
    observations.every(
      (entry) =>
        entry.kind === "list-view" &&
        entry.contentOpacity === "1" &&
        entry.contentTransform === "none",
    ),
  ).toBe(true);
  expect(await page.evaluate(() => window.interactionSnapshots)).toBe(0);
});

test("opening and leaving the focused viewer reveals the same media container", async ({
  page,
}) => {
  await page.goto("/motion-viewer");
  const original = await page.getByTestId("retained-viewer").elementHandle();
  await page.getByRole("button", { name: "Open viewer", exact: true }).tap();
  await expect
    .poll(() => page.evaluate(() => window.contentMotion[0]?.finished))
    .toBe(true);
  await page
    .getByRole("button", { name: "Close scene viewer", exact: true })
    .tap();
  await expect
    .poll(() => page.evaluate(() => window.contentMotion[1]?.finished))
    .toBe(true);
  expect(await original?.evaluate((element) => element.isConnected)).toBe(true);
  expect(
    await page.evaluate(() => window.contentMotion.map((entry) => entry.kind)),
  ).toEqual(["focused-view", "focused-view"]);
  expect(await page.evaluate(() => window.interactionSnapshots)).toBe(0);
});

for (const kind of ["images", "scenes"] as const) {
  for (const dismissal of ["Close", "Escape", "browser Back"] as const) {
    test(`${kind} lightbox fades in and out using ${dismissal}`, async ({
      page,
    }) => {
      await page.goto("/");
      await page.goto("/motion");
      await page
        .getByRole("button", { name: `Open ${kind}`, exact: true })
        .tap();
      const portal = page.locator(".yarl__portal");
      await expect(portal).toHaveCSS("opacity", "1");
      if (dismissal === "Close") {
        await portal
          .getByRole("button", { name: "Close", exact: true })
          .dispatchEvent("click");
        await portal
          .getByRole("button", { name: "Close", exact: true })
          .dispatchEvent("click");
      } else if (dismissal === "Escape") await page.keyboard.press("Escape");
      else await page.goBack();
      await expect(portal).toHaveCount(0);
      await expect(page.getByTestId("close-count")).toHaveText("1");
      const observation = await page.evaluate(() => window.lightboxMotion[0]);
      expect(
        observation?.entering.some((value) => value > 0 && value < 1),
      ).toBe(true);
      expect(observation?.exiting.some((value) => value > 0 && value < 1)).toBe(
        true,
      );
      expect(new URL(page.url()).pathname).toBe("/motion");
      await page.goBack();
      expect(new URL(page.url()).pathname).toBe("/");
    });
  }
  test(`${kind} lightbox follows Reduce Motion and can reopen cleanly`, async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.goto("/motion");
    for (let index = 0; index < 3; index++) {
      await page
        .getByRole("button", { name: `Open ${kind}`, exact: true })
        .tap();
      const portal = page.locator(".yarl__portal");
      await expect(portal).toHaveCount(1);
      await expect(portal).toHaveCSS("transition-duration", "0s");
      await page.goBack();
      await expect(portal).toHaveCount(0);
      await expect(page.getByTestId("close-count")).toHaveText(
        String(index + 1),
      );
    }
    expect(await page.evaluate(() => window.interactionSnapshots)).toBe(0);
  });
}
