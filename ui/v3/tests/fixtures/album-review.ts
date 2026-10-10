import type {
  AlbumJob,
  AlbumPreview,
} from "../../src/core/native-archive/album-review-api";

export const albumReviewID = (value: number) =>
  `35000000-0000-4000-8000-${String(value).padStart(12, "0")}`;
export const albumReviewPost = albumReviewID(1);
export function albumPreview(): AlbumPreview {
  return {
    post_uuid: albumReviewPost,
    policy: "source-identifiers-v1",
    signature: "a".repeat(64),
    action: "create",
    selection_uuid: albumReviewID(2),
    initial_metadata: {
      title: "A day at the coast",
      details: "Three source positions",
      date: "2026-09-01",
    },
    entries: [
      {
        position: 0,
        attachment_uuid: albumReviewID(3),
        media_uuid: albumReviewID(4),
        media_kind: "image",
        media_revision: 1,
        status: "linked",
      },
      { position: 1, attachment_uuid: albumReviewID(5), status: "unselected" },
      {
        position: 2,
        attachment_uuid: albumReviewID(6),
        media_uuid: albumReviewID(7),
        media_kind: "scene",
        media_revision: 2,
        status: "linked",
      },
    ],
    add: [
      {
        uuid: albumReviewID(4),
        kind: "image",
        state: "active",
        revision: 1,
        local_id: 21,
      },
      {
        uuid: albumReviewID(7),
        kind: "scene",
        state: "active",
        revision: 2,
        local_id: 31,
      },
    ],
    remove: [],
    matches: [
      {
        attachment_uuid: albumReviewID(3),
        attachment_revision: 1,
        reference: { namespace: "native:reddit", value: "photo-a" },
        status: "matched",
        candidates: [
          {
            media_uuid: albumReviewID(4),
            media_revision: 1,
            media_kind: "image",
            proofs: [
              {
                evidence_uuid: albumReviewID(8),
                basis: "source-id",
                status: "valid",
                post_file_uuid: albumReviewID(9),
                match_uuid: albumReviewID(10),
                file_uuid: albumReviewID(11),
                generation: 1,
                relative_path: "Coast/photo-a.jpg",
              },
            ],
          },
        ],
      },
      {
        attachment_uuid: albumReviewID(5),
        attachment_revision: 1,
        reference: { namespace: "native:reddit", value: "photo-b" },
        status: "unavailable",
        candidates: [],
      },
      {
        attachment_uuid: albumReviewID(6),
        attachment_revision: 1,
        reference: { namespace: "native:reddit", value: "video-c" },
        decision_uuid: albumReviewID(12),
        status: "preserved",
        candidates: [],
      },
    ],
  };
}
export function albumJob(overrides: Partial<AlbumJob> = {}): AlbumJob {
  return {
    job_uuid: albumReviewID(20),
    sequence: 7,
    post_uuid: albumReviewPost,
    policy: "source-identifiers-v1",
    signature: "a".repeat(64),
    state: "queued",
    revision: 1,
    attempts: 0,
    failures: 0,
    max_attempts: 10,
    available_at: "2026-10-06T12:00:00Z",
    publication_committed: false,
    hooks_finished: false,
    created_at: "2026-10-06T12:00:00Z",
    updated_at: "2026-10-06T12:00:00Z",
    ...overrides,
  };
}
export function publishedAlbumJob(overrides: Partial<AlbumJob> = {}): AlbumJob {
  return albumJob({
    state: "queued",
    revision: 4,
    attempts: 1,
    publication_committed: true,
    error_code: "plugin_notification_failed",
    publication: {
      event_uuid: albumReviewID(20),
      post_uuid: albumReviewPost,
      gallery_uuid: albumReviewID(30),
      action: "create",
      created: true,
      selected: 1,
      review: 0,
      unavailable: 1,
      added: 2,
      removed: 0,
    },
    ...overrides,
  });
}
