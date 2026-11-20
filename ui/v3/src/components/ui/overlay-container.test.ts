// @vitest-environment jsdom
import { createRef } from "react";
import { describe, expect, it } from "vitest";
import { overlayFocus } from "./overlay-container";

describe("overlayFocus", () => {
  it("maps Base UI's false to ui-sdk's none and true to the default", () => {
    expect(overlayFocus(false)).toBe("none");
    expect(overlayFocus(true)).toBeUndefined();
    expect(overlayFocus(undefined)).toBeUndefined();
  });

  it("reads a ref when the overlay asks for its focus target", () => {
    const element = document.createElement("input");
    const ref = createRef<HTMLElement>();
    const target = overlayFocus(ref);
    expect(typeof target).toBe("function");
    ref.current = element;
    expect((target as () => HTMLElement | null)()).toBe(element);
  });

  it("passes keywords, selectors and elements through unchanged", () => {
    const element = document.createElement("button");
    expect(overlayFocus("content")).toBe("content");
    expect(overlayFocus("#search")).toBe("#search");
    expect(overlayFocus(element)).toBe(element);
  });
});
