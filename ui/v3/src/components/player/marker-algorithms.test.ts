// @vitest-environment jsdom
import { expect, it } from "vitest";
import { computeTagColors, findMWIS } from "./player-utils";

it("chooses a maximum-duration set of disjoint marker ranges", () => {
  const markers = [
    { title: "1", primaryTag: { name: "tag" }, seconds: 0, end_seconds: 4 },
    { title: "2", primaryTag: { name: "tag" }, seconds: 3, end_seconds: 10 },
    { title: "3", primaryTag: { name: "tag" }, seconds: 4, end_seconds: 8 },
    { title: "4", primaryTag: { name: "tag" }, seconds: 8, end_seconds: 12 },
  ];
  const selected = findMWIS(markers);
  expect(selected.map((marker) => marker.title)).toEqual(["1", "3", "4"]);
  expect(findMWIS([])).toEqual([]);
});

it("assigns deterministic colours to all tag names, including object property names", () => {
  const tags = ["__proto__", "constructor", "日本語", "Tag"];
  const colors = computeTagColors(tags);
  expect(colors).toEqual(computeTagColors(tags));
  for (const tag of tags) expect(colors[tag]).toMatch(/^#[0-9a-f]{8}$/);
  expect(new Set(Object.values(colors)).size).toBe(tags.length);
});
