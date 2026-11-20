// @vitest-environment jsdom
import type * as React from "react";
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ClearableInput } from "./clearable-input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
  InputGroupText,
} from "./input-group";

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

async function render(ui: React.ReactNode) {
  await act(async () =>
    root.render(
      <IntlProvider locale="en" messages={{}}>
        {ui}
      </IntlProvider>,
    ),
  );
}

// React tracks the value property, so typing has to go through the native setter.
async function type(input: HTMLInputElement, value: string) {
  const setValue = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )?.set;
  await act(async () => {
    setValue?.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function Search({ onEnter }: { onEnter?: () => void }) {
  const [value, setValue] = useState("stash");
  return (
    <ClearableInput
      value={value}
      setValue={setValue}
      onEnter={onEnter}
      placeholder="Search scenes"
    />
  );
}

describe("ClearableInput", () => {
  it("offers a clear button only while there is text", async () => {
    await render(<Search />);
    expect(container.querySelector('[data-slot="input-clear"]')).not.toBeNull();

    const input = container.querySelector("input");
    if (!input) throw new Error("expected an input");
    await type(input, "");
    expect(container.querySelector('[data-slot="input-clear"]')).toBeNull();
  });

  it("empties the field and keeps focus on it when cleared", async () => {
    await render(<Search />);

    const input = container.querySelector("input");
    const clear = container.querySelector<HTMLButtonElement>(
      '[data-slot="input-clear"]',
    );
    if (!input || !clear) throw new Error("expected input and clear button");
    await act(async () => clear.click());

    expect(input.value).toBe("");
    expect(document.activeElement).toBe(input);
  });

  it("runs onEnter when Enter is pressed", async () => {
    const onEnter = vi.fn();
    await render(<Search onEnter={onEnter} />);

    const input = container.querySelector("input");
    if (!input) throw new Error("expected an input");
    await act(async () =>
      input.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Enter", bubbles: true }),
      ),
    );
    expect(onEnter).toHaveBeenCalledTimes(1);
  });
});

describe("InputGroup", () => {
  it("focuses its input when its text is pressed", async () => {
    await render(
      <InputGroup>
        <InputGroupAddon>
          <InputGroupText>https://</InputGroupText>
        </InputGroupAddon>
        <InputGroupInput aria-label="Website" />
      </InputGroup>,
    );

    const text = container.querySelector<HTMLElement>(
      '[data-slot="input-group-text"]',
    );
    text?.click();
    expect(document.activeElement).toBe(container.querySelector("input"));
  });

  it("keeps the addon's data-align and group slots", async () => {
    await render(
      <InputGroup>
        <InputGroupInput aria-label="Tags" />
        <InputGroupAddon align="inline-end">
          <InputGroupButton aria-label="Add tag">+</InputGroupButton>
        </InputGroupAddon>
      </InputGroup>,
    );

    expect(
      container
        .querySelector('[data-slot="input-group-addon"]')
        ?.getAttribute("data-align"),
    ).toBe("inline-end");
    const button = container.querySelector("button");
    expect(button?.getAttribute("data-size")).toBe("xs");
    expect(button?.getAttribute("type")).toBe("button");
  });
});
