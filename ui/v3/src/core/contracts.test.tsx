// @vitest-environment jsdom
import type { ComponentProps } from "react";
import { InMemoryCache } from "@apollo/client";
import { expect, expectTypeOf, it } from "vitest";
import { writeConfigKey } from "@/hooks/config";
import type * as GQL from "./generated-graphql";
import {
  SceneListItems,
  type ImageListItems,
} from "@/components/list/entity-list-items";
import {
  sceneDownloadSnapshot,
  type DownloadableScene,
} from "@/components/offline/scene-download-input";
import {
  entityDestination,
  localNavigationHref,
  type EntityDestination,
} from "./navigation";

it("retains route parameters and validates runtime return URLs", () => {
  document.head.innerHTML = '<base href="/stash/">';
  expect(entityDestination.scene("12", { t: 42, tab: "markers" })).toEqual({
    to: "/scenes/$sceneId",
    params: { sceneId: "12" },
    search: { t: 42, tab: "markers" },
  });
  expect(localNavigationHref("/scenes?fa=u.encoded#cards")).toBe(
    "/stash/scenes?fa=u.encoded#cards",
  );
  expect(localNavigationHref("/stash/scenes?q=日本語")).toContain(
    "/stash/scenes?",
  );
  expect(
    localNavigationHref("https://external.example/scenes"),
  ).toBeUndefined();
  expect(localNavigationHref("//external.example/scenes")).toBeUndefined();
  expect(localNavigationHref("/../other")).toBeUndefined();
  expect(localNavigationHref("/%2e%2e/other")).toBeUndefined();
  expect(
    localNavigationHref(`${window.location.origin}/other`),
  ).toBeUndefined();
  expect(localNavigationHref("javascript:alert(1)")).toBeUndefined();
  document.head.innerHTML = "";
});

it("accepts real scene projections while rejecting invalid shared contracts", () => {
  expectTypeOf<GQL.SceneDataFragment>().toExtend<DownloadableScene>();
  expectTypeOf<GQL.SlimSceneDataFragment>().toExtend<DownloadableScene>();
  expectTypeOf<
    ComponentProps<typeof ImageListItems.Provider>["items"]
  >().not.toExtend<ComponentProps<typeof SceneListItems.Provider>["items"]>();
  const cache = new InMemoryCache();
  function invalidContracts(
    scene: GQL.SceneDataFragment,
    images: GQL.FindImagesQuery["findImages"]["images"],
    config: GQL.ConfigDataFragment,
  ) {
    const route: EntityDestination = {
      to: "/scenes/$sceneId",
      // @ts-expect-error Route params must belong to the destination.
      params: { imageId: "1" },
    };
    // @ts-expect-error Configuration key and value must agree.
    writeConfigKey(cache, "general", config.interface);
    // @ts-expect-error An image provider cannot supply a scene action's files.
    const provider = <SceneListItems.Provider items={images} />;
    // @ts-expect-error Downloads require a file projection, not just an ID.
    sceneDownloadSnapshot({ id: scene.id });
    return { route, provider };
  }
  expectTypeOf(invalidContracts).toBeFunction();
  expect(
    sceneDownloadSnapshot({ id: "1", files: [], paths: {} }),
  ).toBeUndefined();
  expect(
    sceneDownloadSnapshot({
      id: "1",
      title: "Scene",
      files: [{ path: "/library/scene.mp4", width: 1920, height: 1080 }],
      paths: {},
    }),
  ).toMatchObject({
    scene_id: "1",
    title: "Scene",
    source_file_path: "/library/scene.mp4",
    performers: [],
    width: 1920,
  });
});
