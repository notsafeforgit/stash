import { z } from "zod";
import {
  accountUUIDSchema as uuid,
  ownershipReasonSchema,
} from "./account-review-api";
import { attachmentMediaContextSchema } from "./association-review-api";
import {
  postAlbumSchema,
  postIdentifierSchema,
  postIdentitySchema,
} from "./source-post-api";
import {
  sourceDecisionSchema,
  sourceLinkStateSchema,
  sourcePostSchema,
} from "./source-review-api";

export const postMergeLimit = 8192;
export const postMergeBytes = 4 * 1024 * 1024;
const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const ordinal = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
const digest = z.string().regex(/^[0-9a-f]{64}$/);
const time = z.string().datetime({ offset: true });
const mode = z.enum(["automatic", "pinned", "disabled"]);
const action = z.enum(["create", "sync", "disabled", "ineligible"]);
const unique = <T>(values: T[], key: (value: T) => string) =>
  new Set(values.map(key)).size === values.length;
const ids = z
  .array(uuid)
  .max(postMergeLimit)
  .refine((values) => unique(values, (v) => v));
const referenceKey = (value: { namespace: string; value: string }) =>
  JSON.stringify([value.namespace, value.value]);

export const postMergeMediaChoiceSchema = z
  .object({ media_uuid: uuid, state: sourceLinkStateSchema })
  .strict();
export const postMergeAttachmentChoiceSchema = postIdentifierSchema
  .extend({
    state: sourceLinkStateSchema,
    media_uuid: uuid.optional(),
  })
  .strict()
  .refine(
    (item) => (item.state === "linked") === (item.media_uuid !== undefined),
  );
const selectionChoice = z
  .object({
    mode: z.enum(["choose", "combine", "disabled"]),
    decision_uuid: uuid.optional(),
  })
  .strict()
  .refine(
    (value) =>
      (value.mode === "disabled") === (value.decision_uuid === undefined),
  );
const galleryChoice = z
  .object({
    state: z.enum(["linked", "disabled"]),
    gallery_uuid: uuid.optional(),
  })
  .strict()
  .refine(
    (value) =>
      (value.state === "linked") === (value.gallery_uuid !== undefined),
  );
const inputShape = z
  .object({
    source_uuid: uuid,
    destination_uuid: uuid,
    selection: selectionChoice.optional(),
    gallery: galleryChoice.optional(),
    media: z
      .array(postMergeMediaChoiceSchema)
      .max(postMergeLimit)
      .refine((v) => unique(v, (r) => r.media_uuid)),
    attachments: z
      .array(postMergeAttachmentChoiceSchema)
      .max(postMergeLimit)
      .refine((v) => unique(v, referenceKey)),
    reason: ownershipReasonSchema,
  })
  .strict();
const differentPosts = (value: {
  source_uuid: string;
  destination_uuid: string;
}) => value.source_uuid !== value.destination_uuid;
export const postMergeInputSchema = inputShape.refine(differentPosts);
export const postMergeApplySchema = inputShape
  .extend({ request_uuid: uuid, digest })
  .strict()
  .refine(differentPosts);
// Non-browser API callers can omit empty choice lists. Go retains those as
// null in their original receipts; history must preserve that exact request.
const receiptRequestSchema = inputShape
  .extend({
    request_uuid: uuid,
    digest,
    media: inputShape.shape.media.nullable(),
    attachments: inputShape.shape.attachments.nullable(),
  })
  .strict()
  .refine(differentPosts);
export type PostMergeInput = z.infer<typeof postMergeInputSchema>;
export type PostMergeApply = z.infer<typeof postMergeApplySchema>;

export const postMergeEntitySchema = z
  .object({
    uuid,
    kind: z.enum(["scene", "image", "gallery"]),
    state: z.enum(["active", "deleted"]),
    revision,
    local_id: revision.nullable(),
  })
  .refine((value) => (value.state === "active") === (value.local_id !== null));
