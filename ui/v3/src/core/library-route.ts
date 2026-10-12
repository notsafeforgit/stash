import { redirect, type ParsedLocation } from "@tanstack/react-router";
import { createMetadataReviewAPI } from "./native-archive/metadata-review-api";
import { NativeArchiveError } from "./native-archive/client";

const routes = {
  scene: ["/scenes/$sceneId", "sceneId"],
  image: ["/images/$imageId", "imageId"],
  gallery: ["/galleries/$galleryId", "galleryId"],
  performer: ["/performers/$performerId", "performerId"],
  studio: ["/studios/$studioId", "studioId"],
  tag: ["/tags/$tagId", "tagId"],
  group: ["/groups/$groupId", "groupId"],
} as const;

/** Resolve old bookmarks and merged UUIDs without ever returning database keys. */
export async function resolveLibraryRoute(
  kind: keyof typeof routes,
  id: string,
  location: Pick<ParsedLocation, "search" | "hash" | "state">,
  signal?: AbortSignal,
) {
  let identity: Awaited<
    ReturnType<ReturnType<typeof createMetadataReviewAPI>["identity"]>
  >;
  try {
    identity = await createMetadataReviewAPI().identity(kind, id, signal);
  } catch (error) {
    if (error instanceof NativeArchiveError && error.status === 404)
      return null;
    throw error;
  }
  if (id !== identity.uuid) {
    const options = {
      search: location.search,
      hash: location.hash,
      state: location.state,
      replace: true,
    };
    const uuid = identity.uuid;
    switch (kind) {
      case "scene":
        throw redirect({
          ...options,
          to: "/scenes/$sceneId",
          params: { sceneId: uuid },
        });
      case "image":
        throw redirect({
          ...options,
          to: "/images/$imageId",
          params: { imageId: uuid },
        });
      case "gallery":
        throw redirect({
          ...options,
          to: "/galleries/$galleryId",
          params: { galleryId: uuid },
        });
      case "performer":
        throw redirect({
          ...options,
          to: "/performers/$performerId",
          params: { performerId: uuid },
        });
      case "studio":
        throw redirect({
          ...options,
          to: "/studios/$studioId",
          params: { studioId: uuid },
        });
      case "tag":
        throw redirect({
          ...options,
          to: "/tags/$tagId",
          params: { tagId: uuid },
        });
      case "group":
        throw redirect({
          ...options,
          to: "/groups/$groupId",
          params: { groupId: uuid },
        });
    }
  }
  return identity.uuid;
}
