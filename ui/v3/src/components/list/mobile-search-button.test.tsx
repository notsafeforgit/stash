// @vitest-environment jsdom
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MobileSearchButton } from "./mobile-search-button";

const messages = {
  search: "Search",
  "list.active_search": "Search: {query}",
  "actions.edit_search": "Edit search",
  "actions.clear_search": "Clear search",
};

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

async function renderButton(query?: string) {
  const onOpen = vi.fn();
  await act(async () =>
    root.render(
      <IntlProvider locale="en" messages={messages}>
        <MobileSearchButton
          query={query}
          buttonRef={createRef<HTMLButtonElement>()}
          inputRef={createRef<HTMLInputElement>()}
          onOpen={onOpen}
          onClear={vi.fn()}
        />
      </IntlProvider>,
    ),
  );
  const button = container.querySelector<HTMLButtonElement>("button");
  if (!button) throw new Error("Missing search button");
  return { button, onOpen };
}

async function settle(ms = 0) {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms));
  });
}

async function pointer(
  element: Element,
  type: "pointerdown" | "pointerup",
  pointerType: "touch" | "mouse",
) {
  await act(async () => {
    element.dispatchEvent(
      new PointerEvent(type, {
        bubbles: true,
        cancelable: true,
        button: 0,
        buttons: type === "pointerdown" ? 1 : 0,
        isPrimary: true,
        pointerId: 1,
        pointerType,
        clientX: 5,
        clientY: 5,
      }),
    );
  });
}

function openMenu() {
  return document.querySelector<HTMLElement>('[role="menu"]');
}

function item(text: string) {
  const found = [
    ...document.querySelectorAll<HTMLElement>('[role="menuitem"]'),
  ].find((element) => element.textContent?.trim() === text);
  if (!found) throw new Error(`No menu item named ${text}`);
  return found;
}

it("opens the preview on a touch hold, and the click after release does not open the input", async () => {
  const { button, onOpen } = await renderButton("a query");
  await pointer(button, "pointerdown", "touch");
  await settle(600);
  expect(openMenu()?.textContent).toContain("Edit search");
  await pointer(button, "pointerup", "touch");
  await act(async () => button.click());
  expect(onOpen).not.toHaveBeenCalled();
  expect(openMenu()).not.toBeNull();

  await act(async () => item("Edit search").click());
  expect(onOpen).toHaveBeenCalledTimes(1);
  await settle();
  expect(openMenu()).toBeNull();
});

it("opens the input on a tap and ignores a mouse press for the preview", async () => {
  const { button, onOpen } = await renderButton("a query");
  await pointer(button, "pointerdown", "mouse");
  await settle(600);
  expect(openMenu()).toBeNull();
  await pointer(button, "pointerup", "mouse");
  await act(async () => button.click());
  expect(onOpen).toHaveBeenCalledTimes(1);
});

it("does not preview a search without a query", async () => {
  const { button } = await renderButton();
  await pointer(button, "pointerdown", "touch");
  await settle(600);
  expect(openMenu()).toBeNull();
  await pointer(button, "pointerup", "touch");
});

it("opens the same preview from the context-menu key and keeps focus on the button after Escape", async () => {
  const { button } = await renderButton("a query");
  const event = new MouseEvent("contextmenu", {
    bubbles: true,
    cancelable: true,
  });
  await act(async () => button.dispatchEvent(event));
  await settle();
  expect(event.defaultPrevented).toBe(true);
  expect(openMenu()).not.toBeNull();

  await act(async () => {
    item("Edit search").dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Escape",
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  await settle();
  expect(openMenu()).toBeNull();
  expect(document.activeElement).toBe(button);
});
