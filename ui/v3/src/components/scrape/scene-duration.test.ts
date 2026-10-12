import { describe, expect, it } from "vitest";
import { compareSceneDuration } from "./scene-duration";

describe("scene duration comparison", () => {
  it("matches a provider duration without fingerprints", () => {
    expect(
      compareSceneDuration({ duration: 120 }, [{ duration: 123 }]),
    ).toMatchObject({ remote: 120, local: 123, difference: 3, matches: true });
  });

  it("shows remote duration without claiming a match when no local duration is known", () => {
    expect(
      compareSceneDuration({ duration: 120 }, [{ duration: 0 }]),
    ).toMatchObject({ remote: 120, local: undefined, matches: undefined });
  });

  it("prefers explicit scene duration, falling back to scraper file duration", () => {
    expect(
      compareSceneDuration({ duration: 120, file: { duration: 240 } })?.remote,
    ).toBe(120);
    expect(
      compareSceneDuration({ duration: 0, file: { duration: 240 } })?.remote,
    ).toBe(240);
  });

  it("uses the closest valid local file and reports multiple file context", () => {
    expect(
      compareSceneDuration({ duration: 120 }, [
        { duration: 0 },
        { duration: 240 },
        { duration: 121 },
      ]),
    ).toMatchObject({ local: 121, localCount: 2, matches: true });
  });

  it("uses unrounded seconds at the five second tolerance boundary", () => {
    expect(
      compareSceneDuration({ duration: 120 }, [{ duration: 125 }])?.matches,
    ).toBe(true);
    expect(
      compareSceneDuration({ duration: 120 }, [{ duration: 125.01 }])?.matches,
    ).toBe(false);
  });

  it("ignores missing, negative, zero and non-finite durations", () => {
    for (const duration of [
      null,
      undefined,
      -1,
      0,
      Number.NaN,
      Number.POSITIVE_INFINITY,
    ]) {
      expect(compareSceneDuration({ duration })).toBeUndefined();
      expect(
        compareSceneDuration({ duration: 120 }, [{ duration }])?.local,
      ).toBeUndefined();
    }
  });
});
