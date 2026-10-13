import { test, expect } from "./test";

for (const [layout, path, list] of [
  ["standalone", "/?standalone", "standalone"],
  ["collection", "/", "scenes"],
  ["media detail", "/?media", "scenes"],
] as const) {
  test(`${layout} search consumes the first outside tap and keeps the query`, async ({
    page,
  }) => {
    await page.goto(path);
    await page.getByRole("button", { name: "Search…", exact: true }).tap();
    const search = page.getByRole("searchbox");
    await search.fill("keep my query");
    const item = page.getByRole("button", { name: `${list} 1`, exact: true });
    await item.tap();
    await expect(search).not.toBeFocused();
    await expect(search).toHaveValue("keep my query");
    await expect(
      page.getByTestId(`${list}-list`).getByTestId("list-state"),
    ).toHaveAttribute("data-term", "keep my query");
    await expect(
      page.getByRole("button", { name: "Select None", exact: true }),
    ).toBeHidden();
    await item.tap();
    await expect(
      page.getByRole("button", { name: "Select None", exact: true }),
    ).toBeVisible();
  });

  test(`${layout} page picker stays open during navigation and consumes dismissal`, async ({
    page,
  }) => {
    await page.goto(path);
    const trigger = page.getByRole("button", {
      name: layout === "standalone" ? "Pages" : "Detail sections",
      exact: true,
    });
    await trigger.tap();
    const popup = page.locator('[data-slot="popover-content"]:visible');
    const state = page.getByTestId(`${list}-list`).getByTestId("list-state");
    await popup.getByRole("button", { name: "Next", exact: true }).tap();
    await expect(state).toContainText("page 2:");
    await expect(popup).toBeVisible();
    await popup.getByRole("button", { name: "Next", exact: true }).tap();
    await expect(state).toContainText("page 3:");
    await popup.getByRole("button", { name: "Prev", exact: true }).tap();
    await expect(state).toContainText("page 2:");
    const jump = popup.getByRole("spinbutton", { name: "Go to page" });
    await jump.fill("4");
    await popup.getByRole("button", { name: "Go", exact: true }).tap();
    await expect(state).toContainText("page 4:");
    await expect(jump).toHaveValue("4");
    await expect(trigger).toHaveAttribute("aria-expanded", "true");

    // An actual list action verifies that the first tap cannot click through.
    const action = page.getByRole("button", {
      name: "Reduce results",
      exact: true,
    });
    await action.tap();
    await expect(trigger).toHaveAttribute("aria-expanded", "false");
    await trigger.tap();
    await expect(jump).toHaveAttribute("max", "5");
    await action.tap();
    await action.tap();
    await trigger.tap();
    await expect(jump).toHaveAttribute("max", "2");
  });
}

test("page picker leaves outside touch scrolling available even on narrow phones", async ({
  page,
  browserName,
}) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await page.goto("/?standalone");
  const trigger = page.getByRole("button", { name: "Pages", exact: true });
  await trigger.tap();
  const scroller = page.locator("[data-scroll-restoration-id]").last();
  const popup = page.locator('[data-slot="popover-content"]:visible');
  const bounds = await popup.boundingBox();
  if (!bounds) throw new Error("Missing page picker");
  const startY = Math.min(250, bounds.y - 20);
  if (browserName === "chromium") {
    // Native touch input proves the actual scroll gesture is not blocked.
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchStart",
      touchPoints: [{ x: 160, y: startY }],
    });
    for (let step = 1; step <= 5; step++) {
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchMove",
        touchPoints: [{ x: 160, y: startY - step * 20 }],
      });
    }
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchEnd",
      touchPoints: [],
    });
    await expect
      .poll(() => scroller.evaluate((el) => el.scrollTop))
      .toBeGreaterThan(20);
    // Chromium consumes a tap to stop a native fling without generating click.
    // Wait for scrolling to settle before exercising the popup's Next button.
    let previous = -1;
    let stable = 0;
    await expect
      .poll(
        async () => {
          const position = await scroller.evaluate((el) => el.scrollTop);
          stable = position === previous ? stable + 1 : 0;
          previous = position;
          return stable;
        },
        { intervals: [100] },
      )
      .toBeGreaterThanOrEqual(3);
  } else {
    // Playwright WebKit has no native swipe API. Exercise touch/pointer cancel
    // and assert that native default handling remains enabled for scrolling.
    const defaultsAllowed = await scroller.evaluate((el) => {
      const common = {
        bubbles: true,
        cancelable: true,
        pointerId: 1,
        pointerType: "touch",
        isPrimary: true,
      };
      const allowed = el.dispatchEvent(
        new PointerEvent("pointerdown", {
          ...common,
          clientX: 160,
          clientY: 180,
        }),
      );
      const touch = new Event("touchstart", {
        bubbles: true,
        cancelable: true,
      });
      Object.defineProperty(touch, "touches", {
        value: [{ clientX: 160, clientY: 180 }],
      });
      const touchAllowed = el.dispatchEvent(touch);
      el.dispatchEvent(
        new PointerEvent("pointermove", {
          ...common,
          clientX: 160,
          clientY: 80,
        }),
      );
      const move = new Event("touchmove", { bubbles: true, cancelable: true });
      Object.defineProperty(move, "touches", {
        value: [{ clientX: 160, clientY: 80 }],
      });
      const moveAllowed = el.dispatchEvent(move);
      el.dispatchEvent(new PointerEvent("pointercancel", common));
      el.scrollBy(0, 100);
      const end = new Event("touchend", { bubbles: true, cancelable: true });
      Object.defineProperty(end, "touches", { value: [] });
      el.dispatchEvent(end);
      return allowed && touchAllowed && moveAllowed;
    });
    expect(defaultsAllowed).toBe(true);
  }
  await expect(trigger).toHaveAttribute("aria-expanded", "true");
  await popup.getByRole("button", { name: "Next", exact: true }).tap();
  await expect(trigger).toHaveText("2 / 5");
  await expect(popup).toBeVisible();
});

test("mobile settings search dismisses its keyboard before activating an outside field", async ({
  page,
}) => {
  await page.goto("/settings/interface");
  await page
    .getByRole("button", { name: "Search settings", exact: true })
    .tap();
  const search = page.getByRole("searchbox");
  await search.fill("theme");
  const field = page.getByRole("textbox", { name: "Unsaved setting" });
  await field.tap();
  await expect(search).not.toBeFocused();
  await expect(field).not.toBeFocused();
  await expect(search).toHaveValue("theme");
  await field.tap();
  await expect(field).toBeFocused();
});

test("wide touch layouts consume inline search dismissal and keep page jumps open", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.goto("/?standalone");
  const search = page.getByRole("searchbox");
  await search.tap();
  await search.fill("tablet search");
  const action = page.getByRole("button", {
    name: "Reduce results",
    exact: true,
  });
  await action.tap();
  await expect(search).not.toBeFocused();
  await expect(search).toHaveValue("tablet search");
  const navigation = page.getByRole("navigation", { name: "pagination" });
  const first = navigation.getByRole("button", { name: "1", exact: true });
  await first.tap();
  let popup = page.locator('[data-slot="popover-content"]:visible');
  await popup.getByRole("textbox").fill("3");
  await popup.getByRole("button", { name: "Go", exact: true }).tap();
  await expect(page.getByTestId("list-state")).toContainText("page 3:");
  popup = page.locator('[data-slot="popover-content"]:visible');
  await expect(popup).toBeVisible();
  await action.tap();
  await expect(popup).toBeHidden();
  await expect(
    navigation.getByRole("button", { name: "5", exact: true }),
  ).toBeVisible();
});
