import { test, expect } from "./test";

interface TransitionObservation {
  direction?: "forward" | "back" | "replace";
  contentAnimation?: string;
  contentDuration?: string;
  rootName?: string;
  contentName?: string;
  finished: boolean;
}

declare global {
  interface Window {
    observedTransitions: TransitionObservation[];
  }
}

test.use({ viewport: { width: 1280, height: 800 }, isMobile: false });

test.beforeEach(async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.addInitScript(() => {
    window.observedTransitions = [];
    if (typeof document.startViewTransition !== "function") return;
    const start = document.startViewTransition.bind(document);
    document.startViewTransition = (...args) => {
      const observation: TransitionObservation = { finished: false };
      window.observedTransitions.push(observation);
      const transition = start(...args);
      void transition.ready.then(
        () => {
          const root = document.documentElement;
          for (const direction of ["forward", "back", "replace"] as const) {
            const selector = `:active-view-transition-type(route-${direction})`;
            if (
              CSS.supports(`selector(${selector})`) &&
              root.matches(selector)
            ) {
              observation.direction = direction;
            }
          }
          const content = document.querySelector("[data-route-viewport]");
          if (!content) throw new Error("Missing route viewport");
          const style = getComputedStyle(
            root,
            "::view-transition-new(route-content)",
          );
          observation.contentAnimation = style.animationName;
          observation.contentDuration = style.animationDuration;
          observation.rootName = getComputedStyle(root).viewTransitionName;
          observation.contentName =
            getComputedStyle(content).viewTransitionName;
        },
        () => {},
      );
      void transition.finished.then(() => {
        observation.finished = true;
      });
      return transition;
    };
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
        contentAnimation: "route-content-in",
        contentDuration: "0.18s",
        rootName: "none",
        contentName: "route-content",
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

test("older View Transition implementations still honor navigation opt-outs", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const supports = CSS.supports.bind(CSS);
    CSS.supports = (property: string, value?: string) => {
      if (property.includes("active-view-transition-type")) return false;
      return value === undefined
        ? supports(property)
        : supports(property, value);
    };
  });
  await page.goto("/transitions");
  await page.getByRole("link", { name: "Entity 1", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => window.observedTransitions[0]?.finished))
    .toBe(true);
  expect(
    await page.evaluate(() => window.observedTransitions[0]),
  ).toMatchObject({
    contentDuration: "0.14s",
    rootName: "none",
    contentName: "route-content",
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
  // Dispatch during the native animation, without Playwright waiting for stability.
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
    // Playwright's next click scrolls captured elements to the center while
    // their real DOM is hidden by a native snapshot. Finish this animation
    // before asking it to click the restored list again.
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
    Object.defineProperty(document, "startViewTransition", {
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
