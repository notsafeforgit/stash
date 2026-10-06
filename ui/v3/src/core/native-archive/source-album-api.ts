import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { identitySchema } from "./metadata-review-api";
import {
  postAlbumSchema,
  postLibraryItemSchema,
  postSummarySchema,
} from "./source-post-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

const integer = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const position = z.number().int().min(0).max(999999);
const media = postLibraryItemSchema.refine((item) => item.kind !== "gallery");
export const albumSlotSchema = z
  .object({
    position,
    through: position,
    attachment: z
      .object({
        uuid,
        revision: integer,
        reference: z.object({
          namespace: z.string().min(1),
          value: z.string().min(1),
        }),
      })
      .nullable(),
    media_kind: z.enum(["image", "video", "unknown"]),
    selection_state: z.enum([
      "unknown",
      "unselected",
      "linked",
      "unlinked",
      "undecided",
    ]),
    decision_uuid: uuid.nullable(),
    media: media.nullable(),
    post_link_state: z.enum([
      "",
      "linked",
      "unlinked",
      "undecided",
      "conflict",
    ]),
    gallery_membership: z.enum([
      "no_gallery",
      "absent",
      "included",
      "excluded",
    ]),
    registered_files: z
      .number()
      .int()
      .nonnegative()
      .max(Number.MAX_SAFE_INTEGER),
  })
  .refine((slot) => {
    if (slot.through < slot.position) return false;
    if (!slot.attachment)
      return (
        slot.selection_state === "unknown" &&
        slot.media_kind === "unknown" &&
        !slot.decision_uuid &&
        !slot.media &&
        slot.registered_files === 0 &&
        slot.post_link_state === "" &&
        slot.gallery_membership === "no_gallery"
      );
    return (
      slot.position === slot.through &&
      slot.selection_state !== "unknown" &&
      (slot.selection_state === "unselected") ===
        (slot.decision_uuid === null) &&
      (slot.selection_state === "linked") === (slot.media !== null) &&
      (slot.media !== null) === (slot.post_link_state !== "") &&
      (slot.media?.state === "active" || slot.registered_files === 0) &&
      (slot.media !== null ||
        (slot.gallery_membership !== "included" &&
          slot.gallery_membership !== "excluded"))
    );
  });
export const albumPageSchema = z
  .object({
    post_uuid: uuid,
    post_revision: integer,
    post_state: z.enum(["active", "forgotten"]),
    signature: z.string().regex(/^[0-9a-f]{64}$/),
    selection: z
      .object({
        uuid,
        revision: integer,
        mode: z.enum(["automatic", "pinned", "disabled"]),
        complete: z.boolean(),
        declared_album: z.boolean(),
        expected_count: z.number().int().min(0).max(1000000).nullable(),
        entry_count: z.number().int().min(0).max(4096),
      })
      .nullable(),
    album: postAlbumSchema.nullable(),
    slots: z.array(albumSlotSchema).max(25),
    next_after: position.nullable(),
  })
  .refine((page) => {
    if (page.album && page.album.post_uuid !== page.post_uuid) return false;
    if (!page.selection)
      return page.slots.length === 0 && page.next_after === null;
    const count = page.selection.expected_count;
    if (page.selection.complete && count !== page.selection.entry_count)
      return false;
    if (page.slots.some((slot) => count !== null && slot.through >= count))
      return false;
    if (
      page.next_after !== null &&
      (page.slots.length !== 25 ||
        page.slots.at(-1)?.through !== page.next_after)
    )
      return false;
    return true;
  });
export const galleryPostsSchema = z.object({
  requested_uuid: uuid,
  gallery: postLibraryItemSchema.refine((item) => item.kind === "gallery"),
  posts: z.array(postSummarySchema).max(25),
});
export type AlbumPage = z.infer<typeof albumPageSchema>;
export type AlbumSlot = z.infer<typeof albumSlotSchema>;
export type GalleryPosts = z.infer<typeof galleryPostsSchema>;

export function createSourceAlbumAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  return {
    endpoint,
    pageLimit,
    async identity(localId: string, signal?: AbortSignal) {
      z.string()
        .regex(/^[1-9]\d*$/)
        .parse(localId);
      const identity = await request(
        `entity-identities/gallery/${localId}`,
        identitySchema,
        undefined,
        signal,
      );
      if (identity.kind !== "gallery" || String(identity.local_id) !== localId)
        throw new NativeArchiveError(0, "invalid_response");
      return identity;
    },
    async album(post: string, after?: number, signal?: AbortSignal) {
      const params = new URLSearchParams({ limit: String(pageLimit) });
      if (after !== undefined)
        params.set("after", String(position.parse(after)));
      const page = await request(
        `posts/${uuid.parse(post)}/album-media?${params}`,
        albumPageSchema,
        undefined,
        signal,
      );
      if (page.post_uuid !== post)
        throw new NativeArchiveError(0, "invalid_response");
      let previous = after ?? -1;
      for (const slot of page.slots) {
        if (slot.position !== previous + 1)
          throw new NativeArchiveError(0, "invalid_response");
        previous = slot.through;
      }
      return page;
    },
    async posts(gallery: string, after?: string, signal?: AbortSignal) {
      const params = new URLSearchParams({ limit: String(pageLimit) });
      if (after) params.set("after", uuid.parse(after));
      const page = await request(
        `entities/${uuid.parse(gallery)}/album-posts?${params}`,
        galleryPostsSchema,
        undefined,
        signal,
      );
      if (page.requested_uuid !== gallery)
        throw new NativeArchiveError(0, "invalid_response");
      let previous = after ?? "";
      for (const post of page.posts) {
        if (post.uuid <= previous)
          throw new NativeArchiveError(0, "invalid_response");
        previous = post.uuid;
      }
      return page;
    },
  };
}
export type SourceAlbumAPI = ReturnType<typeof createSourceAlbumAPI>;
