import { test, expect } from "./test";

interface TransitionObservation {
  direction?: string;
  duration?: number;
  frames: { opacity: number; x: number; contentOpacity: number }[];
  finished: boolean;
}

declare global {
  interface Window {
    observedTransitions: TransitionObservation[];
    nativeRouteSnapshots: number;
  }
}

test.use({ viewport: { width: 1280, height: 800 }, isMobile: false });

test.beforeEach(async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.addInitScript(() => {
    window.observedTransitions = [];
    window.nativeRouteSnapshots = 0;
    const native = document.startViewTransition?.bind(document);
    if (native)
      document.startViewTransition = (...args) => {
        window.nativeRouteSnapshots++;
        return native(...args);
      };
    const animate = Element.prototype.animate;
    Element.prototype.animate = function (...args) {
      const animation = animate.apply(this, args);
      if (!this.hasAttribute("data-route-transition")) return animation;
      const observation: TransitionObservation = {
        direction: animation.id.replace("route-", ""),
        duration: Number(animation.effect?.getTiming().duration),
        frames: [],
        finished: false,
      };
      window.observedTransitions.push(observation);
      const sample = () => {
        const style = getComputedStyle(this);
        const viewport = this.parentElement;
        if (!viewport) throw new Error("Missing route viewport");
        const contentStyle = getComputedStyle(viewport);
        observation.frames.push({
          opacity: 1 - Number(style.opacity),
          x: new DOMMatrixReadOnly(contentStyle.transform).m41,
          contentOpacity: Number(contentStyle.opacity),
        });
        if (!observation.finished) requestAnimationFrame(sample);
      };
      sample();
      void animation.finished.then(
        () => {
          observation.finished = true;
        },
        () => {
          observation.finished = true;
        },
      );
      return animation;
    };
  });
});

test.describe("mobile touch navigation", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true });

  for (const theme of ["light", "dark"] as const) {
    test(`the reveal paints intermediate pixels in ${theme} mode`, async ({
      page,
    }) => {
      await page.goto("/transitions");
      await page.evaluate((theme) => {
        document.documentElement.classList.toggle("dark", theme === "dark");
        const animate = Element.prototype.animate;
        Element.prototype.animate = function (...args) {
          const animation = animate.apply(this, args);
          if (this.hasAttribute("data-route-transition")) {
            animation.pause();
            animation.currentTime = 0;
          }
          return animation;
        };
      }, theme);
      await page.getByRole("link", { name: "Entity 1", exact: true }).tap();
      const surface = page.locator("[data-route-transition]");
      await expect
        .poll(() =>
          surface.evaluate((element) => element.getAnimations().length),
        )
        .toBe(1);
      const heading = page.getByRole("heading", { name: "Entity 1" });
      const covered = await heading.screenshot();
      await surface.evaluate((element) => {
        const animation = element.getAnimations()[0];
        if (!animation) throw new Error("Missing reveal animation");
        animation.currentTime = 40;
      });
      const partial = await heading.screenshot();
      await surface.evaluate((element) => element.getAnimations()[0]?.finish());
      await expect(surface).toBeHidden();
      const revealed = await heading.screenshot();
      // Computed opacity alone cannot catch a compositor that fails to paint.
      expect(covered.equals(partial)).toBe(false);
      expect(partial.equals(revealed)).toBe(false);
      expect(covered.equals(revealed)).toBe(false);
    });
  }

  test("entity taps and on-screen Back reveal content without transforming the page", async ({
    page,
  }) => {
    await page.goto("/transitions");
    await page.getByRole("link", { name: "Entity 1", exact: true }).tap();
    await expect
      .poll(() => page.evaluate(() => window.observedTransitions[0]?.finished))
      .toBe(true);
    await page.getByRole("button", { name: "Back to list" }).tap();
    await expect
      .poll(() => page.evaluate(() => window.observedTransitions[1]?.finished))
      .toBe(true);
    const transitions = await page.evaluate(() => window.observedTransitions);
    expect(transitions).toHaveLength(2);
    for (const [index, direction] of ["forward", "back"].entries()) {
      const transition = transitions[index];
      if (!transition) throw new Error("Missing touch transition");
      expect(transition.direction).toBe(direction);
      const frames = transition.frames ?? [];
      expect(frames.length).toBeGreaterThan(1);
      const start = frames[0];
      if (!start) throw new Error("Missing animation frames");
      expect(start.opacity).toBeLessThan(1);
      expect(start.opacity).toBeGreaterThanOrEqual(0.85);
      expect(
        frames.every((frame) => frame.x === 0 && frame.contentOpacity === 1),
      ).toBe(true);
      expect(frames.some((frame) => frame.opacity > start.opacity)).toBe(true);
    }
  });

  test("image-heavy routes do not start native snapshot capture", async ({
    page,
  }) => {
    await page.goto("/transitions");
    await page.getByRole("link", { name: "Entity 1", exact: true }).tap();
    await page.getByRole("button", { name: "Back to list" }).tap();
    await expect(page.getByRole("heading", { name: "Entities" })).toBeVisible();
    expect(await page.evaluate(() => window.nativeRouteSnapshots)).toBe(0);
  });
});

