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
  mediaStable: boolean;
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
          mediaStable: true,
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
          const surface = portal.querySelector("[data-lightbox-reveal]");
          const opacity = Number(getComputedStyle(portal).opacity);
          frames.push(
            opacity *
              (1 - (surface ? Number(getComputedStyle(surface).opacity) : 0)),
          );
          const slide = portal.querySelector(".yarl__slide_current");
          if (slide) {
            const style = getComputedStyle(slide);
            observation.mediaStable &&=
              opacity === 1 &&
              style.scale === "none" &&
              style.transform === "none";
          }
          requestAnimationFrame(sample);
        };
        requestAnimationFrame(sample);
      }
    }).observe(document, { childList: true, subtree: true });
  });
});

const touchPointer = {
  pointerId: 71,
  pointerType: "touch",
  isPrimary: true,
  button: 0,
  clientX: 60,
  clientY: 300,
};

for (const kind of ["image", "scene"] as const) {
  test(`${kind} card responds before opening and the lightbox reveals stable media`, async ({
    page,
  }) => {
    await page.goto("/motion");
    const card = page.locator(`article[data-id="${kind}"]`);
    const preview = card.locator("[data-entity-card-preview]");
    const original = await card.elementHandle();
    await preview.dispatchEvent("pointerdown", { ...touchPointer, buttons: 1 });
    await expect
      .poll(() =>
        card.evaluate((element) =>
          Number.parseFloat(getComputedStyle(element).scale),
        ),
      )
      .toBeGreaterThan(1.02);
    await expect(preview).toHaveCSS("scale", "none");
    await expect(page.locator(".yarl__portal")).toHaveCount(0);
    await preview.dispatchEvent("pointerup", { ...touchPointer, buttons: 0 });
    await preview.dispatchEvent("click");
    const portal = page.locator(".yarl__portal");
    await expect(portal.locator("[data-lightbox-reveal]")).toHaveCSS(
      "opacity",
      "0",
    );
    expect(await original?.evaluate((element) => element.isConnected)).toBe(
      true,
    );
    const observation = await page.evaluate(() => window.lightboxMotion[0]);
    expect(
      observation?.entering.some((opacity) => opacity > 0.05 && opacity < 0.95),
    ).toBe(true);
    expect(observation?.mediaStable).toBe(true);
    await expect
      .poll(() =>
        portal.evaluate((element) =>
          [
            element,
            element.querySelector<HTMLElement>("[data-lightbox-reveal]"),
          ].every((node) => !node?.style.willChange),
        ),
      )
      .toBe(true);
    await portal
      .getByRole("button", { name: "Close", exact: true })
      .dispatchEvent("click");
    await expect(portal).toHaveCount(0);
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            document
              .getAnimations()
              .filter((animation) => /^(card-|lightbox-)/.test(animation.id))
              .length,
        ),
      )
      .toBe(0);
  });
}

test("scrolling cancels card feedback, while nested controls and selection keep their own actions", async ({
  page,
}) => {
  await page.goto("/motion");
  const card = page.locator('article[data-id="image"]');
  const preview = card.locator("[data-entity-card-preview]");
  await preview.dispatchEvent("pointerdown", { ...touchPointer, buttons: 1 });
  await preview.dispatchEvent("pointermove", {
    ...touchPointer,
    buttons: 1,
    clientY: 330,
  });
  await preview.dispatchEvent("pointerup", {
    ...touchPointer,
    buttons: 0,
    clientY: 330,
  });
  await expect(card).toHaveCSS("scale", "none");
  await expect(page.locator(".yarl__portal")).toHaveCount(0);
  const action = card.getByRole("button", { name: "Card action", exact: true });
  await action.dispatchEvent("pointerdown", { ...touchPointer, buttons: 1 });
  await expect(card).toHaveCSS("scale", "none");
  await action.click();
  await expect(page.getByTestId("selection")).toHaveText("true");
  await page.getByRole("button", { name: "Select cards", exact: true }).click();
  await preview.dispatchEvent("pointerdown", { ...touchPointer, buttons: 1 });
  await expect(card).toHaveCSS("scale", "none");
  await preview.dispatchEvent("click");
  await expect(page.getByTestId("selection")).toHaveText("false");
  await expect(page.locator(".yarl__portal")).toHaveCount(0);
});

