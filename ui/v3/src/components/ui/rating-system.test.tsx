// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { RatingSystem } from "@/components/ui/rating-system";
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

function star(label: string): HTMLElement {
  const element = container.querySelector<HTMLElement>(
    `[data-slot="rating-radio"][aria-label="${label}"]`,
  );
  if (!element) throw new Error(`Missing ${label} radio`);
  return element;
}

function clearButton(): HTMLElement | null {
  return container.querySelector<HTMLElement>('[aria-label="Clear rating"]');
}

it("shows a stored rating as the number of filled stars", async () => {
  await render(<RatingSystem value={60} onSetRating={vi.fn()} />);
  const filled = container.querySelectorAll(
    '[data-slot="rating-item"][data-filled]',
  );
  expect(filled.length).toBe(3);
});

it("reports a chosen star on the stored 100-point scale", async () => {
  const onSetRating = vi.fn();
  await render(<RatingSystem value={60} onSetRating={onSetRating} />);
  await act(async () => star("4 stars").click());
  expect(onSetRating).toHaveBeenLastCalledWith(80);
});

it("clears the rating when the selected star is chosen again", async () => {
  const onSetRating = vi.fn();
  await render(<RatingSystem value={60} onSetRating={onSetRating} />);
  await act(async () => star("3 stars").click());
  expect(onSetRating).toHaveBeenLastCalledWith(null);
});

it("clears the rating from the clear control", async () => {
  const onSetRating = vi.fn();
  await render(<RatingSystem value={60} onSetRating={onSetRating} />);
  await act(async () => clearButton()?.click());
  expect(onSetRating).toHaveBeenLastCalledWith(null);
});

it("offers no clear control when there is no rating to clear", async () => {
  await render(<RatingSystem value={null} onSetRating={vi.fn()} />);
  expect(clearButton()).toBeNull();
});

it("offers no clear control when a rating is required", async () => {
  await render(<RatingSystem value={60} onSetRating={vi.fn()} valueRequired />);
  expect(clearButton()).toBeNull();
});

it("offers no clear control without a change handler", async () => {
  await render(<RatingSystem value={60} />);
  expect(clearButton()).toBeNull();
});