for (const prefix of ["", "/stash"]) {
  test(`entity navigation and both Back actions animate only content under ${prefix || "/"}`, async ({
    page,
  }) => {
    await page.goto(`${prefix}/transitions`);
    await expect(page.getByRole("heading", { name: "Entities" })).toBeVisible();
    expect(await page.evaluate(() => window.observedTransitions)).toEqual([]);
    const header = await page.locator("header").boundingBox();

    async function expectTransition(count: number, direction: string) {
      await expect
        .poll(() => page.evaluate(() => window.observedTransitions.length))
        .toBe(count);
      await expect
        .poll(() =>
          page.evaluate(() => window.observedTransitions.at(-1)?.finished),
        )
        .toBe(true);
      const observation = await page.evaluate(() =>
        window.observedTransitions.at(-1),
      );
      expect(observation).toMatchObject({
        direction,
        duration: 200,
      });
      expect(await page.locator("header").boundingBox()).toEqual(header);
    }

    await page.getByRole("link", { name: "Entity 1", exact: true }).click();
    await expectTransition(1, "forward");
    await page.getByRole("link", { name: "Next entity" }).click();
    await expect(page.getByRole("heading", { name: "Entity 2" })).toBeVisible();
    await expectTransition(2, "forward");
    await page.goBack();
    await expect(page.getByRole("heading", { name: "Entity 1" })).toBeVisible();
    await expectTransition(3, "back");
    await page.goForward();
    await expectTransition(4, "forward");
    await page.getByRole("button", { name: "Back to list" }).click();
    await expect(page.getByRole("heading", { name: "Entities" })).toBeVisible();
    await expectTransition(5, "back");
  });
}

test("search/hash changes preserve the form and entity changes preserve React state", async ({
  page,
}) => {
  await page.goto("/transitions/1");
  const input = page.getByRole("textbox", { name: "Draft" });
  const original = await input.elementHandle();
  await input.fill("Unfinished draft");
  await page.getByRole("button", { name: "Count 0" }).click();
  await page.getByRole("button", { name: "Details tab" }).click();
  await expect(page).toHaveURL(/tab=details#metadata$/);
  expect(await page.evaluate(() => window.observedTransitions.length)).toBe(0);
  await page.getByRole("link", { name: "Next entity" }).click();
  await expect(page.getByRole("heading", { name: "Entity 2" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Count 1" })).toBeVisible();
  await expect(input).toHaveValue("Unfinished draft");
  expect(await original?.evaluate((element) => element.isConnected)).toBe(true);
  await page.getByRole("button", { name: "Open without motion" }).click();
  await expect(page.getByRole("heading", { name: "Entity 3" })).toBeVisible();
  expect(await page.evaluate(() => window.observedTransitions.length)).toBe(1);
});

test("route motion works without native View Transition support", async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(document, "startViewTransition", {
      value: undefined,
    });
  });
  await page.goto("/transitions");
  await page.getByRole("link", { name: "Entity 1", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => window.observedTransitions[0]?.finished))
    .toBe(true);
  expect(
    await page.evaluate(() => window.observedTransitions[0]),
  ).toMatchObject({
    duration: 200,
    direction: "forward",
  });
  await page.getByRole("button", { name: "Details tab" }).click();
  await expect(page).toHaveURL(/tab=details#metadata$/);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.getByRole("link", { name: "Next entity" }).click();
  await expect(page.getByRole("heading", { name: "Entity 2" })).toBeVisible();
  expect(await page.evaluate(() => window.observedTransitions.length)).toBe(1);
});

test("rapid navigation can interrupt motion and Smart Back can reverse to a sibling", async ({
  page,
}) => {
  await page.goto("/transitions/1");
  await page.getByRole("link", { name: "Next entity" }).click();
  await expect(page.getByRole("heading", { name: "Entity 2" })).toBeVisible();
  // Wait for its first paint, then interrupt the running effect.
  await expect
    .poll(() => page.evaluate(() => window.observedTransitions.length))
    .toBe(1);
  // Dispatch during the animation, without Playwright waiting for stability.
  await page
    .getByRole("button", { name: "Back to entity" })
    .evaluate((element) => {
      if (!(element instanceof HTMLButtonElement))
        throw new Error("Expected a button");
      element.click();
    });
  await expect(page.getByRole("heading", { name: "Entity 1" })).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => window.observedTransitions.length))
    .toBe(2);
  await expect
    .poll(() =>
      page.evaluate(() =>
        window.observedTransitions.every((transition) => transition.finished),
      ),
    )
    .toBe(true);
  expect(
    await page.evaluate(() => window.observedTransitions[1]?.direction),
  ).toBe("back");
  await page.getByRole("button", { name: "Replace entity" }).click();
  await expect(page.getByRole("heading", { name: "Entity 3" })).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => window.observedTransitions[2]?.direction))
    .toBe("replace");
});

