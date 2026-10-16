// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { canPlayHDRHls } from "./player-utils";

afterEach(() => vi.unstubAllGlobals());

it("requires an HDR video display and HEVC Main 10 in the active media engine", () => {
  const main10 = 'video/mp4; codecs="hvc1.2.4.L153.B0"';
  const supported = vi.fn((type: string) => type === main10);
  vi.stubGlobal("matchMedia", () => ({ matches: true }));
  vi.stubGlobal(
    "MediaSource",
    Object.assign(vi.fn(), { isTypeSupported: supported }),
  );
  expect(canPlayHDRHls()).toBe(true);
  expect(supported).toHaveBeenCalledWith(main10);

  // Safari's active MMS capability takes precedence over classic MSE.
  vi.stubGlobal(
    "ManagedMediaSource",
    Object.assign(vi.fn(), { isTypeSupported: () => false }),
  );
  expect(canPlayHDRHls()).toBe(false);
  vi.stubGlobal(
    "ManagedMediaSource",
    Object.assign(vi.fn(), { isTypeSupported: supported }),
  );
  vi.stubGlobal("matchMedia", () => ({ matches: false }));
  expect(canPlayHDRHls()).toBe(false);
});

it("uses Safari's display query when video-dynamic-range is unsupported", () => {
  const supported = vi.fn(() => true);
  vi.stubGlobal(
    "ManagedMediaSource",
    Object.assign(vi.fn(), { isTypeSupported: supported }),
  );
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: query === "(dynamic-range: high)",
  }));
  expect(canPlayHDRHls()).toBe(true);
  expect(supported).toHaveBeenCalled();

  // An explicitly SDR video plane must win over an HDR display, when the
  // browser implements the more specific video query.
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches:
      query === "(dynamic-range: high)" ||
      query === "(video-dynamic-range: standard)",
  }));
  supported.mockClear();
  expect(canPlayHDRHls()).toBe(false);
  expect(supported).not.toHaveBeenCalled();
});
