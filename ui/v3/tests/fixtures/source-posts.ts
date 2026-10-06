import type {
  PostSummary,
  PostMedia,
  PostAlbum,
} from "../../src/core/native-archive/source-post-api";
import { sourcePost, sourceIds } from "./source-review";

export const postIds = {
  ...sourceIds,
  otherPost: "00000000-0000-4000-8000-000000000011",
  gallery: "00000000-0000-4000-8000-000000000012",
  albumDecision: "00000000-0000-4000-8000-000000000013",
};
export function postSummary(): PostSummary {
  const shared = sourcePost();
  return {
    uuid: postIds.post,
    state: "active",
    revision: 4,
    created_at: "2026-09-29T00:00:00Z",
    latest_capture: shared.latest_capture,
    urls: shared.urls,
    more_urls: false,
    identifiers: [{ namespace: "native:reddit", value: "example" }],
    more_identifiers: false,
  };
}
export function postMedia(): PostMedia {
  return {
    media: {
      uuid: postIds.media,
      kind: "scene",
      state: "active",
      revision: 2,
      local_id: 7,
      title: "Associated library video",
      title_truncated: false,
    },
    association: sourcePost().association,
    has_retained_evidence: true,
    linked_attachments: 0,
  };
}
export function postAlbum(): PostAlbum {
  return {
    post_uuid: postIds.post,
    decision_uuid: postIds.albumDecision,
    revision: 4,
    state: "linked",
    gallery_uuid: postIds.gallery,
    selection_uuid: null,
    origin: "review",
    reason: "Reviewed album",
    created_at: "2026-09-30T00:00:00Z",
    gallery: {
      uuid: postIds.gallery,
      kind: "gallery",
      state: "active",
      revision: 1,
      local_id: 12,
      title: "Shared post album",
      title_truncated: false,
    },
  };
}
