import { useState } from "react";
import { MockedProvider } from "@apollo/client/testing/react";
import type { MockedResponse } from "@apollo/client/testing";
import { MarkerEditForm } from "@/components/detail/marker-edit-form";
import * as GQL from "@/core/generated-graphql";
import { markerTitle } from "@/core/markers";
import { scenes } from "./scene-lightbox";

declare global {
  interface Window {
    markerFixtureSaves: {
      operation: "create" | "update";
      variables:
        | GQL.SceneMarkerCreateMutationVariables
        | GQL.SceneMarkerUpdateMutationVariables;
    }[];
  }
}
window.markerFixtureSaves = [];
const original = scenes[0]?.scene_markers[0];
if (!original) throw new Error("Missing synthetic marker");
let savedMarker: GQL.SceneMarkerDataFragment = original;
const tag: GQL.TagDataFragment = {
  __typename: "Tag",
  id: original.primary_tag.id,
  name: original.primary_tag.name,
  sort_name: null,
  description: null,
  aliases: [],
  ignore_auto_tag: false,
  favorite: false,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  stash_ids: [],
  image_path: "",
  scene_count: 0,
  scene_count_all: 0,
  scene_marker_count: 0,
  scene_marker_count_all: 0,
  image_count: 0,
  image_count_all: 0,
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
};
const tags: MockedResponse<GQL.FindTagsQuery, GQL.FindTagsQueryVariables> = {
  request: { query: GQL.FindTagsDocument, variables: () => true },
  delay: 0,
  maxUsageCount: Infinity,
  result: { data: { findTags: { count: 1, tags: [tag] } } },
};
function save(variables: GQL.SceneMarkerCreateMutationVariables) {
  savedMarker = {
    ...savedMarker,
    title: variables.title,
    seconds: variables.seconds,
    end_seconds: variables.end_seconds ?? null,
  };
  return savedMarker;
}
const create: MockedResponse<
  GQL.SceneMarkerCreateMutation,
  GQL.SceneMarkerCreateMutationVariables
> = {
  request: { query: GQL.SceneMarkerCreateDocument, variables: () => true },
  delay: 0,
  result: (variables) => {
    window.markerFixtureSaves.push({ operation: "create", variables });
    return { data: { sceneMarkerCreate: save(variables) } };
  },
};
const update: MockedResponse<
  GQL.SceneMarkerUpdateMutation,
  GQL.SceneMarkerUpdateMutationVariables
> = {
  request: { query: GQL.SceneMarkerUpdateDocument, variables: () => true },
  delay: 0,
  result: (variables) => {
    window.markerFixtureSaves.push({ operation: "update", variables });
    return { data: { sceneMarkerUpdate: save(variables) } };
  },
};

export function MarkerEditorFixture() {
  const [saved, setSaved] = useState(false);
  const editing = new URLSearchParams(location.search).has("edit");
  return (
    <MockedProvider mocks={[tags, create, update]}>
      <div className="mx-auto max-w-lg p-3">
        {saved ? (
          <p data-testid="saved-marker-title">{markerTitle(savedMarker)}</p>
        ) : (
          <MarkerEditForm
            sceneId={savedMarker.scene.id}
            marker={editing ? savedMarker : null}
            maxTimestamp={12}
            onSaved={() => setSaved(true)}
          />
        )}
      </div>
    </MockedProvider>
  );
}
