// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Slider } from "@/components/ui/slider";

let root: Root;
let container: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

function thumbInputs(): HTMLInputElement[] {
  return [
    ...container.querySelectorAll<HTMLInputElement>('input[type="range"]'),
  ];
}

it("renders one thumb per value and names a single thumb itself", async () => {
  await act(async () => root.render(<Slider value={40} aria-label="Volume" />));
  expect(thumbInputs()).toHaveLength(1);
  expect(thumbInputs()[0]?.getAttribute("aria-label")).toBe("Volume");
});

it("renders a thumb for each value of a range and keeps the group name", async () => {
  await act(async () =>
    root.render(<Slider value={[10, 80]} aria-label="Price" />),
  );
  expect(thumbInputs()).toHaveLength(2);
  expect(thumbInputs()[0]?.getAttribute("aria-label")).toBeNull();
  expect(
    container.querySelector('[role="group"]')?.getAttribute("aria-label"),
  ).toBe("Price");
});

it("reports a value change with the thumb's new value", async () => {
  const onValueChange = vi.fn();
  await act(async () =>
    root.render(
      <Slider
        value={[20]}
        min={0}
        max={100}
        step={1}
        aria-label="Level"
        onValueChange={onValueChange}
      />,
    ),
  );
  const input = thumbInputs()[0];
  if (!input) throw new Error("Missing thumb");
  await act(async () => {
    input.focus();
    input.dispatchEvent(
      new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }),
    );
  });
  expect(onValueChange).toHaveBeenCalled();
});
