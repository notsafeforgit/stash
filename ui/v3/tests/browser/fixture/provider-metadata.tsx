import { MockedProvider } from "@apollo/client/testing/react";
import type { MockedResponse } from "@apollo/client/testing";
import * as GQL from "@/core/generated-graphql";
import { SceneEditForm } from "@/components/detail/scene-edit-form";
import { PerformerEditForm } from "@/components/detail/performer-edit-form";
import { ProviderMetadataHistory } from "@/components/detail/provider-metadata-history";
import { ConfigurationProvider } from "@/hooks/config";
import { Toaster } from "@/components/ui/sonner";
import { playerConfiguration } from "./player-configuration";
import { scenes } from "./scene-lightbox";
import { performers } from "./merge-dialogs";

declare global {
  interface Window {
    providerFixture: {
      scenes: GQL.SceneUpdateInput[];
      performers: GQL.PerformerUpdateInput[];
      tags: GQL.TagCreateInput[];
    };
  }
}
window.providerFixture = { scenes: [], performers: [], tags: [] };
const provider = {
  __typename: "StashBox" as const,
  name: "Fixture provider",
  endpoint: "https://provider.test/graphql",
  api_key: "",
  max_requests_per_minute: 60,
};
const configuration = {
  ...playerConfiguration,
  general: { ...playerConfiguration.general, stashBoxes: [provider] },
};
const scrapedTag: GQL.ScrapedSceneTagDataFragment = {
  __typename: "ScrapedTag",
  name: "Provider tag",
  stored_id: null,
  remote_site_id: "tag-9",
  description: null,
  alias_list: null,
  parent: null,
};
const scrapedScene: GQL.ScrapedSceneDataFragment = {
  __typename: "ScrapedScene",
  title: "Imported scene",
  code: "PROVIDER-7",
  details: null,
  director: null,
  urls: [],
  date: null,
  production_date: null,
  image: null,
  remote_site_id: "scene-7",
  file: null,
  studio: null,
  tags: [scrapedTag],
  performers: [],
  groups: [],
  fingerprints: [],
};
const scrapedPerformer: GQL.ScrapedPerformerDataFragment = {
  __typename: "ScrapedPerformer",
  stored_id: null,
  name: "Imported performer",
  disambiguation: null,
  gender: null,
  urls: [],
  birthdate: "1990",
  ethnicity: null,
  country: null,
  eye_color: null,
  height: "168",
  measurements: null,
  fake_tits: null,
  penis_length: null,
  circumcised: null,
  career_start: null,
  career_end: null,
  tattoos: null,
  piercings: null,
  aliases: null,
  tags: [],
  images: [],
  details: null,
  death_date: null,
  hair_color: null,
  weight: null,
  remote_site_id: "performer-7",
};
const tag: GQL.TagDataFragment = {
  __typename: "Tag",
  id: "9",
  name: "Provider tag",
  sort_name: null,
  description: null,
  aliases: [],
  ignore_auto_tag: false,
  favorite: false,
  stash_ids: [],
  image_path: null,
  scene_count: 0,
  scene_count_all: 0,
  image_count: 0,
  image_count_all: 0,
  scene_marker_count: 0,
  scene_marker_count_all: 0,
  gallery_count: 0,
  gallery_count_all: 0,
  performer_count: 0,
  performer_count_all: 0,
  studio_count: 0,
  studio_count_all: 0,
  group_count: 0,
  group_count_all: 0,
  parents: [],
  children: [],
  custom_fields: {},
  created_at: "2026-10-08T00:00:00Z",
  updated_at: "2026-10-08T00:00:00Z",
};
function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Missing provider fixture entity");
  return value;
}
const scene = required(scenes[0]);
const performer = required(performers[0]);
const sceneSave: MockedResponse<
  GQL.SceneUpdateMutation,
  GQL.SceneUpdateMutationVariables
> = {
  request: { query: GQL.SceneUpdateDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: ({ input }) => {
    window.providerFixture.scenes.push(input);
    return { data: { sceneUpdate: { ...scene, title: input.title ?? null } } };
  },
};
const performerSave: MockedResponse<
  GQL.PerformerUpdateMutation,
  GQL.PerformerUpdateMutationVariables
> = {
  request: { query: GQL.PerformerUpdateDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: ({ input }) => {
    window.providerFixture.performers.push(input);
    return {
      data: {
        performerUpdate: { ...performer, name: input.name ?? performer.name },
      },
    };
  },
};
const tagCreate: MockedResponse<
  GQL.TagCreateMutation,
  GQL.TagCreateMutationVariables
> = {
  request: { query: GQL.TagCreateDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: ({ input }) => {
    window.providerFixture.tags.push(input);
    return { data: { tagCreate: tag } };
  },
};
const mocks = [
  sceneSave,
  performerSave,
  tagCreate,
  {
    request: { query: GQL.ConfigurationDocument },
    result: { data: { configuration } },
    maxUsageCount: Infinity,
    delay: 0,
  },
  ...[GQL.ListSceneScrapersDocument, GQL.ListPerformerScrapersDocument].map(
    (query) => ({
      request: { query },
      result: { data: { listScrapers: [] } },
      maxUsageCount: Infinity,
      delay: 0,
    }),
  ),
  {
    request: { query: GQL.ScrapeSingleSceneDocument, variables: () => true },
    result: { data: { scrapeSingleScene: [scrapedScene] } },
    maxUsageCount: Infinity,
    delay: 0,
  },
  {
    request: {
      query: GQL.ScrapeSinglePerformerDocument,
      variables: () => true,
    },
    result: { data: { scrapeSinglePerformer: [scrapedPerformer] } },
    maxUsageCount: Infinity,
    delay: 0,
  },
];

export function ProviderMetadataFixture() {
  const params = new URLSearchParams(location.search);
  if (params.has("history"))
    return (
      <div className="p-4">
        <section aria-label="Scene import history">
          <ProviderMetadataHistory kind="scene" localId="1" />
        </section>
        <section aria-label="Retired performer import history">
          <ProviderMetadataHistory
            kind="performer"
            entityUUID="33333333-3333-4333-8333-333333333333"
          />
        </section>
      </div>
    );
  return (
    <MockedProvider mocks={mocks}>
      <ConfigurationProvider configuration={configuration}>
        <div className="h-dvh">
          {params.has("performer") ? (
            <PerformerEditForm performer={performer} />
          ) : (
            <SceneEditForm scene={scene} />
          )}
        </div>
        <Toaster />
      </ConfigurationProvider>
    </MockedProvider>
  );
}
