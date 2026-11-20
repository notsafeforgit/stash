// @vitest-environment jsdom
import type * as React from "react";
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Input } from "./input";
import { Label } from "./label";
import { Textarea } from "./textarea";

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
  await act(async () => root.render(ui));
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

describe("Input", () => {
  it("shows a numeric value as text in a native input", async () => {
    await render(<Input type="number" value={7} onChange={() => {}} />);

    const input = container.querySelector("input");
    expect(input?.value).toBe("7");
    expect(input?.getAttribute("data-slot")).toBe("input");
  });

  it("keeps the caller's aria-invalid and selectable-text markers", async () => {
    await render(
      <Input aria-invalid="true" data-selectable-text className="w-full" />,
    );

    const input = container.querySelector("input");
    expect(input?.getAttribute("aria-invalid")).toBe("true");
    expect(input?.hasAttribute("data-selectable-text")).toBe(true);
    expect(input?.className).toBe("w-full");
  });
});

describe("Textarea", () => {
  it("grows with its text by default", async () => {
    await render(<Textarea aria-label="Notes" defaultValue="one" />);

    const textarea = container.querySelector("textarea");
    expect(textarea?.getAttribute("data-slot")).toBe("textarea");
    expect(textarea?.getAttribute("style")).toContain("field-sizing");
  });
});

describe("Label", () => {
  it("names the control it points at", async () => {
    await render(
      <>
        <Label htmlFor="endpoint">Endpoint</Label>
        <input id="endpoint" />
      </>,
    );

    const label = container.querySelector("label");
    expect(label?.getAttribute("data-slot")).toBe("label");
    expect(label?.control).toBe(container.querySelector("input"));
  });
});

function Controlled() {
  const [value, setValue] = useState("first");
  return (
    <Input
      aria-label="Controlled"
      value={value}
      onChange={(event) => setValue(event.currentTarget.value)}
    />
  );
}

describe("Input controlled by a caller", () => {
  it("keeps the caller's value in sync with each change", async () => {
    await render(<Controlled />);

    const input = container.querySelector("input");
    if (!input) throw new Error("expected an input");
    await type(input, "second");
    expect(input.value).toBe("second");
  });
});