const media = postMergeEntitySchema.refine((value) => value.kind !== "gallery");
const selection = z.object({
  decision_uuid: uuid,
  mode,
  origin: z.string(),
  capture_uuid: uuid.nullable(),
  complete: z.boolean(),
  declared_album: z.boolean(),
  expected_count: ordinal.nullable(),
  entries: z
    .array(
      z.object({
        position: ordinal,
        attachment_uuid: uuid,
        media_kind: z.enum(["image", "video", "unknown"]),
      }),
    )
    .max(postMergeLimit),
});
export const postMergePostSchema = z.object({
  uuid,
  state: z.enum(["active", "forgotten"]),
  revision,
  identifiers: z.array(postIdentifierSchema).max(4096),
  urls: z.array(z.string()).max(postMergeLimit),
  latest_capture: sourcePostSchema.shape.latest_capture,
  selection: selection.nullable(),
  album: postAlbumSchema.nullable(),
  attachments: z
    .array(
      postIdentifierSchema.extend({
        uuid,
        revision,
        choice: attachmentMediaContextSchema.shape.current,
        media: media.nullable(),
      }),
    )
    .max(postMergeLimit),
  media_choices: z
    .array(z.object({ decision: sourceDecisionSchema, media }))
    .max(postMergeLimit),
});
export const postMergeBlockerSchema = z.object({
  kind: z.enum([
    "source_list_choice",
    "source_list_conflict",
    "gallery_choice",
    "gallery_unavailable",
    "gallery_requires_review",
    "gallery_claimed",
    "media_choice",
    "media_unavailable",
    "attachment_choice",
    "attachment_post_unlink",
  ]),
  namespace: z.string().optional(),
  value: z.string().optional(),
  media_uuid: uuid.optional(),
});
export const postMergePreviewSchema = z
  .object({
    source: postIdentitySchema,
    destination: postIdentitySchema,
    posts: z.array(postMergePostSchema).min(2).max(256),
    selection: z
      .object({
        mode,
        capture_uuid: uuid.nullable(),
        manifest_uuids: ids,
        entry_count: ordinal.max(postMergeLimit),
        complete: z.boolean(),
        declared_album: z.boolean(),
        expected_count: ordinal.nullable(),
      })
      .nullable(),
    gallery: galleryChoice.nullable(),
    album: z
      .object({
        action: z.enum(["create", "sync", "disabled", "ineligible", "review"]),
        gallery_uuid: uuid.optional(),
        title: z.string().optional(),
        details: z.string().optional(),
        date: z.string().optional(),
        entries: z
          .array(
            z.object({
              position: ordinal,
              namespace: z.string(),
              value: z.string(),
              media_uuid: uuid.optional(),
              status: z.enum([
                "unselected",
                "unlinked",
                "deleted",
                "linked",
                "post_unlinked",
                "post_undecided",
                "post_conflict",
                "excluded",
              ]),
            }),
          )
          .max(postMergeLimit),
        add: z.array(media).max(postMergeLimit),
        remove: z.array(media).max(postMergeLimit),
      })
      .nullable(),
    media: z.array(postMergeMediaChoiceSchema).max(postMergeLimit),
    attachments: z.array(postMergeAttachmentChoiceSchema).max(postMergeLimit),
    blockers: z.array(postMergeBlockerSchema).max(postMergeLimit * 2 + 8),
    ready: z.boolean(),
    digest,
  })
  .refine(
    (value) =>
      value.source.uuid !== value.destination.uuid &&
      value.source.canonical_uuid === value.source.uuid &&
      value.source.redirect_to === null &&
      value.destination.canonical_uuid === value.destination.uuid &&
      value.destination.redirect_to === null &&
      value.posts.some((p) => p.uuid === value.source.uuid) &&
      value.posts.some((p) => p.uuid === value.destination.uuid) &&
      unique(value.posts, (p) => p.uuid) &&
      unique(value.media, (p) => p.media_uuid) &&
      unique(value.attachments, referenceKey) &&
      value.ready === (value.blockers.length === 0) &&
      (!value.ready || value.album !== null),
  );
export const postMergeRecordSchema = z.object({
  uuid,
  sequence: revision,
  source_uuid: uuid,
  destination_uuid: uuid,
  source_revision: revision,
  destination_revision: revision,
  member_count: revision.min(2).max(256),
  identity_signature: digest,
  review_signature: digest,
  origin: z.enum(["review", "migration"]),
  reason: z.string(),
  created_at: time,
});
export const postMergeReceiptSchema = z
  .object({
    request: receiptRequestSchema,
    result: z.object({
      consolidation: postMergeRecordSchema,
      members: z
        .array(
          z.object({
            post_uuid: uuid,
            previous_canonical_uuid: uuid,
            previous_revision: revision,
          }),
        )
        .min(2)
        .max(256),
      selection_uuid: uuid.optional(),
      gallery_decision_uuid: uuid.optional(),
      media_decision_uuids: ids,
      attachment_decision_uuids: ids,
      gallery: z.object({
        gallery_uuid: uuid.optional(),
        action,
        created: z.boolean(),
        added: ids,
        removed: ids,
      }),
      notification_job_uuid: uuid.optional(),
    }),
  })
  .refine(({ request, result }) => {
    const event = result.consolidation;
    const changed =
      result.gallery.created ||
      result.gallery.added.length > 0 ||
      result.gallery.removed.length > 0;
    return (
      event.uuid === request.request_uuid &&
      event.source_uuid === request.source_uuid &&
      event.destination_uuid === request.destination_uuid &&
      event.review_signature === request.digest &&
      event.origin === "review" &&
      event.reason === request.reason &&
      result.members.length === event.member_count &&
      unique(result.members, (m) => m.post_uuid) &&
      result.members.some(
        (m) =>
          m.post_uuid === request.source_uuid &&
          m.previous_canonical_uuid === request.source_uuid &&
          m.previous_revision === event.source_revision,
      ) &&
      result.members.some(
        (m) =>
          m.post_uuid === request.destination_uuid &&
          m.previous_canonical_uuid === request.destination_uuid &&
          m.previous_revision === event.destination_revision,
      ) &&
      result.members.every(
        (m) =>
          m.previous_canonical_uuid === request.source_uuid ||
          m.previous_canonical_uuid === request.destination_uuid,
      ) &&
      result.gallery.created === (result.gallery.action === "create") &&
      changed === (result.notification_job_uuid !== undefined) &&
      (!changed ||
        (result.gallery.gallery_uuid !== undefined &&
          result.gallery_decision_uuid !== undefined &&
          ["create", "sync"].includes(result.gallery.action)))
    );
  });
export type PostMergePreview = z.infer<typeof postMergePreviewSchema>;
export type PostMergeReceipt = z.infer<typeof postMergeReceiptSchema>;
export type PostMergePost = z.infer<typeof postMergePostSchema>;
export type PostMergeEntity = z.infer<typeof postMergeEntitySchema>;

export function postMergeRequestKey(value: unknown) {
  return JSON.stringify(postMergeApplySchema.parse(value));
}
