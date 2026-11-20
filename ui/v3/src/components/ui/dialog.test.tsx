// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from "./dialog";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetTitle,
  SheetTrigger,
} from "./sheet";

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

function render(element: React.ReactNode) {
  return act(async () =>
    root.render(<IntlProvider locale="en">{element}</IntlProvider>),
  );
}

it("closes a dialog from its default close button", async () => {
  const onOpenChange = vi.fn();
  await render(
    <Dialog defaultOpen onOpenChange={onOpenChange}>
      <DialogTrigger>Open</DialogTrigger>
      <DialogContent>
        <DialogTitle>Edit</DialogTitle>
      </DialogContent>
    </Dialog>,
  );
  const close = document.querySelector<HTMLButtonElement>(
    '[data-slot="dialog-close"]',
  );
  expect(close?.textContent).toBe("Close");
  await act(async () => close?.click());
  expect(onOpenChange).toHaveBeenCalledWith(false, expect.anything());
});

it("omits the default close button when the content supplies its own", async () => {
  await render(
    <Dialog defaultOpen>
      <DialogTrigger>Open</DialogTrigger>
      <DialogContent showCloseButton={false}>
        <DialogTitle>Confirm</DialogTitle>
      </DialogContent>
    </Dialog>,
  );
  expect(document.querySelector('[role="dialog"]')).not.toBeNull();
  expect(document.querySelector('[data-slot="dialog-close"]')).toBeNull();
});

it("records the sheet edge on its content and closes from its close button", async () => {
  const onOpenChange = vi.fn();
  await render(
    <Sheet defaultOpen onOpenChange={onOpenChange}>
      <SheetTrigger>Filters</SheetTrigger>
      <SheetContent side="left">
        <SheetTitle>Filters</SheetTitle>
        <SheetClose>Done</SheetClose>
      </SheetContent>
    </Sheet>,
  );
  const content = document.querySelector<HTMLElement>('[role="dialog"]');
  expect(content?.dataset.side).toBe("left");
  const done = [...document.querySelectorAll("button")].find(
    (button) => button.textContent === "Done",
  );
  await act(async () => done?.click());
  expect(onOpenChange).toHaveBeenCalledWith(false, expect.anything());
});
