import { useState } from "react";
import { MockedProvider } from "@apollo/client/testing/react";
import { PerformerMergeDialog } from "@/components/detail/performer-merge-dialog";
import { SceneMergeDialog } from "@/components/detail/scene-merge-dialog";
import * as GQL from "@/core/generated-graphql";

const longValue = "LongUnbrokenName".repeat(8);
const performers: GQL.PerformerDataFragment[] = ["1", "2", "3", "4"].map(
  (id) => ({
    __typename: "Performer",
    id,
    name: id === "1" ? "Destination" : `Source ${id} ${longValue}`,
    disambiguation: null,
    urls: [`https://example.com/${longValue}/${id}`],
    gender: null,
    birthdate: null,
    death_date: null,
    ethnicity: null,
    country: null,
    eye_color: null,
    hair_color: null,
    height_cm: null,
    weight: null,
    measurements: null,
    fake_tits: null,
    penis_length: null,
    circumcised: null,
    career_start: null,
    career_end: null,
    tattoos: null,
    piercings: null,
    rating100: null,
    favorite: false,
    ignore_auto_tag: false,
    ignore_primary_name_auto_tag: false,
    image_path: null,
    scene_count: 0,
    image_count: 0,
    gallery_count: 0,
    group_count: 0,
    performer_count: 0,
    o_counter: 0,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    details: `${id}: ${"Details to resolve. ".repeat(30)}`,
    aliases: [
      {
        __typename: "PerformerAlias",
        alias: `${longValue}${id}`,
        ignore_auto_tag: false,
      },
    ],
    custom_fields: {},
    stash_ids: [],
    tags: [],
  }),
);
const scenes: GQL.SlimSceneDataFragment[] = performers.map((performer) => ({
  __typename: "Scene",
  id: performer.id,
  title: performer.name,
  code: null,
  details: performer.details,
  director: null,
  urls: performer.urls ?? [],
  date: null,
  rating100: null,
  o_counter: 0,
  organized: false,
  interactive: false,
  interactive_speed: null,
  resume_time: 0,
  play_duration: 0,
  play_count: 0,
  preview_image: null,
  files: [],
  paths: {
    __typename: "ScenePathsType",
    screenshot: null,
    preview: null,
    webp: null,
    vtt: null,
    interactive_heatmap: null,
  },
  scene_markers: [],
  galleries: [],
  studio: null,
  groups: [],
  tags: [
    {
      __typename: "Tag",
      id: performer.id,
      name: `${longValue}${performer.id}`,
    },
  ],
  performers: [],
  stash_ids: [],
}));
const performerResult = {
  data: { findPerformers: { count: performers.length, performers } },
};
const sceneResult = {
  data: {
    findScenes: { count: scenes.length, duration: 0, filesize: 0, scenes },
  },
};
const mocks = [
  ...[GQL.FindPerformersForSelectDocument, GQL.FindPerformersDocument].map(
    (query) => ({
      request: { query, variables: () => true },
      result: performerResult,
      maxUsageCount: Infinity,
      delay: 0,
    }),
  ),
  ...[GQL.FindScenesForSelectDocument, GQL.FindScenesDocument].map((query) => ({
    request: { query, variables: () => true },
    result: sceneResult,
    maxUsageCount: Infinity,
    delay: 0,
  })),
];

export function MergeDialogsFixture() {
  const [open, setOpen] = useState(true);
  const params = new URLSearchParams(location.search);
  const end = params.has("bulk") ? 4 : 2;
  return (
    <MockedProvider mocks={mocks}>
      {params.has("scene") ? (
        <SceneMergeDialog
          open={open}
          onOpenChange={setOpen}
          sources={scenes.slice(1, end)}
        />
      ) : (
        <PerformerMergeDialog
          open={open}
          onOpenChange={setOpen}
          sources={performers.slice(1, end)}
        />
      )}
    </MockedProvider>
  );
}
