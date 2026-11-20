// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { FilterMode } from "@/core/generated-graphql";
import { SaveFilterDialog } from "./saved-filter-list";

const savedFilters = vi.hoisted(() => [
  { id: "favourite", name: "Favourite clips" },
  { id: "short", name: "Short clips" },
]);

vi.mock("@/core/saved-filters", () => ({
  useFindSavedFilters: () => ({
    data: { findSavedFilters: savedFilters },
    previousData: undefined,
  }),
}));

const messages = {
  filter_name: "Filter name",
  "actions.save_filter": "Save filter",
  "actions.cancel": "Cancel",
  "actions.save": "Save",
  "actions.saving": "Saving…",
  "search_filter.save_name_placeholder": "Name this filter",
  "search_filter.save_name_help": "Choose a name",
  "search_filter.save_name_exists": "Name exists",
  "search_filter.save_no_matches": "No matching filters",
  "dialogs.overwrite_filter_warning":
    'Saved filter "{entityName}" will be overwritten.',
};

let root: Root | undefined;
let container: HTMLDivElement | undefined;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  // jsdom has no Web Animations; the dialog waits for them when it unmounts.
});

afterEach(async () => {
  if (root) await act(async () => root?.unmount());
  container?.remove();
  root = undefined;
  container = undefined;
  vi.unstubAllGlobals();
});

async function render() {
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () =>
    root?.render(
      <IntlProvider locale="en-GB" messages={messages}>
        <SaveFilterDialog mode={FilterMode.Images} onClose={vi.fn()} />
      </IntlProvider>,
    ),
  );
}

function nameInput() {
  const input = document.querySelector<HTMLInputElement>(
    'input[placeholder="Name this filter"]',
  );
  if (!input) throw new Error("Name input missing");
  return input;
}

function optionElements() {
  return [...document.querySelectorAll<HTMLElement>('[role="option"]')];
}

async function typeName(value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value",
    )?.set?.call(nameInput(), value);
    nameInput().dispatchEvent(new Event("input", { bubbles: true }));
  });
}

async function pressKey(target: HTMLElement, key: string) {
  await act(async () => {
    target.dispatchEvent(
      new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }),
    );
  });
}

it("moves focus from the name into the suggestions and back with the arrow keys", async () => {
  await render();
  const input = nameInput();
  await act(async () => input.focus());

  await pressKey(input, "ArrowDown");
  const [favourite, short] = optionElements();
  expect(favourite?.textContent).toBe("Favourite clips");
  expect(document.activeElement).toBe(favourite);

  await pressKey(favourite as HTMLElement, "ArrowDown");
  expect(document.activeElement).toBe(short);

  await pressKey(short as HTMLElement, "ArrowUp");
  expect(document.activeElement).toBe(favourite);

  await pressKey(favourite as HTMLElement, "ArrowUp");
  expect(document.activeElement).toBe(input);
});

it("keeps typing filtering the suggestions, and Enter chooses the focused one and returns to the name", async () => {
  await render();
  const input = nameInput();
  await typeName("short");
  expect(optionElements().map((option) => option.textContent)).toEqual([
    "Short clips",
  ]);

  await pressKey(input, "ArrowDown");
  const [short] = optionElements();
  expect(document.activeElement).toBe(short);

  await pressKey(short as HTMLElement, "Enter");
  expect(nameInput().value).toBe("Short clips");
  expect(document.activeElement).toBe(nameInput());
});
