import type {
  SourcePost,
  SourceLinkInput,
  SourceDecision,
} from "../../src/core/native-archive/source-review-api";

export const sourceIds = {
  post: "00000000-0000-4000-8000-000000000001",
  media: "00000000-0000-4000-8000-000000000002",
  capture: "00000000-0000-4000-8000-000000000003",
  revision: "00000000-0000-4000-8000-000000000004",
  url: "00000000-0000-4000-8000-000000000005",
  decision: "00000000-0000-4000-8000-000000000006",
  secondCapture: "00000000-0000-4000-8000-000000000007",
};
export function sourcePost(): SourcePost {
  return {
    requested_post_uuid: sourceIds.post,
    association: {
      post_uuid: sourceIds.post,
      post_revision: 4,
      post_state: "active",
      media_uuid: sourceIds.media,
      media_revision: 2,
      media_state: "active",
      state: "undecided",
      decisions: [],
    },
    latest_capture: {
      uuid: sourceIds.capture,
      post_uuid: sourceIds.post,
      revision_uuid: sourceIds.revision,
      origin: "gallery-dl",
      platform: "reddit",
      captured_at: "2026-09-30T08:00:00Z",
      recorded_at: null,
      title: "Shared album caption",
      title_truncated: false,
      published_at: "2026-09-29",
      date_basis: "source",
    },
    urls: [
      {
        uuid: sourceIds.url,
        post_uuid: sourceIds.post,
        url: "https://www.reddit.com/gallery/example",
      },
    ],
    more_urls: false,
    has_retained_evidence: true,
    linked_attachments: 0,
  };
}
export function sourceReceipt(input: SourceLinkInput): SourceDecision {
  return {
    uuid: input.uuid,
    post_uuid: input.post_uuid,
    media_uuid: input.media_uuid,
    post_revision: input.expected_post_revision + 1,
    media_revision: input.expected_media_revision,
    state: input.state,
    reason: input.reason,
    origin: input.origin,
    created_at: "2026-10-05T19:00:00Z",
  };
}
