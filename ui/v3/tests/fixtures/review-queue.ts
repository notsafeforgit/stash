import type {
  ReviewQueuePage,
  ReviewQueueKind,
} from "../../src/core/native-archive/review-queue-api";
import type { Collection } from "../../src/core/native-archive/collection-api";
import { account } from "./account-review";
import { postSummary } from "./source-posts";

export function heldSource(): Collection {
  return {
    uuid: "6f9d3a52-8a51-4c43-9c39-1c0cbb1f1d2e",
    label: "Redgifs / reposted (held: repost limit)",
    kind: "account",
    namespace: "native:redgifs",
    state: "active",
    target_url: "https://www.redgifs.com/users/reposted",
    account_uuid: null,
    root_uuid: "38a5e0a8-6a4f-4c6e-9d43-5d1c2b8f7e10",
    path_prefix: "Example, reddit",
    revision: 1,
    created_at: "2026-11-20T03:32:46Z",
  };
}

export function reviewQueuePage(kind: ReviewQueueKind): ReviewQueuePage {
  const owner = account(),
    post = postSummary();
  if (post.latest_capture) post.latest_capture.title = "Shared album post";
  return {
    kind,
    checked: 1,
    items:
      kind === "accounts"
        ? [
            {
              uuid: owner.uuid,
              account: owner,
              reasons: ["account_owner_undecided"],
            },
          ]
        : kind === "media"
          ? [
              {
                uuid: post.uuid,
                post,
                reasons: ["post_link_conflict", "attachment_link_conflict"],
              },
            ]
          : [
              {
                uuid: heldSource().uuid,
                collection: heldSource(),
                reasons: ["source_repost_hold"],
              },
            ],
  };
}
