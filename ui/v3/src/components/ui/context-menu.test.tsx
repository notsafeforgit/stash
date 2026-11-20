// @vitest-environment jsdom
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "./context-menu";

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

async function render(node: ReactNode) {
  await act(async () => root.render(node));
}

async function settle(ms = 0) {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms));
  });
}

function area(text: string) {
  const found = [
    ...container.querySelectorAll<HTMLElement>("[data-slot]"),
  ].find((element) => element.textContent?.includes(text));
  if (!found) throw new Error(`No area containing ${text}`);
  return found;
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

async function contextMenuAt(element: Element, button: number) {
  await act(async () => {
    element.dispatchEvent(
      new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        button,
        clientX: 5,
        clientY: 5,
      }),
    );
  });
  await settle();
}

async function pointer(element: Element, type: string) {
  await act(async () => {
    element.dispatchEvent(
      new PointerEvent(type, {
        bubbles: true,
        cancelable: true,
        button: 0,
        isPrimary: true,
        pointerId: 1,
        pointerType: "touch",
        clientX: 5,
        clientY: 5,
      }),
    );
  });
}

it("opens at a right click and runs an item's click handler", async () => {
  const onClick = vi.fn();
  await render(
    <ContextMenu>
      <ContextMenuTrigger>
        <span>Scene title</span>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onClick={onClick}>Edit</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>,
  );

  expect(area("Scene title").dataset.slot).toBe("menu-context-trigger");
  await contextMenuAt(area("Scene title"), 2);
  expect(openMenu()).not.toBeNull();
  await act(async () => item("Edit").click());
  await settle();
  expect(onClick).toHaveBeenCalledTimes(1);
  expect(openMenu()).toBeNull();
});

it("portals the menu to the document body rather than the row that opened it", async () => {
  await render(
    <table>
      <tbody>
        <ContextMenu>
          <ContextMenuTrigger render={<tr />}>
            <td>Row title</td>
          </ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem>Edit</ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      </tbody>
    </table>,
  );

  await contextMenuAt(area("Row title"), 2);
  const menu = openMenu();
  expect(menu).not.toBeNull();
  expect(menu?.parentElement).toBe(document.body);
});

it("opens on a long press and stays open after the finger lifts", async () => {
  await render(
    <ContextMenu>
      <ContextMenuTrigger>
        <span>Scene title</span>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem>Edit</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>,
  );

  const trigger = area("Scene title");
  await pointer(trigger, "pointerdown");
  expect(openMenu()).toBeNull();
  await settle(600);
  expect(openMenu()).not.toBeNull();
  await pointer(trigger, "pointerup");
  await settle();
  expect(openMenu()).not.toBeNull();
});

it("cancels a long press that moves away before it opens", async () => {
  await render(
    <ContextMenu>
      <ContextMenuTrigger>
        <span>Scene title</span>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem>Edit</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>,
  );

  const trigger = area("Scene title");
  await pointer(trigger, "pointerdown");
  await act(async () => {
    trigger.dispatchEvent(
      new PointerEvent("pointermove", {
        bubbles: true,
        pointerId: 1,
        pointerType: "touch",
        isPrimary: true,
        clientX: 5,
        clientY: 60,
      }),
    );
  });
  await settle(600);
  expect(openMenu()).toBeNull();
});

it("leaves the browser's own menu to a disabled area", async () => {
  await render(
    <ContextMenu>
      <ContextMenuTrigger disabled>
        <span>Scene title</span>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem>Edit</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>,
  );

  const event = new MouseEvent("contextmenu", {
    bubbles: true,
    cancelable: true,
    button: 2,
  });
  await act(async () => {
    area("Scene title").dispatchEvent(event);
  });
  await settle();
  expect(openMenu()).toBeNull();
  expect(event.defaultPrevented).toBe(false);
});

it("opens from the keyboard's context-menu key on a focused link inside the area", async () => {
  await render(
    <ContextMenu>
      <ContextMenuTrigger>
        <a href="/scenes/1">Scene title</a>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem>Edit</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>,
  );

  const link = container.querySelector<HTMLElement>("a");
  if (!link) throw new Error("Missing link");
  link.focus();
  await contextMenuAt(link, 0);
  expect(openMenu()).not.toBeNull();
});
