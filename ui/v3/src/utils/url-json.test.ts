import { describe, expect, it } from "vitest";
import { decodeURLJSON, encodeURLJSON } from "./url-json";

describe("URL JSON encoding", () => {
  it.each(["ASCII", "café", "日本語 🎬", "Ã©"])("round trips %s", (title) => {
    const value = { title, values: [1, null, true] };
    expect(decodeURLJSON(encodeURLJSON(value))).toEqual(value);
  });

  it("preserves the meaning of unversioned Latin-1 URLs", () => {
    const value = { title: "café Ã©" };
    expect(decodeURLJSON(btoa(JSON.stringify(value)))).toEqual(value);
  });

  it.each([
    "not!base64",
    "u./w==",
    "u.e30=garbage",
    btoa("not json"),
  ])("rejects invalid input %s", (value) =>
    expect(() => decodeURLJSON(value)).toThrow());
});
