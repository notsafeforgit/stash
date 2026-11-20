// @vitest-environment jsdom
import type * as React from "react";
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { createTabsController } from "@ui-sdk/react/tabs";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./tabs";

let root: Root;
let container: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  // jsdom has no Web Animations; panels wait for their exit animations.
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

function tab(label: string) {
  const found = Array.from(
    container.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
  ).find((element) => element.textContent === label);
  if (!found) throw new Error(`no tab ${label}`);
  return found;
}

function panelText() {
  return Array.from(
    container.querySelectorAll<HTMLElement>('[role="tabpanel"]'),
  ).map((panel) => panel.textContent);
}

describe("Tabs", () => {
  it("reports the newly chosen string value and shows its panel", async () => {
    const onValueChange = vi.fn();
    function Controlled() {
      const [value, setValue] = useState("scene");
      return (
        <Tabs
          value={value}
          onValueChange={(next, details) => {
            onValueChange(next, details.reason);
            setValue(next);
          }}
        >
          <TabsList aria-label="Media">
            <TabsTrigger value="scene">Scenes</TabsTrigger>
            <TabsTrigger value="image">Images</TabsTrigger>
          </TabsList>
          <TabsContent value="scene">scene panel</TabsContent>
          <TabsContent value="image">image panel</TabsContent>
        </Tabs>
      );
    }
    await render(<Controlled />);

    expect(panelText()).toEqual(["scene panel"]);
    await act(async () => tab("Images").click());

    expect(onValueChange).toHaveBeenCalledWith("image", expect.any(String));
    expect(panelText()).toEqual(["image panel"]);
  });

  it("refuses to clear the selection", async () => {
    const onValueChange = vi.fn();
    const controller = createTabsController<string>();
    await render(
      <Tabs
        defaultValue="scene"
        controller={controller}
        onValueChange={onValueChange}
      >
        <TabsList aria-label="Media">
          <TabsTrigger value="scene">Scenes</TabsTrigger>
          <TabsTrigger value="image">Images</TabsTrigger>
        </TabsList>
        <TabsContent value="scene">scene panel</TabsContent>
      </Tabs>,
    );

    let changed = true;
    await act(async () => {
      changed = controller.setValue(null);
    });

    expect(changed).toBe(false);
    expect(onValueChange).not.toHaveBeenCalled();
    expect(tab("Scenes").getAttribute("aria-selected")).toBe("true");
  });

  it("uses up and down arrows for a vertical list and marks it so", async () => {
    await render(
      <Tabs defaultValue="general" orientation="vertical">
        <TabsList aria-label="Preferences">
          <TabsTrigger value="general">General</TabsTrigger>
          <TabsTrigger value="security">Security</TabsTrigger>
        </TabsList>
        <TabsContent value="general">general panel</TabsContent>
        <TabsContent value="security">security panel</TabsContent>
      </Tabs>,
    );

    const list = container.querySelector('[role="tablist"]');
    expect(list?.getAttribute("aria-orientation")).toBe("vertical");
    expect(tab("General").getAttribute("data-slot")).toBe("tabs-tab");

    await act(async () =>
      tab("General").dispatchEvent(
        new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }),
      ),
    );
    expect(tab("Security").getAttribute("aria-selected")).toBe("true");
  });

  it("marks a default-variant list for the pill styling", async () => {
    await render(
      <Tabs defaultValue="a">
        <TabsList aria-label="Pill" variant="default">
          <TabsTrigger value="a">A</TabsTrigger>
        </TabsList>
        <TabsContent value="a">a</TabsContent>
      </Tabs>,
    );

    expect(
      container
        .querySelector('[data-slot="tabs-list"]')
        ?.getAttribute("data-variant"),
    ).toBe("default");
  });
});
