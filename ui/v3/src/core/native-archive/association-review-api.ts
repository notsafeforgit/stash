import { z } from "zod";
import {
  accountUUIDSchema as uuid,
  ownershipReasonSchema as reason,
} from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
import { createAssociationProtocol } from "./association-review-protocol";
import {
  postAlbumSchema,
  postLibraryItemSchema,
  createSourcePostAPI,
} from "./source-post-api";

const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const digest = z.string().regex(/^[0-9a-f]{64}$/);
const time = z.string().datetime({ offset: true });
const inputBase = { post_uuid: uuid, post_revision: revision };
const galleryShape = z
  .object({
    ...inputBase,
    state: z.enum(["linked", "disabled"]),
    gallery_uuid: uuid.optional(),
    gallery_revision: revision.optional(),
    reason: reason.optional(),
  })
  .strict();
function validGallery(input: z.infer<typeof galleryShape>) {
  return input.state === "linked"
    ? input.gallery_uuid !== undefined && input.gallery_revision !== undefined
    : input.gallery_uuid === undefined && input.gallery_revision === undefined;
}
export const galleryAssociationInputSchema = galleryShape.refine(validGallery);
export const galleryAssociationApplySchema = galleryShape
  .extend({ request_uuid: uuid, digest })
  .refine(validGallery);
export const galleryAssociationPreviewSchema = z
  .object({
    input: galleryAssociationInputSchema,
    current: postAlbumSchema.nullable(),
    proposed: postLibraryItemSchema.nullable(),
    changed: z.boolean(),
    digest,
  })
  .refine(
    ({ input, current, proposed }) =>
      (!current ||
        (current.post_uuid === input.post_uuid &&
          current.revision <= input.post_revision)) &&
      (input.state === "disabled"
        ? proposed === null
        : proposed?.kind === "gallery" &&
          proposed.state === "active" &&
          proposed.uuid === input.gallery_uuid &&
          proposed.revision === input.gallery_revision),
  );
export const galleryAssociationReceiptSchema = z.object({
  request_uuid: uuid,
  decision_uuid: uuid,
  request: galleryAssociationApplySchema,
  created_at: time,
});
const galleryDecisionSchema = z
  .object({
    post_uuid: uuid,
    decision_uuid: uuid,
    revision,
    state: z.enum(["linked", "disabled"]),
    gallery_uuid: uuid.nullable(),
    gallery: z.null(),
    selection_uuid: uuid.nullable(),
    origin: z.enum(["review", "migration", "source"]),
    reason,
    created_at: time,
  })
  .refine((item) => (item.state === "linked") === (item.gallery_uuid !== null));

const mediaShape = z
  .object({
    ...inputBase,
    attachment_uuid: uuid,
    attachment_revision: revision,
    state: z.enum(["linked", "unlinked", "undecided"]),
    media_uuid: uuid.optional(),
    media_revision: revision.optional(),
    reason: reason.optional(),
  })
  .strict();
function validMedia(input: z.infer<typeof mediaShape>) {
  return input.state === "linked"
    ? input.media_uuid !== undefined && input.media_revision !== undefined
    : input.media_uuid === undefined && input.media_revision === undefined;
}
export const attachmentMediaInputSchema = mediaShape.refine(validMedia);
export const attachmentMediaApplySchema = mediaShape
  .extend({ request_uuid: uuid, digest })
  .refine(validMedia);
const mediaDecisionSchema = z
  .object({
    uuid,
    attachment_uuid: uuid,
    revision,
    state: z.enum(["linked", "unlinked", "undecided"]),
    media_uuid: uuid.nullable(),
    origin: z.enum(["review", "ingest", "migration"]),
    reason,
    created_at: time,
  })
  .refine((item) => (item.state === "linked") === (item.media_uuid !== null));
const mediaItem = postLibraryItemSchema.refine(
  (item) => item.kind !== "gallery",
);
export const attachmentMediaContextSchema = z
  .object({
    requested_attachment_uuid: uuid,
    post_uuid: uuid,
    post_revision: revision,
    post_state: z.enum(["active", "forgotten"]),
    attachment: z.object({
      uuid,
      revision,
      reference: z.object({
        namespace: z.string().min(1).max(128),
        value: z.string().min(1).max(2048),
      }),
    }),
    current: mediaDecisionSchema.nullable(),
    media: mediaItem.nullable(),
    post_link_state: z.enum([
      "",
      "linked",
      "unlinked",
      "undecided",
      "conflict",
    ]),
    source_media_kinds: z.array(z.enum(["image", "video"])).max(2),
  })
  .refine((item) => {
    if (
      new Set(item.source_media_kinds).size !== item.source_media_kinds.length
    )
      return false;
    if (
      item.current &&
      (item.current.attachment_uuid !== item.attachment.uuid ||
        item.current.revision > item.attachment.revision)
    )
      return false;
    return item.current?.state === "linked"
      ? item.media !== null && item.post_link_state !== ""
      : item.media === null && item.post_link_state === "";
  });
