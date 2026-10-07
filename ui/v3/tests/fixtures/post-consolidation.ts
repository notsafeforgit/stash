import protocol from "./post-consolidation-protocol.json" with { type: "json" };
import {
  postMergeInputSchema,
  postMergePreviewSchema,
  postMergeReceiptSchema,
  type PostMergeApply,
} from "../../src/core/native-archive/post-consolidation-schema";
import type { MergeNotification } from "../../src/core/native-archive/post-consolidation-api";

// Synthetic responses captured from the native Go preview/apply route fixture.
export const mergeInput = () => postMergeInputSchema.parse(protocol.input);
export const mergePreview = () =>
  postMergePreviewSchema.parse(protocol.preview);
export function mergeReceipt(input?: PostMergeApply) {
  const receipt = postMergeReceiptSchema.parse(protocol.receipt);
  if (input) {
    receipt.request = input;
    Object.assign(receipt.result.consolidation, {
      uuid: input.request_uuid,
      source_uuid: input.source_uuid,
      destination_uuid: input.destination_uuid,
      review_signature: input.digest,
      reason: input.reason,
    });
  }
  return postMergeReceiptSchema.parse(receipt);
}
export function mergeNotification(): MergeNotification {
  const receipt = mergeReceipt();
  return {
    sequence: 1,
    job_uuid: receipt.result.notification_job_uuid!,
    review_uuid: receipt.request.request_uuid,
    state: "cancelled",
    revision: 2,
    resume_from_job_uuid: "",
    resume_from_job_revision: 0,
    hooks_finished: false,
    error_code: "",
    created_at: "2026-10-06T12:00:00Z",
    updated_at: "2026-10-06T12:01:00Z",
  };
}

export function mergeConflictPreview() {
  const preview = mergePreview();
  const source = preview.posts.find(
    (post) => post.uuid === preview.source.uuid,
  )!;
  const target = preview.posts.find(
    (post) => post.uuid === preview.destination.uuid,
  )!;
  const id = (n: number) =>
    `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`;
  const time = "2026-10-06T12:00:00Z";
  const media = {
    uuid: id(101),
    revision: 1,
    kind: "scene" as const,
    state: "active" as const,
    local_id: 7,
  };
  source.selection = { ...target.selection!, decision_uuid: id(102) };
  source.attachments = structuredClone(target.attachments);
  source.attachments[0]!.uuid = id(103);
  source.selection.entries[0]!.attachment_uuid = id(103);
  for (const [index, post] of [source, target].entries()) {
    const item = post.attachments[0]!;
    item.choice = {
      uuid: id(110 + index),
      attachment_uuid: item.uuid,
      revision: 1,
      state: index ? "linked" : "unlinked",
      media_uuid: index ? media.uuid : null,
      origin: "review",
      reason: "Reviewed link",
      created_at: time,
    };
    item.media = index ? media : null;
    post.media_choices = [
      {
        media,
        decision: {
          uuid: id(120 + index),
          post_uuid: post.uuid,
          post_revision: post.revision,
          media_uuid: media.uuid,
          media_revision: 1,
          state: index ? "linked" : "unlinked",
          origin: "review",
          reason: "Reviewed post link",
          created_at: time,
        },
      },
    ];
    const gallery = {
      ...media,
      uuid: id(130 + index),
      kind: "gallery" as const,
      local_id: 12 + index,
      title: `Existing gallery ${index + 1}`,
      title_truncated: false,
    };
    post.album = {
      post_uuid: post.uuid,
      decision_uuid: id(140 + index),
      revision: 1,
      state: "linked",
      gallery_uuid: gallery.uuid,
      gallery,
      selection_uuid: post.selection?.decision_uuid ?? null,
      origin: "review",
      reason: "Reviewed gallery",
      created_at: time,
    };
  }
  preview.ready = false;
  preview.selection = null;
  preview.gallery = null;
  preview.album = null;
  preview.blockers = [
    { kind: "source_list_choice" },
    { kind: "gallery_choice" },
    { kind: "media_choice", media_uuid: media.uuid },
    { kind: "attachment_choice", namespace: "native:reddit", value: "first" },
  ];
  return postMergePreviewSchema.parse(preview);
}
