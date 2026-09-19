// @vitest-environment jsdom
import { act, type ComponentProps } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { PositionScrubber } from "./position-scrubber";

let container: HTMLDivElement;
let root: Root;
const seek = vi.fn();
const preview = vi.fn();
const vibrate = vi.fn();

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("navigator", { vibrate });
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function render(
  props: Partial<ComponentProps<typeof PositionScrubber>> = {},
) {
  await act(async () => {
    root.render(
      <IntlProvider locale="en-GB">
        <PositionScrubber
          value={20}
          duration={120}
          onSeek={seek}
          onSeekPreview={preview}
          {...props}
        />
      </IntlProvider>,
    );
  });
  const slider = container.querySelector<HTMLDivElement>('[role="slider"]');
  if (!slider) throw new Error("Missing scrubber");
  slider.setPointerCapture = vi.fn();
  slider.hasPointerCapture = () => true;
  slider.releasePointerCapture = vi.fn();
  vi.spyOn(slider, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, 0, 400, 400),
  );
  return slider;
}

async function pointer(
  slider: HTMLElement,
  type: string,
  x = 200,
  y = 200,
  pointerType = "touch",
  pointerId = 1,
) {
  const event = new MouseEvent(type, {
    bubbles: true,
    cancelable: true,
    clientX: x,
    clientY: y,
    button: 0,
  });
  Object.defineProperties(event, {
    pointerId: { value: pointerId },
    pointerType: { value: pointerType },
  });
  await act(async () => {
    slider.dispatchEvent(event);
  });
}

async function wait(ms: number) {
  await act(async () => {
    vi.advanceTimersByTime(ms);
  });
}

const time = (slider: HTMLElement) =>
  Number(slider.getAttribute("aria-valuenow"));

for (const direction of ["right", "down", "up"] as const) {
  it(`anchors successive touch zooms and commits the precise time when seeking ${direction}`, async () => {
    const slider = await render({ direction });
    await pointer(slider, "pointerdown");
    expect(time(slider)).toBe(60);
    expect(preview).not.toHaveBeenCalled();
    await wait(650);
    expect(time(slider)).toBe(60);
    expect(slider.hasAttribute("data-precision")).toBe(true);
    expect(preview).toHaveBeenLastCalledWith(60);
    expect(seek).not.toHaveBeenCalled();
    await wait(650);
    // 120 seconds -> 30 -> 7.5. A quarter-width move now seeks 1.875s.
    const x = direction === "right" ? 300 : 200;
    const y = direction === "down" ? 300 : direction === "up" ? 100 : 200;
    await pointer(slider, "pointermove", x, y);
    expect(time(slider)).toBe(61.875);
    await pointer(slider, "pointerup", x, y);
    expect(seek).toHaveBeenCalledExactlyOnceWith(61.875);
    expect(slider.hasAttribute("data-precision")).toBe(false);
    expect(slider.hasAttribute("data-dragging")).toBe(false);
    await wait(2000);
    expect(vibrate).toHaveBeenCalledTimes(2);
    await pointer(slider, "pointerdown", 100, 100);
    expect(time(slider)).toBe(direction === "up" ? 90 : 30);
  });
}

it("tolerates jitter but restarts the dwell after cumulative deliberate movement", async () => {
  const slider = await render();
  await pointer(slider, "pointerdown");
  await wait(400);
  await pointer(slider, "pointermove", 203);
  await wait(250);
  expect(slider.hasAttribute("data-precision")).toBe(true);
  expect(time(slider)).toBeCloseTo(60.9);
  expect(vibrate).toHaveBeenCalledTimes(1);
  await pointer(slider, "pointermove", 206);
  await wait(400);
  await pointer(slider, "pointermove", 209);
  await wait(250);
  expect(vibrate).toHaveBeenCalledTimes(1);
  await wait(400);
  expect(vibrate).toHaveBeenCalledTimes(2);
});

