// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DatePicker } from "@/components/ui/date-picker";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";

let root: Root;
let container: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
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
  const element = container.querySelector<HTMLInputElement>(
    '[data-slot="date-picker-input"]',
  );
  if (!element) throw new Error("Missing date picker input");
  return element;
}

function typeInto(element: HTMLInputElement, text: string) {
  Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )?.set?.call(element, text);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

it("shows the date in the locale's order", async () => {
  await render(<DatePicker value="2026-11-02" onChange={vi.fn()} />);
  expect(input().value).toBe("02/11/2026");
});

it("shows the placeholder when no date is chosen", async () => {
  await render(<DatePicker value="" onChange={vi.fn()} />);
  expect(input().value).toBe("");
  expect(input().placeholder).toBe("Choose a date");
});

it("reports a day picked in the calendar as an ISO date", async () => {
  const onChange = vi.fn();
  await render(<DatePicker value="2026-11-02" onChange={onChange} />);
  await act(async () => {
    container
      .querySelector<HTMLElement>('[data-slot="date-picker-trigger"]')
      ?.click();
  });
  const day = Array.from(
    document.querySelectorAll<HTMLElement>('[data-slot="calendar-day"]'),
  ).find((element) => element.textContent === "3");
  await act(async () => day?.click());
  expect(onChange).toHaveBeenLastCalledWith("2026-11-03");
});

it("reports an empty string when the typed date is cleared", async () => {
  const onChange = vi.fn();
  await render(<DatePicker value="2026-11-02" onChange={onChange} />);
  await act(async () => {
    input().focus();
    typeInto(input(), "");
    input().blur();
  });
  expect(onChange).toHaveBeenLastCalledWith("");
});

it("disables the input and trigger together", async () => {
  await render(<DatePicker value="" disabled onChange={vi.fn()} />);
  expect(input().disabled).toBe(true);
  expect(
    container.querySelector<HTMLButtonElement>(
      '[data-slot="date-picker-trigger"]',
    )?.disabled,
  ).toBe(true);
});
