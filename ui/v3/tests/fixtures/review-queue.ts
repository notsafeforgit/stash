import type {
  ReviewQueuePage,
  ReviewQueueKind,
} from "../../src/core/native-archive/review-queue-api";
import { account } from "./account-review";
import { postSummary, postMedia } from "./source-posts";

export function reviewQueuePage(kind: ReviewQueueKind): ReviewQueuePage {
  const owner = account(),
    post = postSummary(),
    media = postMedia().media;
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
          : [{ uuid: media.uuid, media, reasons: ["retained_metadata"] }],
  };
}
