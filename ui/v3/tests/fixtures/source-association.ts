import type {
  GalleryAssociationPreview,
  GalleryAssociationApply,
  GalleryAssociationReceipt,
  AttachmentMediaPreview,
  AttachmentMediaApply,
  AttachmentMediaReceipt,
} from "../../src/core/native-archive/association-review-api";

export const ids = {
  post: "11111111-1111-4111-8111-111111111111",
  otherPost: "11111111-1111-4111-8111-111111111112",
  gallery: "22222222-2222-4222-8222-222222222222",
  media: "33333333-3333-4333-8333-333333333333",
  attachment: "44444444-4444-4444-8444-444444444444",
  request: "55555555-5555-4555-8555-555555555555",
  decision: "66666666-6666-4666-8666-666666666666",
};
export function galleryPreview(): GalleryAssociationPreview {
  return {
    input: {
      post_uuid: ids.post,
      post_revision: 3,
      state: "linked",
      gallery_uuid: ids.gallery,
      gallery_revision: 2,
    },
    current: null,
    proposed: {
      uuid: ids.gallery,
      revision: 2,
      kind: "gallery",
      state: "active",
      local_id: 42,
      title: "Existing album",
      title_truncated: false,
    },
    changed: true,
    digest: "a".repeat(64),
  };
}
export function mediaPreview(): AttachmentMediaPreview {
  return {
    input: {
      post_uuid: ids.post,
      post_revision: 3,
      attachment_uuid: ids.attachment,
      attachment_revision: 2,
      state: "linked",
      media_uuid: ids.media,
      media_revision: 4,
    },
    current: {
      post_uuid: ids.post,
      post_revision: 3,
      post_state: "active",
      attachment: {
        uuid: ids.attachment,
        revision: 2,
        reference: { namespace: "native:twitter", value: "animation" },
      },
      current: null,
      media: null,
      post_link_state: "",
      source_media_kinds: ["image"],
    },
    proposed: {
      uuid: ids.media,
      revision: 4,
      kind: "scene",
      state: "active",
      local_id: 31,
      title: "Converted animation",
      title_truncated: false,
    },
    changed: true,
    digest: "b".repeat(64),
  };
}
export function galleryReceipt(
  request: GalleryAssociationApply,
): GalleryAssociationReceipt {
  return {
    request_uuid: request.request_uuid,
    decision_uuid: ids.decision,
    request,
    created_at: "2026-10-06T15:00:00Z",
  };
}
export function mediaReceipt(
  request: AttachmentMediaApply,
): AttachmentMediaReceipt {
  return {
    request_uuid: request.request_uuid,
    decision_uuid: ids.decision,
    request,
    created_at: "2026-10-06T15:00:00Z",
  };
}
