// @vitest-environment jsdom
import type * as React from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Button } from "./button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "./collapsible";

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

async function render(ui: React.ReactNode) {
  await act(async () => root.render(ui));
}

describe("Collapsible", () => {
  it("opens its panel from the trigger and reports the change", async () => {
    const onOpenChange = vi.fn();
    await render(
      <Collapsible onOpenChange={onOpenChange}>
        <CollapsibleTrigger>Details</CollapsibleTrigger>
        <CollapsibleContent className="pt-3">Body text</CollapsibleContent>
      </Collapsible>,
    );

    const trigger = container.querySelector<HTMLButtonElement>(
      '[data-slot="collapsible-trigger"]',
    );
    expect(trigger?.getAttribute("aria-expanded")).toBe("false");
    expect(container.textContent).not.toContain("Body text");

    await act(async () => trigger?.click());

    expect(onOpenChange).toHaveBeenCalledWith(true, expect.any(Object));
    expect(trigger?.getAttribute("aria-expanded")).toBe("true");
    const panel = container.querySelector('[data-slot="collapsible-panel"]');
    expect(panel?.textContent).toBe("Body text");
    expect(panel?.classList.contains("pt-3")).toBe(true);
  });

  it("keeps a closed panel mounted when keepMounted is set", async () => {
    await render(
      <Collapsible>
        <CollapsibleTrigger>Settings</CollapsibleTrigger>
        <CollapsibleContent keepMounted>Draft</CollapsibleContent>
      </Collapsible>,
    );

    expect(container.textContent).toContain("Draft");
  });

  it("opens from a trigger rendered as a Button", async () => {
    await render(
      <Collapsible>
        <CollapsibleTrigger render={<Button variant="outline" />}>
          History
        </CollapsibleTrigger>
        <CollapsibleContent>Past edits</CollapsibleContent>
      </Collapsible>,
    );

    const trigger = container.querySelector<HTMLButtonElement>("button");
    await act(async () => trigger?.click());

    expect(trigger?.getAttribute("aria-expanded")).toBe("true");
    expect(container.textContent).toContain("Past edits");
  });
});
