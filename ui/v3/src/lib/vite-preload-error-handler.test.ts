// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const reload = vi.fn();
let target: EventTarget;
beforeEach(() => {
  vi.resetModules();
  reload.mockReset();
  sessionStorage.clear();
  target = new EventTarget();
  vi.stubGlobal(
    "window",
    Object.assign(target, {
      location: { reload, href: "http://localhost/performers" },
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());

it("lets local lazy boundaries handle preload failures without reloading", async () => {
  const { installVitePreloadErrorHandler } = await import(
    "./vite-preload-error-handler"
  );
  installVitePreloadErrorHandler();
  const event = Object.assign(
    new Event("vite:preloadError", { cancelable: true }),
    {
      payload: new Error("Failed to fetch dynamically imported module"),
    },
  );
  target.dispatchEvent(event);
  expect(reload).not.toHaveBeenCalled();
  // Preventing Vite's default would swallow the error before React can show it.
  expect(event.defaultPrevented).toBe(false);
});

it("still recovers once for an unhandled startup import failure", async () => {
  const { installVitePreloadErrorHandler } = await import(
    "./vite-preload-error-handler"
  );
  installVitePreloadErrorHandler();
  for (let i = 0; i < 2; i++) {
    target.dispatchEvent(
      Object.assign(new Event("unhandledrejection", { cancelable: true }), {
        reason: new Error(
          "Failed to fetch dynamically imported module: startup.js",
        ),
      }),
    );
  }
  expect(reload).toHaveBeenCalledTimes(1);
});
