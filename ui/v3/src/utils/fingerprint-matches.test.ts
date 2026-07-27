import { describe, expect, it } from "vitest";
import {
  findSceneFingerprintMatches,
  fingerprintDistance,
} from "./fingerprint-matches";

describe("fingerprintDistance", () => {
  it("counts differing bits in hexadecimal hashes", () => {
    expect(fingerprintDistance("0f", "00")).toBe(4);
    expect(fingerprintDistance("ffff", "ffff")).toBe(0);
  });

  it("rejects hashes with incompatible or invalid shapes", () => {
    expect(fingerprintDistance("0f", "000f")).toBe(Number.POSITIVE_INFINITY);
    expect(fingerprintDistance("zz", "00")).toBe(Number.POSITIVE_INFINITY);
  });
});

describe("findSceneFingerprintMatches", () => {
  it("matches OSHash only against local OSHash values", () => {
    const result = findSceneFingerprintMatches(
      [
        {
          algorithm: "OSHASH",
          hash: "shared",
          submissions: 7,
        },
      ],
      [
        {
          fingerprints: [
            { type: "oshash", value: "shared" },
            { type: "md5", value: "shared" },
          ],
        },
      ],
    );

    expect(result.oshash).toEqual([
      { algorithm: "OSHASH", hash: "shared", submissions: 7 },
    ]);
  });

  it("keeps perceptual hashes within distance eight and sorts best first", () => {
    const result = findSceneFingerprintMatches(
      [
        { algorithm: "PHASH", hash: "0003", submissions: 0 },
        { algorithm: "PHASH", hash: "ffff", submissions: 0 },
        { algorithm: "PHASH", hash: "0000", submissions: 0 },
      ],
      [{ fingerprints: [{ type: "phash", value: "0000" }] }],
    );

    expect(result.phash).toEqual([
      { hash: "0000", distance: 0 },
      { hash: "0003", distance: 2 },
    ]);
  });
});