export const attachmentMediaPreviewSchema = z
  .object({
    input: attachmentMediaInputSchema,
    current: attachmentMediaContextSchema,
    proposed: mediaItem.nullable(),
    changed: z.boolean(),
    digest,
  })
  .refine(
    ({ input, current, proposed }) =>
      current.post_uuid === input.post_uuid &&
      current.post_revision === input.post_revision &&
      current.post_state === "active" &&
      current.requested_attachment_uuid === input.attachment_uuid &&
      current.attachment.uuid === input.attachment_uuid &&
      current.attachment.revision === input.attachment_revision &&
      (input.state === "linked"
        ? proposed?.state === "active" &&
          proposed.uuid === input.media_uuid &&
          proposed.revision === input.media_revision
        : proposed === null),
  );
export const attachmentMediaReceiptSchema = z.object({
  request_uuid: uuid,
  decision_uuid: uuid,
  request: attachmentMediaApplySchema,
  created_at: time,
});

export type GalleryAssociationInput = z.infer<
  typeof galleryAssociationInputSchema
>;
export type GalleryAssociationApply = z.infer<
  typeof galleryAssociationApplySchema
>;
export type GalleryAssociationPreview = z.infer<
  typeof galleryAssociationPreviewSchema
>;
export type GalleryAssociationReceipt = z.infer<
  typeof galleryAssociationReceiptSchema
>;
export type GalleryAssociationDecision = z.infer<typeof galleryDecisionSchema>;
export type AttachmentMediaInput = z.infer<typeof attachmentMediaInputSchema>;
export type AttachmentMediaApply = z.infer<typeof attachmentMediaApplySchema>;
export type AttachmentMediaPreview = z.infer<
  typeof attachmentMediaPreviewSchema
>;
export type AttachmentMediaReceipt = z.infer<
  typeof attachmentMediaReceiptSchema
>;
export type AttachmentMediaContext = z.infer<
  typeof attachmentMediaContextSchema
>;
export type AttachmentMediaDecision = z.infer<typeof mediaDecisionSchema>;

function historyCursor(after: number) {
  z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).parse(after);
  return `after=${after}&limit=25`;
}
export function createGalleryAssociationAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const posts = createSourcePostAPI(endpoint, transport);
  return {
    ...createAssociationProtocol(
      "gallery-association",
      endpoint,
      transport,
      {
        input: galleryAssociationInputSchema,
        apply: galleryAssociationApplySchema,
        preview: galleryAssociationPreviewSchema,
        receipt: galleryAssociationReceiptSchema,
      },
      (input) => input.post_uuid,
    ),
    pageLimit: 25,
    contextScope: (value: { post: { uuid: string } }) => value.post.uuid,
    async context(post: string, signal?: AbortSignal) {
      const [value, album] = await Promise.all([
        posts.post(post, signal),
        posts.album(post, signal),
      ]);
      return { post: value, album };
    },
    async history(post: string, after = 0, signal?: AbortSignal) {
      const values = await request(
        `posts/${uuid.parse(post)}/gallery-association-history?${historyCursor(after)}`,
        z.array(galleryDecisionSchema).max(25),
        undefined,
        signal,
      );
      let previous = after;
      for (const item of values) {
        if (item.post_uuid !== post || item.revision <= previous)
          throw new NativeArchiveError(0, "invalid_response");
        previous = item.revision;
      }
      return values;
    },
  };
}
export function createAttachmentMediaAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  return {
    ...createAssociationProtocol(
      "attachment-media",
      endpoint,
      transport,
      {
        input: attachmentMediaInputSchema,
        apply: attachmentMediaApplySchema,
        preview: attachmentMediaPreviewSchema,
        receipt: attachmentMediaReceiptSchema,
      },
      (input) => input.attachment_uuid,
    ),
    pageLimit: 25,
    contextScope: (value: AttachmentMediaContext) => value.attachment.uuid,
    async context(attachment: string, signal?: AbortSignal) {
      const value = await request(
        `attachments/${uuid.parse(attachment)}/review`,
        attachmentMediaContextSchema,
        undefined,
        signal,
      );
      if (value.requested_attachment_uuid !== attachment)
        throw new NativeArchiveError(0, "invalid_response");
      return value;
    },
    async history(attachment: string, after = 0, signal?: AbortSignal) {
      const values = await request(
        `attachments/${uuid.parse(attachment)}/media-history?${historyCursor(after)}`,
        z.array(mediaDecisionSchema).max(25),
        undefined,
        signal,
      );
      let previous = after;
      for (const item of values) {
        if (item.attachment_uuid !== attachment || item.revision <= previous)
          throw new NativeArchiveError(0, "invalid_response");
        previous = item.revision;
      }
      return values;
    },
  };
}
export type GalleryAssociationAPI = ReturnType<
  typeof createGalleryAssociationAPI
>;
export type AttachmentMediaAPI = ReturnType<typeof createAttachmentMediaAPI>;
