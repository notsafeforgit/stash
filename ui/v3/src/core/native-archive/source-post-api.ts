import { z } from "zod";
import { accountSchema, accountUUIDSchema as uuid } from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
import {
  createSourceReviewAPI,
  sourceAssociationSchema,
  sourcePostSchema,
  sourceURLSchema,
} from "./source-review-api";

function boundedText(bytes: number) {
  return z
    .string()
    .refine(
      (value) =>
        value === value.trim() &&
        !/\p{Cc}/u.test(value) &&
        new TextEncoder().encode(value).length <= bytes,
    );
}
const namespace = boundedText(128).regex(
  /^(?:native|ytdl):[a-z0-9][a-z0-9_.-]*$|^(?:mirror|legacy):[a-z0-9][a-z0-9_.-]*:[a-z0-9][a-z0-9_.-]*$/,
);
const identifier = z.object({ namespace, value: boundedText(2048).min(1) });
const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const postQueryShape = z.object({
  mode: z.enum(["all", "url", "source_id", "uuid"]),
  value: boundedText(8192),
  namespace: boundedText(128),
});
function validateFilter(
  filter: z.infer<typeof postQueryShape>,
  ctx: z.RefinementCtx,
) {
  if (filter.mode === "uuid" && !uuid.safeParse(filter.value).success)
    ctx.addIssue({
      code: "custom",
      path: ["value"],
      message: "Invalid archive ID",
    });
  if (filter.mode === "source_id") {
    if (!namespace.safeParse(filter.namespace).success)
      ctx.addIssue({
        code: "custom",
        path: ["namespace"],
        message: "Invalid source namespace",
      });
    if (!boundedText(2048).min(1).safeParse(filter.value).success)
      ctx.addIssue({
        code: "custom",
        path: ["value"],
        message: "Invalid source ID",
      });
  }
  if (filter.mode === "url") {
    try {
      const url = new URL(filter.value);
      if (!/^https?:$/.test(url.protocol) || url.username || url.password)
        throw new Error("Invalid URL");
    } catch {
      ctx.addIssue({
        code: "custom",
        path: ["value"],
        message: "Invalid post URL",
      });
    }
  }
}
export const postQuerySchema = postQueryShape.superRefine(validateFilter);
export const postFilterSchema = z
  .object({
    mode: postQueryShape.shape.mode.default("all"),
    value: postQueryShape.shape.value.default(""),
    namespace: postQueryShape.shape.namespace.default(""),
  })
  .superRefine(validateFilter);
export const postSearchSchema = postFilterSchema.safeExtend({
  post: uuid.optional(),
});
export type PostFilter = z.infer<typeof postFilterSchema>;
export type PostIdentifier = z.infer<typeof identifier>;
export const postSummarySchema = z
  .object({
    uuid,
    state: z.enum(["active", "forgotten"]),
    revision,
    created_at: z.string().datetime({ offset: true }),
    identifiers: z.array(identifier).max(3),
    more_identifiers: z.boolean(),
    urls: z.array(sourceURLSchema).max(3),
    more_urls: z.boolean(),
    latest_capture: sourcePostSchema.shape.latest_capture,
  })
  .refine((post) => post.urls.every((url) => url.post_uuid === post.uuid));
export const postLibraryItemSchema = z
  .object({
    uuid,
    kind: z.enum(["scene", "image", "gallery"]),
    state: z.enum(["active", "deleted"]),
    revision,
    local_id: revision.nullable(),
    title: z.string(),
    title_truncated: z.boolean(),
  })
  .refine((item) => (item.state === "active") === (item.local_id !== null));
export const postMediaSchema = z
  .object({
    media: postLibraryItemSchema,
    association: sourceAssociationSchema,
    has_retained_evidence: z.boolean(),
    linked_attachments: z.number().int().nonnegative(),
  })
  .refine(
    ({ media, association }) =>
      media.kind !== "gallery" &&
      media.uuid === association.media_uuid &&
      media.revision === association.media_revision &&
      media.state === association.media_state,
  );
export const postAlbumSchema = z
  .object({
    post_uuid: uuid,
    decision_uuid: uuid,
    revision,
    state: z.enum(["linked", "disabled"]),
    gallery_uuid: uuid.nullable(),
    gallery: postLibraryItemSchema.nullable(),
    selection_uuid: uuid.nullable(),
    origin: z.string(),
    reason: z.string(),
    created_at: z.string().datetime({ offset: true }),
  })
  .refine((album) =>
    album.state === "disabled"
      ? album.gallery_uuid === null && album.gallery === null
      : album.gallery_uuid !== null && album.gallery?.kind === "gallery",
  );
