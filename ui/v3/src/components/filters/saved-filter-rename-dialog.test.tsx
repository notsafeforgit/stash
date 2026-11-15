// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, expect, it, vi } from "vitest";
import type { SavedFilterDataFragment } from "@/core/generated-graphql";
import { RenameSavedFilterDialog } from "./saved-filter-rename-dialog";

const messages = {
  filter_name: "Filter name",
  "actions.cancel": "Cancel",
  "actions.save": "Save",
  "search_filter.rename_saved_filter_title": "Rename saved filter",
  "search_filter.rename_name_exists":
    "A saved filter with this name already exists. Choose another name.",
};

const favourites = { id: "1", name: "Favourites" } as SavedFilterDataFragment;
const unrated = { id: "2", name: "Unrated" } as SavedFilterDataFragment;

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

let cleanup: (() => Promise<void>) | undefined;

afterEach(async () => {
  await cleanup?.();
  cleanup = undefined;
  vi.unstubAllGlobals();
  document.body.innerHTML = "";
});

async function mount(onRename: (name: string) => Promise<boolean>) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const onClose = vi.fn();
  await act(async () => {
    root.render(
      <IntlProvider locale="en-GB" messages={messages}>
        <RenameSavedFilterDialog
          filter={favourites}
          filters={[favourites, unrated]}
          onRename={onRename}
          onClose={onClose}
        />
      </IntlProvider>,
    );
  });
  cleanup = async () => {
    await act(async () => root.unmount());
    container.remove();
  };
  const input = document.querySelector<HTMLInputElement>("input");
  if (!input) throw new Error("Missing rename input");
  const save = () =>
    [...document.querySelectorAll("button")].find(
      (button) => button.textContent === "Save",
    );
  return { input, save, onClose };
}

it("blocks a case-insensitive duplicate and enables a unique name", async () => {
  const onRename = vi.fn(async () => true);
  const { input, save } = await mount(onRename);
  expect(input.value).toBe("Favourites");

  await act(async () => setInputValue(input, "  unrated "));
  expect(document.body.textContent).toContain(
    "A saved filter with this name already exists.",
  );
  expect(save()?.disabled).toBe(true);

  await act(async () => setInputValue(input, "  Top rated  "));
  expect(save()?.disabled).toBe(false);
});

it("submits the trimmed name and closes once the rename is saved", async () => {
  const onRename = vi.fn(async () => true);
  const { input, save, onClose } = await mount(onRename);

  await act(async () => setInputValue(input, "  Top rated  "));
  await act(async () => {
    save()?.click();
  });

  expect(onRename).toHaveBeenCalledWith("Top rated");
  expect(onClose).toHaveBeenCalledTimes(1);
});

it("keeps the dialog open when the rename fails", async () => {
  const onRename = vi.fn(async () => false);
  const { input, save, onClose } = await mount(onRename);

  await act(async () => setInputValue(input, "Top rated"));
  await act(async () => {
    save()?.click();
  });

  expect(onRename).toHaveBeenCalledWith("Top rated");
  expect(onClose).not.toHaveBeenCalled();
});
