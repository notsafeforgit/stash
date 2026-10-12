import { describe, expect, it } from "vitest";
import { matchPackage } from "./package-search";

const pkg = {
  name: "Network scraper",
  package_id: "network-scraper",
  metadata: {
    scrapes: ["Example Studio", "Another Studio"],
    scene_urls: ["example.test/scenes/"],
    performer_urls: ["people.test/models/"],
  },
};

describe("package search", () => {
  it("keeps name and ID matches and ignores surrounding whitespace", () => {
    expect(matchPackage(pkg, "  NETWORK  ")).toEqual({});
    expect(matchPackage(pkg, "network-scraper")).toEqual({});
    expect(matchPackage(pkg, "  ")).toEqual({});
  });

  it("finds supported studios without requiring their name in the package ID", () => {
    expect(matchPackage(pkg, "example studio")).toEqual({
      via: "Example Studio",
    });
  });

  it("finds a domain, URL fragment, or pasted full URL across entity types", () => {
    expect(matchPackage(pkg, "example.test")).toEqual({
      via: "example.test/scenes/",
    });
    expect(
      matchPackage(pkg, "HTTPS://EXAMPLE.TEST/scenes/123?foo=bar"),
    ).toEqual({ via: "example.test/scenes/" });
    expect(matchPackage(pkg, "https://people.test/models/person")).toEqual({
      via: "people.test/models/",
    });
  });

  it("handles old indexes and ignores empty or malformed metadata", () => {
    expect(
      matchPackage({ name: "Old", package_id: "old" }, "example"),
    ).toBeUndefined();
    expect(
      matchPackage(
        {
          ...pkg,
          metadata: { scrapes: false, scene_urls: [null, 12, "", "  "] },
        },
        "unrelated",
      ),
    ).toBeUndefined();
  });

  it("does not search unrelated metadata or evaluate URL fragments as regexes", () => {
    expect(
      matchPackage({ ...pkg, metadata: { description: "Example" } }, "example"),
    ).toBeUndefined();
    expect(
      matchPackage({ ...pkg, metadata: { scene_urls: [".*"] } }, "unrelated"),
    ).toBeUndefined();
  });
});
