import type { BrowserContext, Page } from "@playwright/test";
import { test, expect } from "./test";

type Point = { x: number; y: number };

async function dragTouch(
  page: Page,
  context: BrowserContext,
  browserName: string,
  from: Point,
  to: Point,
  whileDragging: () => Promise<void>,
) {
  if (browserName === "chromium") {
    // Native touch input lets the browser arbitrate drag versus page scroll.
    // Synthetic pointer events alone would miss the CSS cascade regression.
    const input = await context.newCDPSession(page);
    try {
      await input.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [from],
      });
      for (let step = 1; step <= 12; step++) {
        await input.send("Input.dispatchTouchEvent", {
          type: "touchMove",
          touchPoints: [{ x: to.x, y: from.y + ((to.y - from.y) * step) / 12 }],
        });
      }
      await whileDragging();
    } finally {
      await input.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
      await input.detach();
    }
    return;
  }

  // Playwright has no native WebKit swipe API. Check its computed touch policy
  // separately and exercise the real sensor with touch pointer events.
  const handle = await page.evaluateHandle(({ x, y }) => {
    const element = document.elementFromPoint(x, y);
    if (!element) throw new Error("Missing drag handle");
    return element;
  }, from);
  const dispatch = async (type: string, point: Point) =>
    handle.evaluate(
      async (element, { type, point }) => {
        element.dispatchEvent(
          new PointerEvent(type, {
            bubbles: true,
            cancelable: true,
            pointerId: 41,
            pointerType: "touch",
            isPrimary: true,
            button: 0,
            buttons: type === "pointerup" ? 0 : 1,
            clientX: point.x,
            clientY: point.y,
          }),
        );
        await new Promise<void>((resolve) =>
          requestAnimationFrame(() => resolve()),
        );
      },
      { type, point },
    );
  try {
    await dispatch("pointerdown", from);
    for (let step = 1; step <= 12; step++)
      await dispatch("pointermove", {
        x: to.x,
        y: from.y + ((to.y - from.y) * step) / 12,
      });
    await whileDragging();
  } finally {
    await dispatch("pointerup", to);
    await handle.dispose();
  }
}

for (const width of [320, 390, 1280]) {
  test.describe(`TV rail dragging at ${width}px`, () => {
    const mobile = width < 600;
    test.use({
      viewport: { width, height: 844 },
      isMobile: mobile,
      hasTouch: mobile,
    });

    test("the handle reorders and saves without scrolling, and arrows remain usable", async ({
      page,
      context,
      browserName,
    }) => {
      await page.goto("/tv-fixture/settings/tv?paused");
      const rows = page.locator("[data-tv-rail-entry]");
      const info = page.getByRole("button", {
        name: "Drag Information",
        exact: true,
      });
      const counter = page.getByRole("button", {
        name: "Drag O-counter",
        exact: true,
      });
      const scroller = page.locator(".overflow-y-auto").filter({ has: info });
      await info.evaluate((element) =>
        element.scrollIntoView({ block: "center" }),
      );
      const beforeScroll = await scroller.evaluate(
        (element) => element.scrollTop,
      );
      const fromBox = await info.boundingBox();
      const toBox = await counter.boundingBox();
      if (!fromBox || !toBox) throw new Error("Missing drag handle bounds");
      const from = {
        x: fromBox.x + fromBox.width / 2,
        y: fromBox.y + fromBox.height / 2,
      };
      const to = {
        x: toBox.x + toBox.width / 2,
        y: toBox.y + toBox.height / 2,
      };
      const whileDragging = async () => {
        await expect(info).toHaveAttribute("aria-pressed", "true");
        expect(await scroller.evaluate((element) => element.scrollTop)).toBe(
          beforeScroll,
        );
      };
      if (mobile)
        await dragTouch(page, context, browserName, from, to, whileDragging);
      else {
        await page.mouse.move(from.x, from.y);
        await page.mouse.down();
        try {
          await page.mouse.move(to.x, to.y, { steps: 12 });
          await whileDragging();
        } finally {
          await page.mouse.up();
        }
      }
      await expect(info).toHaveCSS("touch-action", "none");
      await expect(
        page.getByRole("button", { name: "Information", exact: true }),
      ).toHaveCSS("touch-action", "manipulation");
      await expect(rows.nth(1)).toHaveAttribute(
        "data-tv-rail-entry",
        "counter",
      );
      await expect(rows.nth(2)).toHaveAttribute("data-tv-rail-entry", "info");
      await expect(info).not.toHaveAttribute("aria-pressed", "true");
      await expect
        .poll(() =>
          page.evaluate(() =>
            window.tvFixtureRequests
              .filter((request) => request.name === "ConfigureUISetting")
              .at(-1),
          ),
        )
        .toMatchObject({
          variables: {
            key: "tv",
            value: {
              rail: [
                { action: { id: "visibility" } },
                { action: { id: "counter" } },
                { action: { id: "info" } },
                { id: "edit" },
                { id: "playback" },
              ],
            },
          },
        });

      const moveUp = page.getByRole("button", {
        name: "Move Information up",
        exact: true,
      });
      // dnd-kit suppresses post-drag ghost clicks for 50ms. Start a separate
      // tap after that window, as a person moving to the arrow would.
      await page.waitForTimeout(60);
      if (mobile && browserName === "chromium") {
        await moveUp.tap({ trial: true });
        const box = await moveUp.boundingBox();
        if (!box) throw new Error("Missing arrow bounds");
        const input = await context.newCDPSession(page);
        try {
          // Chromium can omit the click for a zero-duration tap after a CDP
          // drag. Keep native touch input, with time to process the new press.
          await input.send("Input.dispatchTouchEvent", {
            type: "touchStart",
            touchPoints: [
              { x: box.x + box.width / 2, y: box.y + box.height / 2 },
            ],
          });
          await page.waitForTimeout(100);
          await input.send("Input.dispatchTouchEvent", {
            type: "touchEnd",
            touchPoints: [],
          });
        } finally {
          await input.detach();
        }
      } else if (mobile) await moveUp.tap();
      else await moveUp.click();
      await expect(rows.nth(1)).toHaveAttribute("data-tv-rail-entry", "info");

      if (mobile && browserName === "chromium") {
        // Starting on the row label must still scroll normally after a drag.
        const label = page.getByRole("button", {
          name: "Information",
          exact: true,
        });
        await label.evaluate((element) =>
          element.scrollIntoView({ block: "center" }),
        );
        const box = await label.boundingBox();
        if (!box) throw new Error("Missing row label bounds");
        const top = await scroller.evaluate((element) => element.scrollTop);
        await dragTouch(
          page,
          context,
          browserName,
          { x: box.x + box.width / 2, y: box.y + box.height / 2 },
          { x: box.x + box.width / 2, y: box.y + box.height / 2 - 120 },
          async () => {
            await expect(info).not.toHaveAttribute("aria-pressed", "true");
          },
        );
        await expect
          .poll(() => scroller.evaluate((element) => element.scrollTop))
          .toBeGreaterThan(top + 30);
        await expect(rows.nth(1)).toHaveAttribute("data-tv-rail-entry", "info");
      }
    });
  });
}
