// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Button, buttonVariants } from "@/components/ui/button";

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

it("maps the default variant to the solid data variant and keeps others", async () => {
  await act(async () =>
    root.render(
      <>
        <Button>Save</Button>
        <Button variant="outline">Cancel</Button>
      </>,
    ),
  );
  const [save, cancel] = container.querySelectorAll("button");
  expect(save?.dataset.variant).toBe("solid");
  expect(cancel?.dataset.variant).toBe("outline");
});

it("renders a link through render and keeps link semantics", async () => {
  await act(async () =>
    root.render(
      <Button variant="outline" render={<a href="/settings" />}>
        Settings
      </Button>,
    ),
  );
  const link = container.querySelector("a");
  expect(link?.getAttribute("href")).toBe("/settings");
  expect(link?.dataset.slot).toBe("button");
  expect(link?.dataset.variant).toBe("outline");
});

it("gives plugin elements the solid look by default and appends their classes", () => {
  const solid = buttonVariants();
  expect(solid).toContain("bg-[color:var(--ui-primary)]");
  expect(solid).not.toContain("bg-[color:var(--ui-background)]");

  const outline = buttonVariants({ variant: "outline", size: "sm" });
  expect(outline).toContain("bg-[color:var(--ui-background)]");
  expect(outline).toContain("h-7");
  expect(outline).not.toContain("bg-[color:var(--ui-primary)]");

  expect(buttonVariants({ className: "self-start" })).toContain("self-start");
});
