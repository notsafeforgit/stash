// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Checkbox } from "@/components/ui/checkbox";
import { IndeterminateCheckbox } from "@/components/ui/indeterminate-checkbox";

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

function checkbox(): HTMLButtonElement {
  const element = container.querySelector<HTMLButtonElement>(
    '[data-slot="checkbox"]',
  );
  if (!element) throw new Error("Missing checkbox");
  return element;
}

it("reports presses as booleans", async () => {
  const onCheckedChange = vi.fn();
  await act(async () =>
    root.render(
      <Checkbox
        aria-label="Row"
        defaultChecked={false}
        onCheckedChange={onCheckedChange}
      />,
    ),
  );
  await act(async () => checkbox().click());
  expect(onCheckedChange.mock.calls[0]?.[0]).toBe(true);
});

it("shows the mixed state from the indeterminate prop", async () => {
  await act(async () =>
    root.render(<Checkbox aria-label="All" indeterminate />),
  );
  expect(checkbox().hasAttribute("data-indeterminate")).toBe(true);
  expect(checkbox().hasAttribute("data-checked")).toBe(false);
});

it("cycles checked, unchecked and indeterminate through its setter", async () => {
  const setChecked = vi.fn();
  await act(async () =>
    root.render(
      <IndeterminateCheckbox
        aria-label="Duplicates"
        checked={false}
        setChecked={setChecked}
      />,
    ),
  );
  await act(async () => checkbox().click());
  expect(setChecked).toHaveBeenLastCalledWith(true);

  await act(async () =>
    root.render(
      <IndeterminateCheckbox
        aria-label="Duplicates"
        checked
        setChecked={setChecked}
      />,
    ),
  );
  await act(async () => checkbox().click());
  expect(setChecked).toHaveBeenLastCalledWith(undefined);
});