test("returning to a scrolled list restores its position with either Back action", async ({
  page,
}) => {
  await page.goto("/stash/transitions");
  const list = page.locator('[data-scroll-restoration-id="transition-list"]');
  await list.evaluate((element) => {
    element.scrollTop = 250;
  });
  for (const back of ["browser", "app"]) {
    await page.getByRole("link", { name: "Entity 22", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Entity 22" }),
    ).toBeVisible();
    if (back === "browser") await page.goBack();
    else await page.getByRole("button", { name: "Back to list" }).click();
    await expect
      .poll(() => list.evaluate((element) => element.scrollTop))
      .toBe(250);
    // Finish the entrance animation before clicking the restored list again.
    await expect
      .poll(() =>
        page.evaluate(() =>
          window.observedTransitions.every((transition) => transition.finished),
        ),
      )
      .toBe(true);
  }
});

test("reduced motion is checked on every navigation", async ({ page }) => {
  await page.goto("/transitions/1");
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.getByRole("link", { name: "Next entity" }).click();
  await expect(page.getByRole("heading", { name: "Entity 2" })).toBeVisible();
  expect(await page.evaluate(() => window.observedTransitions.length)).toBe(0);
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.getByRole("button", { name: "Back to list" }).click();
  await expect
    .poll(() => page.evaluate(() => window.observedTransitions.length))
    .toBe(1);
});

test("unsupported browsers navigate normally", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(Element.prototype, "animate", {
      value: undefined,
    });
  });
  await page.goto("/transitions");
  await page.getByRole("link", { name: "Entity 1", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Entity 1" })).toBeVisible();
  await page.getByRole("button", { name: "Back to list" }).click();
  await expect(page.getByRole("heading", { name: "Entities" })).toBeVisible();
  expect(await page.evaluate(() => window.observedTransitions.length)).toBe(0);
});

test("a slow route keeps its outgoing content until the route can commit", async ({
  page,
}) => {
  await page.goto("/transitions");
  await page.getByRole("link", { name: "Slow entity" }).click();
  await expect(page.getByRole("heading", { name: "Entities" })).toBeVisible();
  expect(await page.evaluate(() => window.observedTransitions.length)).toBe(0);
  await expect(
    page.getByRole("heading", { name: "Entity slow" }),
  ).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => window.observedTransitions.length))
    .toBe(1);
});

test("a long detail load stays navigable and a cancelled load cannot replace the current page", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/transitions");
  await page.getByRole("link", { name: "pending entity" }).click();
  await expect(
    page.getByRole("heading", { name: "Entities", exact: true }),
  ).toBeVisible();
  await expect(
    page.locator('[data-slot="skeleton"]').filter({ visible: true }).first(),
  ).toBeVisible();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await page.evaluate(() => window.releaseDetailLoad?.());
  await expect(
    page.getByRole("heading", { name: "Entities", exact: true }),
  ).toBeVisible();
  await expect(page.locator('[data-slot="skeleton"]')).toHaveCount(0);
});

test.describe("detail load recovery", () => {
  test.use({ expectedConsoleErrors: ["Error: Temporary entity load failure"] });
  test("a failed detail load offers a working retry", async ({ page }) => {
    await page.goto("/transitions");
    await page.getByRole("link", { name: "error entity" }).click();
    await expect(page.getByRole("alert")).toContainText(
      "Temporary entity load failure",
    );
    await page.getByRole("button", { name: "Retry", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Entity error" }),
    ).toBeVisible();
    await expect(page.getByRole("alert")).toHaveCount(0);
  });
});
