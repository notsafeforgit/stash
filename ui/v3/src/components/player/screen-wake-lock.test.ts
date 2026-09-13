// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createScreenWakeLock } from "./screen-wake-lock";

class Sentinel extends EventTarget {
  released = false;
  type = "screen" as const;
  onrelease = null;
  release = vi.fn(async () => {
    this.released = true;
    this.dispatchEvent(new Event("release"));
  });
}
const request = vi.fn<() => Promise<WakeLockSentinel>>();
let visibility: DocumentVisibilityState;
beforeEach(() => {
  request.mockReset();
  visibility = "visible";
  vi.spyOn(document, "visibilityState", "get").mockImplementation(
    () => visibility,
  );
  vi.stubGlobal("navigator", { wakeLock: { request } });
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it("releases late requests after pause and unmount", async () => {
  const pending = Promise.withResolvers<WakeLockSentinel>();
  request.mockReturnValue(pending.promise);
  const controller = createScreenWakeLock();
  controller.setActive(true);
  controller.setActive(false);
  controller.dispose();
  const lock = new Sentinel();
  pending.resolve(lock);
  await pending.promise;
  expect(lock.release).toHaveBeenCalledOnce();
});
it("reacquires on return to a visible playing page, without retry loops on refusal", async () => {
  const first = new Sentinel();
  const second = new Sentinel();
  request
    .mockResolvedValueOnce(first)
    .mockRejectedValueOnce(new Error("Power saving"))
    .mockResolvedValueOnce(second);
  const controller = createScreenWakeLock();
  controller.setActive(true);
  await Promise.resolve();
  visibility = "hidden";
  document.dispatchEvent(new Event("visibilitychange"));
  expect(first.release).toHaveBeenCalledOnce();
  visibility = "visible";
  document.dispatchEvent(new Event("visibilitychange"));
  await Promise.resolve();
  controller.setActive(true);
  expect(request).toHaveBeenCalledTimes(2);
  controller.setActive(false);
  controller.setActive(true);
  await Promise.resolve();
  expect(request).toHaveBeenCalledTimes(3);
  controller.dispose();
  expect(second.release).toHaveBeenCalledOnce();
});