test("Reduce Motion cancels a held press and skips the lightbox reveal", async ({
  page,
}) => {
  await page.goto("/motion");
  const card = page.locator('article[data-id="image"]');
  const preview = card.locator("[data-entity-card-preview]");
  await preview.dispatchEvent("pointerdown", { ...touchPointer, buttons: 1 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(card).toHaveCSS("scale", "none");
  await preview.dispatchEvent("pointerup", { ...touchPointer, buttons: 0 });
  await preview.dispatchEvent("click");
  await expect(page.locator("[data-lightbox-reveal]")).toHaveCSS(
    "opacity",
    "0",
  );
  expect(
    await page.evaluate(
      () =>
        document
          .getAnimations()
          .filter((animation) => /^(card-|lightbox-)/.test(animation.id))
          .length,
    ),
  ).toBe(0);
});

test("an entrance can be closed immediately and reopened without leftover effects", async ({
  page,
}) => {
  await page.goto("/motion");
  for (let index = 1; index <= 3; index++) {
    await page
      .getByRole("button", { name: "Open scenes", exact: true })
      .dispatchEvent("click");
    const portal = page.locator(".yarl__portal");
    await portal
      .getByRole("button", { name: "Close", exact: true })
      .dispatchEvent("click");
    await expect(portal).toHaveCount(0);
    await expect(page.getByTestId("close-count")).toHaveText(String(index));
    expect(
      await page.evaluate(
        () =>
          document
            .getAnimations()
            .filter((animation) => /^lightbox-/.test(animation.id)).length,
      ),
    ).toBe(0);
  }
});

test("browsers without Web Animations retain the library fade and dismissal", async ({
  page,
}) => {
  await page.goto("/motion");
  await page.evaluate(() => {
    Object.defineProperty(Element.prototype, "animate", {
      value: undefined,
      configurable: true,
    });
  });
  await page.getByRole("button", { name: "Open images", exact: true }).tap();
  const portal = page.locator(".yarl__portal");
  await expect(portal).toHaveCSS("opacity", "1");
  await expect(portal.locator("[data-lightbox-reveal]")).toHaveCSS(
    "opacity",
    "0",
  );
  await page.goBack();
  await expect(portal).toHaveCount(0);
  await expect(page.getByTestId("close-count")).toHaveText("1");
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

test.describe("lightbox painting at phone pixel density", () => {
  test.use({ deviceScaleFactor: 3 });
  for (const theme of ["light", "dark"] as const) {
    test(`image reveal paints intermediate pixels in ${theme} mode`, async ({
      page,
    }) => {
      await page.goto("/motion");
      await page.evaluate((theme) => {
        document.documentElement.classList.toggle("dark", theme === "dark");
        const animate = Element.prototype.animate;
        Element.prototype.animate = function (...args) {
          const animation = animate.apply(this, args);
          if (animation.id === "lightbox-enter") {
            animation.pause();
            animation.currentTime = 0;
          }
          return animation;
        };
      }, theme);
      await page
        .getByRole("button", { name: "Open images", exact: true })
        .tap();
      const surface = page.locator("[data-lightbox-reveal]");
      await expect
        .poll(() =>
          surface.evaluate((element) => element.getAnimations().length),
        )
        .toBe(1);
      const image = page.locator(".yarl__slide_current .yarl__slide_image");
      const covered = await image.screenshot();
      await surface.evaluate((element) => {
        const animation = element.getAnimations()[0];
        if (!animation) throw new Error("Missing lightbox reveal");
        animation.currentTime = 100;
      });
      const partial = await image.screenshot();
      await surface.evaluate((element) => element.getAnimations()[0]?.finish());
      await expect(surface).toHaveCSS("opacity", "0");
      const revealed = await image.screenshot();
      expect(covered.equals(partial)).toBe(false);
      expect(partial.equals(revealed)).toBe(false);
      expect(covered.equals(revealed)).toBe(false);
    });
  }
  for (const kind of ["images", "scenes"] as const) {
    test(`${kind} entrance remains visible after expensive first-frame work`, async ({
      page,
    }) => {
      await page.goto("/motion?busy");
      await page
        .getByRole("button", { name: `Open ${kind}`, exact: true })
        .tap();
      await expect(page.locator("[data-lightbox-reveal]")).toHaveCSS(
        "opacity",
        "0",
      );
      const frames = await page.evaluate(
        () => window.lightboxMotion[0]?.entering,
      );
      expect(
        frames?.filter((value) => value > 0.05 && value < 0.95).length,
      ).toBeGreaterThan(2);
    });
  }
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
      await expect(portal.locator("[data-lightbox-reveal]")).toHaveCSS(
        "opacity",
        "0",
      );
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
