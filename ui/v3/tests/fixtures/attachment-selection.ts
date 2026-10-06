import type {
  SelectionApply,
  SelectionPreview,
  SelectionReceipt,
} from "../../src/core/native-archive/attachment-selection-api";

export const ids = {
  post: "11111111-1111-4111-8111-111111111111",
  otherPost: "11111111-1111-4111-8111-111111111112",
  capture: "22222222-2222-4222-8222-222222222222",
  manifest: "33333333-3333-4333-8333-333333333333",
  attachment: "44444444-4444-4444-8444-444444444444",
  request: "55555555-5555-4555-8555-555555555555",
  decision: "66666666-6666-4666-8666-666666666666",
};
export function preview(disabled = false): SelectionPreview {
  return {
    input: {
      post_uuid: ids.post,
      post_revision: 3,
      mode: disabled ? "disabled" : "pinned",
      ...(disabled ? {} : { capture_uuid: ids.capture }),
    },
    current: null,
    proposed: {
      mode: disabled ? "disabled" : "pinned",
      origin: "review",
      reason: "",
      ...(disabled ? {} : { capture_uuid: ids.capture }),
      manifest_uuids: disabled ? [] : [ids.manifest],
      complete: false,
      declared_album: !disabled,
      entries: disabled
        ? []
        : [0, 5].map((position) => ({
            position,
            attachment_uuid: ids.attachment,
            reference: { namespace: "native:reddit", value: "original" },
            media_kind: "image",
          })),
    },
    changed: true,
    digest: "a".repeat(64),
  };
}
export function receipt(request: SelectionApply): SelectionReceipt {
  return {
    request_uuid: request.request_uuid,
    request,
    decision_uuid: ids.decision,
    created_at: "2026-10-06T14:00:00Z",
  };
}