export type PostSummary = z.infer<typeof postSummarySchema>;
export type PostMedia = z.infer<typeof postMediaSchema>;
export type PostAlbum = z.infer<typeof postAlbumSchema>;
export type PostLibraryItem = z.infer<typeof postLibraryItemSchema>;

function invalidResponse(): never {
  throw new NativeArchiveError(0, "invalid_response");
}
function ordered<T>(rows: T[], key: (row: T) => string, after = "") {
  let previous = after;
  for (const row of rows) {
    const current = key(row);
    if (current <= previous) invalidResponse();
    previous = current;
  }
  return rows;
}
// SQLite's BINARY ordering uses UTF-8 bytes, unlike JavaScript UTF-16 ordering.
function compareIdentifiers(a: PostIdentifier, b: PostIdentifier) {
  const encoder = new TextEncoder();
  const first = encoder.encode(`${a.namespace}\0${a.value}`);
  const second = encoder.encode(`${b.namespace}\0${b.value}`);
  for (let i = 0; i < Math.min(first.length, second.length); i++) {
    if (first[i] !== second[i]) return (first[i] ?? 0) - (second[i] ?? 0);
  }
  return first.length - second.length;
}

export function createSourcePostAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  const path = (post: string) => `posts/${uuid.parse(post)}`;
  function page(after?: string) {
    const query = new URLSearchParams({ limit: String(pageLimit) });
    if (after) query.set("after", uuid.parse(after));
    return query;
  }
  return {
    endpoint,
    pageLimit,
    review: createSourceReviewAPI(endpoint, transport),
    async posts(filter: PostFilter, after?: string, signal?: AbortSignal) {
      const valid = postFilterSchema.parse(filter);
      const query = page(after);
      if (valid.mode === "url" || valid.mode === "uuid")
        query.set(valid.mode, valid.value);
      if (valid.mode === "source_id") {
        query.set("namespace", valid.namespace);
        query.set("value", valid.value);
      }
      const rows = await request(
        `posts?${query}`,
        z.array(postSummarySchema).max(pageLimit),
        undefined,
        signal,
      );
      if (valid.mode === "uuid" && rows.some((row) => row.uuid !== valid.value))
        invalidResponse();
      return ordered(rows, (row) => row.uuid, after);
    },
    async post(id: string, signal?: AbortSignal) {
      const result = await request(
        path(id),
        postSummarySchema,
        undefined,
        signal,
      );
      if (result.uuid !== id) invalidResponse();
      return result;
    },
    async identifiers(
      post: string,
      after?: PostIdentifier,
      signal?: AbortSignal,
    ) {
      const query = page();
      if (after) {
        const cursor = identifier.parse(after);
        query.set("after_namespace", cursor.namespace);
        query.set("after_value", cursor.value);
      }
      const rows = await request(
        `${path(post)}/identifiers?${query}`,
        z.array(identifier).max(pageLimit),
        undefined,
        signal,
      );
      let previous = after;
      for (const row of rows) {
        if (previous && compareIdentifiers(row, previous) <= 0)
          invalidResponse();
        previous = row;
      }
      return rows;
    },
    async publishers(post: string, after?: string, signal?: AbortSignal) {
      const rows = await request(
        `${path(post)}/publishers?${page(after)}`,
        z.array(accountSchema).max(pageLimit),
        undefined,
        signal,
      );
      if (
        rows.some((row) => row.uuid !== row.canonical_uuid || row.redirect_to)
      )
        invalidResponse();
      return ordered(rows, (row) => row.uuid, after);
    },
    async media(post: string, after?: string, signal?: AbortSignal) {
      const rows = await request(
        `${path(post)}/media?${page(after)}`,
        z.array(postMediaSchema).max(pageLimit),
        undefined,
        signal,
      );
      if (rows.some((row) => row.association.post_uuid !== post))
        invalidResponse();
      return ordered(rows, (row) => row.media.uuid, after);
    },
    async album(post: string, signal?: AbortSignal) {
      const album = await request(
        `${path(post)}/album`,
        postAlbumSchema.nullable(),
        undefined,
        signal,
      );
      if (album && album.post_uuid !== post) invalidResponse();
      return album;
    },
  };
}
export type SourcePostAPI = ReturnType<typeof createSourcePostAPI>;
