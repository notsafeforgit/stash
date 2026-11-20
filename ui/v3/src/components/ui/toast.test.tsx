// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { toast } from "@/components/ui/toast";
import { Toaster } from "@/components/ui/toaster";

let root: Root;
let container: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
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
  await act(async () => {
    toast.dismiss();
    root.unmount();
  });
  container.remove();
  vi.unstubAllGlobals();
});

function regionText(index = 0): string {
  return (
    document.querySelectorAll('[data-slot="toast-region"]')[index]
      ?.textContent ?? ""
  );
}

it("shows success and error messages in the notification region", async () => {
  await act(async () => root.render(<Toaster />));
  await act(async () => {
    toast.success("Saved");
    toast.error(new Error("Upload failed"));
  });
  expect(regionText()).toContain("Saved");
  expect(regionText()).toContain("Upload failed");
});

it("shows a rejected value as its message rather than an object", async () => {
  await act(async () => root.render(<Toaster />));
  await act(async () => toast.error({ code: 1 }));
  expect(regionText()).toContain("[object Object]");
});

it("sends a toast with a toasterId only to the region with that id", async () => {
  await act(async () =>
    root.render(
      <>
        <Toaster />
        <Toaster id="lightbox-test" />
      </>,
    ),
  );
  await act(async () => {
    toast.success("Shared message");
    toast.success("Lightbox message", { toasterId: "lightbox-test" });
  });
  expect(regionText(0)).toContain("Shared message");
  expect(regionText(0)).not.toContain("Lightbox message");
  expect(regionText(1)).toContain("Lightbox message");
  expect(regionText(1)).not.toContain("Shared message");
});

it("runs the action callback when its button is pressed", async () => {
  const onClick = vi.fn();
  await act(async () => root.render(<Toaster />));
  await act(async () =>
    toast.success("Image rotated.", {
      action: { label: "Undo", onClick },
    }),
  );
  const undo = [...document.querySelectorAll("button")].find(
    (button) => button.textContent === "Undo",
  );
  expect(undo).toBeDefined();
  await act(async () => undo?.click());
  expect(onClick).toHaveBeenCalledTimes(1);
});