it("caps a long scene at a minute initially and a second at maximum precision", async () => {
  const slider = await render({ duration: 7200 });
  await pointer(slider, "pointerdown");
  await wait(650);
  await pointer(slider, "pointermove", 300);
  expect(time(slider)).toBe(3615);
  await wait(650 * 6);
  expect(vibrate).toHaveBeenCalledTimes(4);
  await pointer(slider, "pointermove", 200);
  expect(time(slider)).toBe(3614.75);
});

it("zooms short marker clips without requiring vibration support", async () => {
  vi.stubGlobal("navigator", {});
  const slider = await render({ duration: 2, value: 0 });
  await pointer(slider, "pointerdown", 100);
  await wait(650);
  expect(time(slider)).toBe(0.5);
  await pointer(slider, "pointermove", 300);
  expect(time(slider)).toBe(1);
  await pointer(slider, "pointerup", 300);
  expect(seek).toHaveBeenCalledExactlyOnceWith(1);
});

for (const event of ["pointercancel", "lostpointercapture", "blur"]) {
  it(`cancels precision seeking and its pending dwell on ${event}`, async () => {
    const slider = await render();
    await pointer(slider, "pointerdown");
    await wait(650);
    if (event === "blur")
      await act(async () => {
        slider.blur();
      });
    else await pointer(slider, event);
    expect(preview).toHaveBeenLastCalledWith(null);
    expect(slider.hasAttribute("data-precision")).toBe(false);
    await pointer(slider, "pointerup");
    await wait(2000);
    expect(vibrate).toHaveBeenCalledTimes(1);
    expect(seek).not.toHaveBeenCalled();
  });
}

it("cleans up an active precision preview when the scrubber is replaced", async () => {
  const onScrubChange = vi.fn();
  const onPreviewChange = vi.fn();
  const slider = await render({ onScrubChange, onPreviewChange });
  await pointer(slider, "pointerdown");
  await wait(650);
  await act(async () => {
    root.render(null);
  });
  await wait(2000);
  expect(preview).toHaveBeenLastCalledWith(null);
  expect(onScrubChange).toHaveBeenLastCalledWith(null);
  expect(onPreviewChange).toHaveBeenLastCalledWith(false);
  expect(vibrate).toHaveBeenCalledTimes(1);
});

it("leaves mouse dragging, quick touch taps, and keyboard seeks at the full scale", async () => {
  const slider = await render();
  await pointer(slider, "pointerdown", 200, 200, "mouse");
  await wait(2000);
  await pointer(slider, "pointerup", 300, 200, "mouse");
  expect(seek).toHaveBeenLastCalledWith(90);
  await pointer(slider, "pointerdown", 100);
  await wait(300);
  await pointer(slider, "pointerup", 100);
  await wait(1000);
  expect(seek).toHaveBeenLastCalledWith(30);
  expect(vibrate).not.toHaveBeenCalled();
  expect(preview).not.toHaveBeenCalled();
  await act(async () => {
    slider.dispatchEvent(
      new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }),
    );
  });
  expect(seek).toHaveBeenLastCalledWith(25);
});

it("clips buffered intervals to the magnified range in display coordinates", async () => {
  const slider = await render({
    bufferedOffset: -100,
    bufferedRanges: [
      { start: 130, end: 150 },
      { start: 160, end: 180 },
    ],
  });
  await pointer(slider, "pointerdown");
  await wait(650);
  // The visible range is 45..75; scene buffers become 30..50 and 60..80.
  const buffers = Array.from(
    slider.querySelectorAll<HTMLDivElement>("[data-position-scrubber-buffer]"),
  );
  expect(buffers).toHaveLength(2);
  expect(parseFloat(buffers[0]?.style.left ?? "")).toBe(0);
  expect(parseFloat(buffers[0]?.style.width ?? "")).toBeCloseTo(100 / 6);
  expect(parseFloat(buffers[1]?.style.left ?? "")).toBe(50);
  expect(parseFloat(buffers[1]?.style.width ?? "")).toBe(50);
});
