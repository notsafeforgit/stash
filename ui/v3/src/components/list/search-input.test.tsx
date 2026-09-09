// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { expect, it, vi } from "vitest";
import { SearchInput } from "./search-input";

it("commits pending text before a closing search row unmounts", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.useFakeTimers();
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const onChange = vi.fn();
  try {
    await act(async () =>
      root.render(
        <IntlProvider locale="en">
          <SearchInput value="" onChange={onChange} mobile />
        </IntlProvider>,
      ),
    );
    const input = container.querySelector("input");
    const setValue = Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value",
    )?.set;
    if (!input || !setValue) throw new Error("Missing search input");
    await act(async () => {
      input.focus();
      setValue.call(input, "new search");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    expect(onChange).not.toHaveBeenCalled();
    await act(async () => {
      input.blur();
      root.render(null);
    });
    expect(onChange).toHaveBeenCalledExactlyOnceWith("new search");
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(onChange).toHaveBeenCalledTimes(1);
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  }
});

it("syncs external filter resets without changing the input's initial default", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const onChange = vi.fn();
  const onError = vi.spyOn(console, "error").mockImplementation(() => {});
  const render = (value: string) =>
    root.render(
      <IntlProvider locale="en">
        <SearchInput value={value} onChange={onChange} />
      </IntlProvider>,
    );
  try {
    await act(async () => render("original"));
    const input = container.querySelector("input");
    expect(input?.value).toBe("original");
    await act(async () => render("updated"));
    expect(container.querySelector("input")).toBe(input);
    expect(input?.value).toBe("updated");
    expect(onChange).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  } finally {
    await act(async () => root.unmount());
    container.remove();
    onError.mockRestore();
    vi.unstubAllGlobals();
  }
});

it("clears immediately even if the row closes before another input blur", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const onChange = vi.fn();
  try {
    await act(async () =>
      root.render(
        <IntlProvider locale="en">
          <SearchInput value="previous search" onChange={onChange} />
        </IntlProvider>,
      ),
    );
    const clearButton = container.querySelector<HTMLButtonElement>(
      '[aria-label="Clear"]',
    );
    if (!clearButton) throw new Error("Missing clear control");
    await act(async () => clearButton.click());
    await act(async () => root.render(null));
    expect(onChange).toHaveBeenCalledExactlyOnceWith("");
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});
