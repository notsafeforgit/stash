import { expect, it } from "vitest";
import { FilterMode } from "@/core/generated-graphql";
import { ListFilterModel } from "./list-filter/filter";
import { SceneQueue } from "./scene-queue";

const scene = "00a0cf9d-4659-4e18-891e-2fc13ee26af0";
it("preserves AST selections in playback links", () => {
  const filter = new ListFilterModel(FilterMode.Scenes);
  filter.configureFromSavedFilter({
    filter_ast: {
      root: {
        group: {
          operator: "OR",
          children: [
            {
              condition: {
                field: "title",
                value: { modifier: "INCLUDES", value: "one" },
              },
            },
            {
              condition: {
                field: "title",
                value: { modifier: "INCLUDES", value: "two" },
              },
            },
          ],
        },
      },
    },
  });
  const link = SceneQueue.fromListFilterModel(filter).makeLink(scene, {
    autoPlay: true,
  });
  const params = new URL(link, "https://stash.test").searchParams;
  expect(params.has("qfc")).toBe(false);
  expect(params.has("qfa")).toBe(true);
  expect(SceneQueue.fromQueryParameters(params).query?.makeFilterAST()).toEqual(
    filter.makeFilterAST(),
  );
});

it("keeps UUIDs intact in an explicit playback selection", () => {
  const link = SceneQueue.fromSceneIDList([scene]).makeLink(scene, {});
  expect(
    SceneQueue.fromQueryParameters(
      new URL(link, "https://stash.test").searchParams,
    ).sceneIDs,
  ).toEqual([scene]);
});

it("rejects old playback predicates rather than playing an unfiltered list", () => {
  expect(() =>
    SceneQueue.fromQueryParameters(new URLSearchParams("qfp=1&qfc=old")),
  ).toThrow(/Flat playback filters/);
});
