// @vitest-environment jsdom
import type * as React from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ScrollArea } from "./scroll-area";

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

describe("ScrollArea", () => {
  it("scrolls its children inside the viewport and applies the viewport class", async () => {
    await render(
      <ScrollArea className="min-h-0" viewportClassName="h-64">
        <ul>
          <li>Alpha</li>
        </ul>
      </ScrollArea>,
    );

    const viewport = container.querySelector(
      '[data-slot="scroll-area-viewport"]',
    );
    expect(viewport?.classList.contains("h-64")).toBe(true);
    expect(viewport?.textContent).toBe("Alpha");
    expect(
      container
        .querySelector('[data-slot="scroll-area"]')
        ?.classList.contains("min-h-0"),
    ).toBe(true);
  });

  it("renders a vertical scrollbar by default and a horizontal one on request", async () => {
    await render(
      <ScrollArea>
        <p>wide table</p>
      </ScrollArea>,
    );
    const scrollbars = () =>
      Array.from(
        container.querySelectorAll('[data-slot="scroll-area-scrollbar"]'),
      ).map((element) => element.getAttribute("data-orientation"));
    expect(scrollbars()).toEqual(["vertical"]);

    await render(
      <ScrollArea horizontal>
        <p>wide table</p>
      </ScrollArea>,
    );
    expect(scrollbars()).toEqual(["vertical", "horizontal"]);
  });
});
