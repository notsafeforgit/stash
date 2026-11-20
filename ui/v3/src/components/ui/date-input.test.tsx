// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DateInput } from "@/components/ui/date-input";
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

function segment(type: string): HTMLInputElement {
  const element = container.querySelector<HTMLInputElement>(
    `[data-slot="date-input-segment"][data-type="${type}"]`,
  );
  if (!element) throw new Error(`Missing ${type} segment`);
  return element;
}

function stepUp(type: string) {
  return act(async () => {
    segment(type).dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "ArrowUp",
        bubbles: true,
        cancelable: true,
      }),
    );
  });
}

function dateField(): HTMLInputElement {
  const element = container.querySelector<HTMLInputElement>(
    '[data-slot="date-picker-input"]',
  );
  if (!element) throw new Error("Missing date field");
  return element;
}

function typeInto(element: HTMLInputElement, text: string) {
  Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )?.set?.call(element, text);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

/** The calendar opens in a portal, so its days are searched in the document. */
function calendarDay(text: string): HTMLElement {
  const day = Array.from(
    document.querySelectorAll<HTMLElement>('[data-slot="calendar-day"]'),
  ).find((element) => element.textContent === text);
  if (!day) throw new Error(`Missing calendar day ${text}`);
  return day;
}

function openDateCalendar() {
  return act(async () => {
    container
      .querySelector<HTMLElement>('[data-slot="date-picker-trigger"]')
      ?.click();
  });
}

function openTimeCalendar() {
  return act(async () => {
    container
      .querySelector<HTMLElement>('button[aria-label="Choose a date"]')
      ?.click();
  });
}

it("shows a date and time in the locale's order", async () => {
  await render(
    <DateInput isTime value="2026-11-02 14:30" onValueChange={vi.fn()} />,
  );
  expect(segment("day").value).toBe("02");
  expect(segment("month").value).toBe("11");
  expect(segment("year").value).toBe("2026");
  expect(segment("hour").value).toBe("14");
  expect(segment("minute").value).toBe("30");
});

it("reads and writes date and time with a space, as the filters expect", async () => {
  const onValueChange = vi.fn();
  await render(
    <DateInput isTime value="2026-11-02 14:30" onValueChange={onValueChange} />,
  );
  await stepUp("minute");
  expect(onValueChange).toHaveBeenLastCalledWith("2026-11-02 14:31");
});

it("steps the day segment of a date and time", async () => {
  const onValueChange = vi.fn();
  await render(
    <DateInput isTime value="2026-11-02 14:30" onValueChange={onValueChange} />,
  );
  await stepUp("day");
  expect(onValueChange).toHaveBeenLastCalledWith("2026-11-03 14:30");
});

it("shows a date in the date picker's locale format", async () => {
  await render(<DateInput value="2026-11-02" onValueChange={vi.fn()} />);
  expect(dateField().value).toBe("02/11/2026");
});

it("types a date in the locale's order", async () => {
  const onValueChange = vi.fn();
  await render(<DateInput value="" onValueChange={onValueChange} />);
  await act(async () => {
    dateField().focus();
    typeInto(dateField(), "03/11/2026");
    dateField().blur();
  });
  expect(onValueChange).toHaveBeenLastCalledWith("2026-11-03");
});

it("picks a day from the calendar in date mode", async () => {
  const onValueChange = vi.fn();
  await render(<DateInput value="2026-11-02" onValueChange={onValueChange} />);
  await openDateCalendar();
  await act(async () => calendarDay("3").click());
  expect(onValueChange).toHaveBeenLastCalledWith("2026-11-03");
});

it("keeps the chosen time when a day is picked from the calendar", async () => {
  const onValueChange = vi.fn();
  await render(
    <DateInput isTime value="2026-11-02 14:30" onValueChange={onValueChange} />,
  );
  await openTimeCalendar();
  await act(async () => calendarDay("5").click());
  expect(onValueChange).toHaveBeenLastCalledWith("2026-11-05 14:30");
});

it("starts at midnight when a day is picked before any time is set", async () => {
  const onValueChange = vi.fn();
  await render(<DateInput isTime value="" onValueChange={onValueChange} />);
  await openTimeCalendar();
  await act(async () => calendarDay("5").click());
  expect(onValueChange).toHaveBeenLastCalledWith("2026-11-05 00:00");
});

it("does not pick a day its disabledDays rule rejects in date mode", async () => {
  const onValueChange = vi.fn();
  await render(
    <DateInput
      value="2026-11-02"
      onValueChange={onValueChange}
      disabledDays={(date) => date.getDate() === 3}
    />,
  );
  await openDateCalendar();
  expect(calendarDay("3").hasAttribute("data-unavailable")).toBe(true);
  await act(async () => calendarDay("3").click());
  expect(onValueChange).not.toHaveBeenCalled();
});

it("does not pick a day its disabledDays rule rejects in time mode", async () => {
  const onValueChange = vi.fn();
  await render(
    <DateInput
      isTime
      value="2026-11-02 14:30"
      onValueChange={onValueChange}
      disabledDays={(date) => date.getDate() === 3}
    />,
  );
  await openTimeCalendar();
  expect(calendarDay("3").hasAttribute("data-unavailable")).toBe(true);
  await act(async () => calendarDay("3").click());
  expect(onValueChange).not.toHaveBeenCalled();
});

function Controlled({
  initial,
  ...props
}: Omit<React.ComponentProps<typeof DateInput>, "value" | "onValueChange"> & {
  initial: string;
}) {
  const [value, setValue] = useState(initial);
  return <DateInput {...props} value={value} onValueChange={setValue} />;
}

it("marks a time whose day its disabledDays rule rejects, using local dates", async () => {
  await render(
    <Controlled
      isTime
      initial="2026-11-02 14:30"
      disabledDays={(date) => date.getDate() === 3}
    />,
  );
  expect(
    container
      .querySelector('[data-slot="date-input"]')
      ?.hasAttribute("data-invalid"),
  ).toBe(false);
  await stepUp("day");
  expect(
    container
      .querySelector('[data-slot="date-input"]')
      ?.hasAttribute("data-invalid"),
  ).toBe(true);
});

it("names the date field and shows an error message", async () => {
  await render(
    <DateInput
      value="2026-11-02"
      onValueChange={vi.fn()}
      placeholder="Due date"
      error="Choose a date."
    />,
  );
  expect(dateField().getAttribute("aria-label")).toBe("Due date");
  expect(container.textContent).toContain("Choose a date.");
});

it("names the time group and its calendar button", async () => {
  await render(
    <DateInput
      isTime
      value="2026-11-02 14:30"
      onValueChange={vi.fn()}
      placeholder="Due date"
    />,
  );
  expect(
    container
      .querySelector('[data-slot="date-input"]')
      ?.getAttribute("aria-label"),
  ).toBe("Due date");
  expect(
    container.querySelector('button[aria-label="Choose a date"]'),
  ).not.toBeNull();
});
