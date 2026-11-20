// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

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

function item(text: string): HTMLButtonElement {
  const element = [...container.querySelectorAll("button")].find(
    (button) => button.textContent?.trim() === text,
  );
  if (!element) throw new Error(`Missing toggle: ${text}`);
  return element;
}

it("reports the pressed values as an array and styles items from the group", async () => {
  const onValueChange = vi.fn();
  await act(async () =>
    root.render(
      <ToggleGroup
        aria-label="Mode"
        value={["keep"]}
        onValueChange={onValueChange}
        variant="outline"
        size="sm"
      >
        <ToggleGroupItem value="keep">Keep</ToggleGroupItem>
        <ToggleGroupItem value="true">Yes</ToggleGroupItem>
      </ToggleGroup>,
    ),
  );
  expect(item("Keep").getAttribute("aria-pressed")).toBe("true");
  expect(item("Yes").getAttribute("aria-pressed")).toBe("false");
  expect(item("Yes").dataset.variant).toBe("outline");
  expect(item("Yes").dataset.size).toBe("sm");

  await act(async () => item("Yes").click());
  expect(onValueChange.mock.calls[0]?.[0]).toEqual(["true"]);
});
