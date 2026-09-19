import type { BrowserContext, Page } from "@playwright/test";

export type Point = { x: number; y: number };

export async function dragInput(
  page: Page,
  context: BrowserContext,
  nativeTouch: boolean,
  touch: boolean,
  start: Point,
) {
  if (nativeTouch) {
    const input = await context.newCDPSession(page);
    await input.send("Input.dispatchTouchEvent", {
      type: "touchStart",
      touchPoints: [start],
    });
    return {
      move: (point: Point) =>
        input.send("Input.dispatchTouchEvent", {
          type: "touchMove",
          touchPoints: [point],
        }),
      end: async () => {
        await input.send("Input.dispatchTouchEvent", {
          type: "touchEnd",
          touchPoints: [],
        });
        await input.detach();
      },
    };
  }

  // Playwright has no native WebKit touch-drag API. Retain trusted pointer
  // events for capture/slider behavior, identify them as touch, and deliver
  // the parallel touch stream to the original hit target as WebKit does.
  const target = await page.evaluateHandle(
    ({ x, y }) => document.elementFromPoint(x, y),
    start,
  );
  const touchPointer = await page.evaluateHandle((touch) => {
    const retype = (event: PointerEvent) => {
      if (touch)
        Object.defineProperty(event, "pointerType", { value: "touch" });
    };
    for (const type of ["pointerdown", "pointermove", "pointerup"] as const)
      document.addEventListener(type, retype, true);
    return retype;
  }, touch);
  const dispatchTouch = (phase: "start" | "move" | "end", point: Point) =>
    target.evaluate(
      (element, { phase, point }) => {
        if (!element) throw new Error("Missing scrubber hit target");
        const contact: Touch = {
          identifier: 1,
          target: element,
          clientX: point.x,
          clientY: point.y,
          pageX: point.x,
          pageY: point.y,
          screenX: point.x,
          screenY: point.y,
          force: 1,
          radiusX: 1,
          radiusY: 1,
          rotationAngle: 0,
        };
        const event = new Event(`touch${phase}`, {
          bubbles: true,
          cancelable: true,
        });
        Object.defineProperties(event, {
          touches: { value: phase === "end" ? [] : [contact] },
          targetTouches: { value: phase === "end" ? [] : [contact] },
          changedTouches: { value: [contact] },
        });
        element.dispatchEvent(event);
      },
      { phase, point },
    );
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  if (touch) await dispatchTouch("start", start);
  let last = start;
  return {
    move: async (point: Point) => {
      await page.mouse.move(point.x, point.y);
      if (touch) await dispatchTouch("move", point);
      last = point;
    },
    end: async () => {
      await page.mouse.up();
      if (touch) await dispatchTouch("end", last);
      await touchPointer.evaluate((retype) => {
        for (const type of ["pointerdown", "pointermove", "pointerup"] as const)
          document.removeEventListener(type, retype, true);
      });
      await touchPointer.dispose();
      await target.dispose();
    },
  };
}
