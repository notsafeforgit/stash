// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ListPagination } from "@/components/list/list-pagination";
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

function pageLink(text: string): HTMLElement {
  const element = Array.from(
    container.querySelectorAll<HTMLElement>('[data-slot="pagination-link"]'),
  ).find((link) => link.textContent === text);
  if (!element) throw new Error(`Missing page ${text}`);
  return element;
}

function control(name: string): HTMLElement {
  const element = container.querySelector<HTMLElement>(
    `[aria-label="${name}"]`,
  );
  if (!element) throw new Error(`Missing ${name} control`);
  return element;
}

it("renders nothing when everything fits on one page", async () => {
  await render(
    <ListPagination
      currentPage={1}
      itemsPerPage={10}
      totalItems={5}
      onChangePage={vi.fn()}
    />,
  );
  expect(container.querySelector('[aria-label="pagination"]')).toBeNull();
});

it("marks the current page and moves to a chosen page", async () => {
  const onChangePage = vi.fn();
  await render(
    <ListPagination
      currentPage={2}
      itemsPerPage={10}
      totalItems={30}
      onChangePage={onChangePage}
    />,
  );
  expect(pageLink("2").getAttribute("aria-current")).toBe("page");
  await act(async () => pageLink("3").click());
  expect(onChangePage.mock.lastCall?.[0]).toBe(3);
});

it("moves with the previous and next controls", async () => {
  const onChangePage = vi.fn();
  await render(
    <ListPagination
      currentPage={2}
      itemsPerPage={10}
      totalItems={30}
      onChangePage={onChangePage}
    />,
  );
  await act(async () => control("Next").click());
  expect(onChangePage.mock.lastCall?.[0]).toBe(3);
  await act(async () => control("Previous").click());
  expect(onChangePage.mock.lastCall?.[0]).toBe(1);
});

it("does not move past the last page", async () => {
  const onChangePage = vi.fn();
  await render(
    <ListPagination
      currentPage={3}
      itemsPerPage={10}
      totalItems={30}
      onChangePage={onChangePage}
    />,
  );
  expect(control("Next").getAttribute("aria-disabled")).toBe("true");
  await act(async () => control("Next").click());
  expect(onChangePage).not.toHaveBeenCalled();
});

it("opens the page jump on the current page without changing page", async () => {
  const onChangePage = vi.fn();
  await render(
    <ListPagination
      currentPage={2}
      itemsPerPage={10}
      totalItems={30}
      onChangePage={onChangePage}
    />,
  );
  await act(async () => pageLink("2").click());
  expect(onChangePage).not.toHaveBeenCalled();
  expect(document.querySelector("form input")).not.toBeNull();
});

it("jumps to a typed page from the page-jump popover", async () => {
  const onChangePage = vi.fn();
  await render(
    <ListPagination
      currentPage={2}
      itemsPerPage={10}
      totalItems={30}
      onChangePage={onChangePage}
    />,
  );
  await act(async () => pageLink("2").click());
  const field = document.querySelector<HTMLInputElement>("form input");
  if (!field) throw new Error("Missing page field");
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value",
    )?.set?.call(field, "1");
    field.dispatchEvent(new Event("input", { bubbles: true }));
  });
  const go = Array.from(document.querySelectorAll("button")).find(
    (button) => button.textContent === "Go",
  );
  await act(async () => go?.click());
  expect(onChangePage.mock.lastCall?.[0]).toBe(1);
});
