// @vitest-environment jsdom
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Button } from "./button";
import { OverlayContainerProvider } from "./overlay-container";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "./dropdown-menu";

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

async function click(element: HTMLElement) {
  await act(async () => element.click());
  await settle();
}

async function pressKey(element: HTMLElement, key: string) {
  await act(async () => {
    element.dispatchEvent(
      new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }),
    );
  });
  await settle();
}

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

function trigger(text: string) {
  const found = [...container.querySelectorAll<HTMLElement>("button")].find(
    (button) => button.textContent?.trim() === text,
  );
  if (!found) throw new Error(`No trigger named ${text}`);
  return found;
}

function openMenu() {
  return document.querySelector<HTMLElement>('[role="menu"]');
}

function item(text: string) {
  const found = [
    ...document.querySelectorAll<HTMLElement>(
      '[role="menuitem"], [role="menuitemcheckbox"], [role="menuitemradio"]',
    ),
  ].find((element) => element.textContent?.trim() === text);
  if (!found) throw new Error(`No menu item named ${text}`);
  return found;
}

it("runs an item's click handler for a pointer click and for Enter, then closes", async () => {
  const onClick = vi.fn();
  await render(
    <DropdownMenu>
      <DropdownMenuTrigger>Actions</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem onClick={onClick}>Rename</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>,
  );

  await click(trigger("Actions"));
  await pressKey(item("Rename"), "Enter");
  expect(onClick).toHaveBeenCalledTimes(1);
  expect(openMenu()).toBeNull();

  await click(trigger("Actions"));
  await click(item("Rename"));
  expect(onClick).toHaveBeenCalledTimes(2);
  expect(openMenu()).toBeNull();
});

it("returns focus to the trigger when Escape closes the menu", async () => {
  await render(
    <DropdownMenu>
      <DropdownMenuTrigger>Actions</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem>Rename</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>,
  );

  await click(trigger("Actions"));
  await pressKey(item("Rename"), "Escape");
  expect(openMenu()).toBeNull();
  expect(document.activeElement).toBe(trigger("Actions"));
});

it("marks a destructive item and leaves the others plain", async () => {
  await render(
    <DropdownMenu>
      <DropdownMenuTrigger>Actions</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem>Rename</DropdownMenuItem>
        <DropdownMenuItem variant="destructive">Delete</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>,
  );

  await click(trigger("Actions"));
  expect(item("Delete").hasAttribute("data-destructive")).toBe(true);
  expect(item("Rename").hasAttribute("data-destructive")).toBe(false);
});

it("does not run a disabled item", async () => {
  const onClick = vi.fn();
  await render(
    <DropdownMenu>
      <DropdownMenuTrigger>Actions</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem disabled onClick={onClick}>
          Archive
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>,
  );

  await click(trigger("Actions"));
  await click(item("Archive"));
  expect(onClick).not.toHaveBeenCalled();
  expect(openMenu()).not.toBeNull();
  expect(item("Archive").getAttribute("aria-disabled")).toBe("true");
});

it("keeps a checkbox menu open while toggling its item", async () => {
  const onCheckedChange = vi.fn();
  await render(
    <DropdownMenu>
      <DropdownMenuTrigger>View</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuCheckboxItem onCheckedChange={onCheckedChange}>
          Line numbers
        </DropdownMenuCheckboxItem>
      </DropdownMenuContent>
    </DropdownMenu>,
  );

  await click(trigger("View"));
  await click(item("Line numbers"));
  expect(onCheckedChange).toHaveBeenCalledWith(true, expect.anything());
  expect(item("Line numbers").hasAttribute("data-checked")).toBe(true);
  expect(openMenu()).not.toBeNull();
});

it("chooses one radio item, reports it, and closes only when asked", async () => {
  const onValueChange = vi.fn();
  await render(
    <DropdownMenu>
      <DropdownMenuTrigger>Export</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuRadioGroup
          defaultValue="pdf"
          onValueChange={onValueChange}
        >
          <DropdownMenuRadioItem value="pdf">PDF</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="markdown">
            Markdown
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="text" closeOnSelect>
            Plain text
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>,
  );

  await click(trigger("Export"));
  await click(item("Markdown"));
  expect(onValueChange).toHaveBeenLastCalledWith("markdown", expect.anything());
  expect(item("Markdown").hasAttribute("data-checked")).toBe(true);
  expect(item("PDF").hasAttribute("data-checked")).toBe(false);
  expect(openMenu()).not.toBeNull();

  await click(item("Plain text"));
  expect(onValueChange).toHaveBeenLastCalledWith("text", expect.anything());
  expect(openMenu()).toBeNull();
});

it("renders labelled groups and separators", async () => {
  await render(
    <DropdownMenu>
      <DropdownMenuTrigger>Actions</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuGroup>
          <DropdownMenuLabel>Export</DropdownMenuLabel>
          <DropdownMenuItem>PDF</DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem>Delete</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>,
  );

  await click(trigger("Actions"));
  const group = openMenu()?.querySelector<HTMLElement>('[role="group"]');
  const labelId = group?.getAttribute("aria-labelledby");
  expect(labelId && document.getElementById(labelId)?.textContent).toBe(
    "Export",
  );
  expect(openMenu()?.querySelectorAll('[role="separator"]')).toHaveLength(1);
});

it("opens a submenu from its trigger and runs one of its items", async () => {
  const onClick = vi.fn();
  await render(
    <DropdownMenu>
      <DropdownMenuTrigger>Edit</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>Transform</DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            <DropdownMenuItem onClick={onClick}>Uppercase</DropdownMenuItem>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
      </DropdownMenuContent>
    </DropdownMenu>,
  );

  await click(trigger("Edit"));
  await pressKey(item("Transform"), "ArrowRight");
  await click(item("Uppercase"));
  expect(onClick).toHaveBeenCalledTimes(1);
  expect(openMenu()).toBeNull();
});

it("keeps a button trigger's variant and size on the button", async () => {
  await render(
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>
        Options
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem>Rename</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>,
  );

  const button = trigger("Options");
  expect(button.dataset.variant).toBe("outline");
  expect(button.dataset.size).toBe("sm");
  await click(button);
  expect(openMenu()).not.toBeNull();
});

it("renders the menu inside the overlay container when one is provided", async () => {
  const portals: { current: HTMLElement | null } = { current: null };
  await render(
    <OverlayContainerProvider container={portals}>
      <div
        ref={(element) => {
          portals.current = element;
        }}
        data-testid="portals"
      />
      <DropdownMenu>
        <DropdownMenuTrigger>Actions</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem>Rename</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </OverlayContainerProvider>,
  );

  await click(trigger("Actions"));
  const menu = openMenu();
  expect(menu).not.toBeNull();
  expect(portals.current?.contains(menu)).toBe(true);
});
