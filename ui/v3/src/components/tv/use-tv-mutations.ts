import { useRef, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import * as GQL from "@/core/generated-graphql";
import type { TvFeedItem } from "@/core/tv/feed-state";
import type { TvAction } from "@/core/tv/action-config";
import type { DeleteOptions } from "@/components/detail/delete-dialog";
import { useToast } from "@/hooks/toast";

export type TvScene = NonNullable<GQL.FindSceneQuery["findScene"]>;
export function useTvMutations(onChanged: (deleted?: TvFeedItem) => void) {
  const client = useApolloClient();
  const report = useToast().error;
  const pending = useRef(new Set<string>());
  const [busy, setBusy] = useState(false);
  async function run(target: string, work: () => Promise<void>) {
    if (pending.current.has(target)) return false;
    pending.current.add(target);
    setBusy(true);
    try {
      await work();
      return true;
    } catch (error) {
      report(error);
      return false;
    } finally {
      pending.current.delete(target);
      setBusy(pending.current.size > 0);
    }
  }
  async function refresh(sceneId: string) {
    try {
      await client.query({
        query: GQL.FindSceneDocument,
        variables: { id: sceneId },
        fetchPolicy: "network-only",
      });
    } catch (error) {
      // The mutation already succeeded. Report the refresh failure without
      // inviting the form to submit an already-created marker again.
      report(error);
    }
    onChanged();
  }
  async function updateScene(
    sceneId: string,
    input: Omit<GQL.SceneUpdateInput, "id">,
  ) {
    const response = await client.mutate({
      mutation: GQL.SceneUpdateDocument,
      variables: { input: { ...input, id: sceneId } },
    });
    if (!response.data?.sceneUpdate)
      throw new Error("The scene could not be updated");
    onChanged();
  }
  async function updateTags(
    scene: TvScene,
    item: TvFeedItem,
    tags: string[],
    primary?: string,
  ) {
    if (item.kind === "scene") return updateScene(scene.id, { tag_ids: tags });
    const marker = scene.scene_markers.find((marker) => marker.id === item.id);
    if (!marker) throw new Error("This marker is no longer available");
    const result = await client.mutate({
      mutation: GQL.SceneMarkerUpdateDocument,
      variables: {
        id: marker.id,
        scene_id: scene.id,
        title: marker.title,
        seconds: marker.seconds,
        end_seconds: marker.end_seconds,
        primary_tag_id: primary ?? marker.primary_tag.id,
        tag_ids: tags,
      },
    });
    if (!result.data?.sceneMarkerUpdate)
      throw new Error("The marker could not be updated");
    await refresh(scene.id);
  }
  async function quickTag(
    scene: TvScene,
    item: TvFeedItem,
    action: Extract<TvAction, { kind: "quick-tag" }>,
  ) {
    const marker =
      item.kind === "marker"
        ? scene.scene_markers.find((marker) => marker.id === item.id)
        : undefined;
    if (item.kind === "marker" && !marker)
      throw new Error("This marker is no longer available");
    const ids = (marker?.tags ?? scene.tags).map((tag) => tag.id);
    if (marker && action.target === "primary")
      return updateTags(
        scene,
        item,
        ids.filter((id) => id !== action.tagId),
        action.tagId,
      );
    const next = ids.includes(action.tagId)
      ? ids.filter((id) => id !== action.tagId)
      : [...ids, action.tagId];
    return updateTags(scene, item, next);
  }
  async function quickMarker(
    scene: TvScene,
    position: number,
    action: Extract<TvAction, { kind: "quick-marker" }>,
  ) {
    const duration = scene.files[0]?.duration ?? 0;
    if (!Number.isFinite(position) || position < 0 || position >= duration)
      throw new Error("Choose a valid scene position first");
    const result = await client.mutate({
      mutation: GQL.SceneMarkerCreateDocument,
      variables: {
        scene_id: scene.id,
        title: action.title,
        seconds: position,
        end_seconds:
          action.duration === null
            ? null
            : Math.min(duration, position + action.duration),
        primary_tag_id: action.primaryTagId,
        tag_ids: action.tagIds,
      },
    });
    if (!result.data?.sceneMarkerCreate)
      throw new Error("The marker could not be created");
    await refresh(scene.id);
  }
  async function counter(
    sceneId: string,
    operation: "add" | "subtract" | "reset",
  ) {
    if (operation === "reset") {
      const result = await client.mutate({
        mutation: GQL.SceneResetODocument,
        variables: { id: sceneId },
      });
      if (result.data?.sceneResetO == null)
        throw new Error("The counter could not be reset");
      client.cache.modify({
        id: client.cache.identify({ __typename: "Scene", id: sceneId }),
        fields: { o_counter: () => 0, o_history: () => [] },
      });
    } else {
      const result =
        operation === "add"
          ? await client
              .mutate({
                mutation: GQL.SceneAddODocument,
                variables: { id: sceneId },
              })
              .then((result) => result.data?.sceneAddO)
          : await client
              .mutate({
                mutation: GQL.SceneDeleteODocument,
                variables: { id: sceneId },
              })
              .then((result) => result.data?.sceneDeleteO);
      if (!result) throw new Error("The counter could not be updated");
      client.cache.modify({
        id: client.cache.identify({ __typename: "Scene", id: sceneId }),
        fields: {
          o_counter: () => result.count,
          o_history: () => result.history,
        },
      });
    }
    onChanged();
  }
  async function destroy(item: TvFeedItem, options: DeleteOptions) {
    if (item.kind === "scene") {
      const result = await client.mutate({
        mutation: GQL.SceneDestroyDocument,
        variables: {
          id: item.id,
          delete_file: options.deleteFile,
          delete_generated: options.deleteGenerated,
        },
      });
      if (!result.data?.sceneDestroy)
        throw new Error("The scene could not be deleted");
    } else {
      const result = await client.mutate({
        mutation: GQL.SceneMarkerDestroyDocument,
        variables: { id: item.id },
      });
      if (!result.data?.sceneMarkerDestroy)
        throw new Error("The marker could not be deleted");
    }
    // Tombstone before late pages can be admitted; evict only this identity.
    onChanged(item);
    client.cache.evict({
      id: client.cache.identify({
        __typename: item.kind === "scene" ? "Scene" : "SceneMarker",
        id: item.id,
      }),
    });
  }
  return {
    busy,
    run,
    updateScene,
    updateTags,
    quickTag,
    quickMarker,
    counter,
    destroy,
    refresh,
  };
}
