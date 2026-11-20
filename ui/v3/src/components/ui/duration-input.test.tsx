// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DurationInput } from "@/components/ui/duration-input";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";

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

function render(element: React.ReactElement) {
  return act(async () =>
    root.render(
      <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
        {element}
      </IntlProvider>,
    ),
  );
}

function input(): HTMLInputElement {
  const element =
    container.querySelector<HTMLInputElement>('input[type="text"]');
  if (!element) throw new Error("Missing duration input");
  return element;
}

function button(label: string): HTMLButtonElement {
  const element = container.querySelector<HTMLButtonElement>(
    `button[aria-label="${label}"]`,
  );
  if (!element) throw new Error(`Missing ${label} button`);
  return element;
}

function typeInto(element: HTMLInputElement, text: string) {
  Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )?.set?.call(element, text);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

it("shows the seconds as a timestamp with milliseconds", async () => {
  await render(<DurationInput value={90.5} setValue={vi.fn()} />);
  expect(input().value).toBe("1:30.500");
});

it("steps up by one second", async () => {
  const setValue = vi.fn();
  await render(<DurationInput value={90.5} setValue={setValue} />);
  await act(async () => button("Increase").click());
  expect(setValue).toHaveBeenLastCalledWith(91.5);
});

it("does not step below zero when negative values are not allowed", async () => {
  const setValue = vi.fn();
  await render(<DurationInput value={0} setValue={setValue} />);
  await act(async () => button("Decrease").click());
  expect(setValue).toHaveBeenLastCalledWith(0);
});

it("commits a typed timestamp when the field loses focus", async () => {
  const setValue = vi.fn();
  await render(<DurationInput value={null} setValue={setValue} />);
  await act(async () => {
    input().focus();
    typeInto(input(), "00:02:00");
    input().blur();
  });
  expect(setValue).toHaveBeenLastCalledWith(120);
});

it("sets the current time from the reset control", async () => {
  const onReset = vi.fn();
  await render(
    <DurationInput value={5} setValue={vi.fn()} onReset={onReset} />,
  );
  await act(async () => button("Use current time").click());
  expect(onReset).toHaveBeenCalledOnce();
});

it("hides the steppers while disabled", async () => {
  await render(<DurationInput value={5} setValue={vi.fn()} disabled />);
  expect(container.querySelector('[aria-label="Increase"]')).toBeNull();
  expect(input().disabled).toBe(true);
});

it("marks the input invalid and shows its error", async () => {
  await render(
    <DurationInput value={5} setValue={vi.fn()} error="Enter a duration." />,
  );
  expect(input().getAttribute("aria-invalid")).toBe("true");
  expect(container.textContent).toContain("Enter a duration.");
});
