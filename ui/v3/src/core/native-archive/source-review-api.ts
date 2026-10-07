import { z } from "zod";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
import { identitySchema } from "./metadata-review-api";

const uuid = z
  .string()
  .regex(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/)
  .refine((v) => v !== "00000000-0000-0000-0000-000000000000");
const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const time = z.string().datetime({ offset: true });
export const sourceLinkStateSchema = z.enum([
  "linked",
  "unlinked",
  "undecided",
]);
export const sourceDecisionSchema = z.object({
  uuid,
  post_uuid: uuid,
  media_uuid: uuid,
  post_revision: revision,
  media_revision: revision,
  state: sourceLinkStateSchema,
  origin: z.enum(["review", "migration"]),
  reason: z.string(),
  created_at: time,
});
export const sourceAssociationSchema = z
  .object({
    post_uuid: uuid,
    post_revision: revision,
    post_state: z.enum(["active", "forgotten"]),
    media_uuid: uuid,
    media_revision: revision,
    media_state: z.enum(["active", "redirected", "deleted"]),
    state: z.enum(["linked", "unlinked", "undecided", "conflict"]),
    decisions: z.array(sourceDecisionSchema).max(1024),
  })
  .refine(
    (a) => new Set(a.decisions.map((d) => d.uuid)).size === a.decisions.length,
  );
const clockSchema = z.object({
  captured_at: time.nullable(),
  recorded_at: time.nullable(),
});
const validClock = (value: z.infer<typeof clockSchema>) =>
  (value.captured_at === null) !== (value.recorded_at === null);
export const sourceURLSchema = z.object({
  uuid,
  post_uuid: uuid,
  url: z.string().min(1).max(8192),
});
export const sourcePostSchema = z
  .object({
    requested_post_uuid: uuid,
    association: sourceAssociationSchema,
    latest_capture: clockSchema
      .extend({
        uuid,
        post_uuid: uuid,
        revision_uuid: uuid,
        origin: z.string(),
        platform: z.string(),
        title: z.string().nullable(),
        title_truncated: z.boolean(),
        published_at: z.string().nullable(),
        date_basis: z.string().nullable(),
      })
      .refine(validClock)
      .nullable(),
    urls: z.array(sourceURLSchema).max(3),
    more_urls: z.boolean(),
    has_retained_evidence: z.boolean(),
    linked_attachments: z.number().int().nonnegative(),
  })
  .refine(
    (post) =>
      new Set(post.urls.map((url) => url.url)).size === post.urls.length,
  );
export const sourceCaptureSchema = clockSchema
  .extend({
    uuid,
    post_uuid: uuid,
    revision_uuid: uuid,
    origin: z.string(),
    platform: z.string(),
    extractor_version: z.string().nullable(),
  })
  .refine(validClock);
export const sourceRevisionSchema = z.object({
  uuid,
  metadata: z.object({
    title: z.string().optional(),
    original_text: z.string().optional(),
    published_at: z.string().optional(),
    date_basis: z.string().optional(),
    language: z.string().optional(),
  }),
});
const capturesSchema = z
  .object({
    requested_uuid: uuid,
    captures: z.array(sourceCaptureSchema).max(100),
    revisions: z.array(sourceRevisionSchema).max(100),
  })
  .refine(
    (page) =>
      new Set(page.captures.map((c) => c.uuid)).size === page.captures.length &&
      new Set(page.revisions.map((r) => r.uuid)).size ===
        page.revisions.length &&
      page.captures.every((c) =>
        page.revisions.some((r) => r.uuid === c.revision_uuid),
      ),
  );
export const sourceLinkInputSchema = z
  .object({
    uuid,
    post_uuid: uuid,
    media_uuid: uuid,
    expected_post_revision: revision,
    expected_media_revision: revision,
    expected_decisions: z
      .array(uuid)
      .max(1024)
      .refine((ids) => new Set(ids).size === ids.length),
    state: sourceLinkStateSchema,
    origin: z.literal("review"),
    reason: z
      .string()
      .refine((value) => new TextEncoder().encode(value).length <= 4096),
  })
  .strict();
export type SourcePost = z.infer<typeof sourcePostSchema>;
export type SourceAssociation = z.infer<typeof sourceAssociationSchema>;
export type SourceDecision = z.infer<typeof sourceDecisionSchema>;
export type SourceLinkInput = z.infer<typeof sourceLinkInputSchema>;
export type SourceLinkState = z.infer<typeof sourceLinkStateSchema>;
export type SourceCapture = z.infer<typeof sourceCaptureSchema>;
export type SourceRevision = z.infer<typeof sourceRevisionSchema>;
export type SourceURL = z.infer<typeof sourceURLSchema>;

export function sourceLinkKey(input: SourceLinkInput): string {
  const value = sourceLinkInputSchema.parse(input);
  return JSON.stringify({
    ...value,
    expected_decisions: [...value.expected_decisions].sort(),
  });
}
function checkReceipt(input: SourceLinkInput, receipt: SourceDecision) {
  if (
    receipt.uuid !== input.uuid ||
    receipt.post_uuid !== input.post_uuid ||
    receipt.media_uuid !== input.media_uuid ||
    receipt.post_revision !== input.expected_post_revision + 1 ||
    receipt.media_revision !== input.expected_media_revision ||
    receipt.state !== input.state ||
    receipt.origin !== input.origin ||
    receipt.reason !== input.reason
  )
    throw new NativeArchiveError(0, "mismatched_receipt");
  return receipt;
}

export function createSourceReviewAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  const mediaPath = (post: string, media: string) =>
    `posts/${uuid.parse(post)}/media/${uuid.parse(media)}`;
  return {
    endpoint,
    pageLimit,
    async identity(
      kind: "scene" | "image",
      localId: string,
      signal?: AbortSignal,
    ) {
      z.string()
        .regex(/^[1-9]\d*$/)
        .parse(localId);
      const result = await request(
        `entity-identities/${kind}/${localId}`,
        identitySchema,
        undefined,
        signal,
      );
      if (result.kind !== kind || String(result.local_id) !== localId)
        throw new NativeArchiveError(0, "invalid_response");
      return result;
    },
    async posts(media: string, after?: string, signal?: AbortSignal) {
      const query = new URLSearchParams({ limit: String(pageLimit) });
      if (after) query.set("after", uuid.parse(after));
      const rows = await request(
        `entities/${uuid.parse(media)}/source-posts?${query}`,
        z.array(sourcePostSchema).max(pageLimit),
        undefined,
        signal,
      );
      let previous = after ?? "";
      for (const row of rows) {
        if (
          row.association.media_uuid !== media ||
          row.requested_post_uuid !== row.association.post_uuid ||
          row.association.post_uuid <= previous
        )
          throw new NativeArchiveError(0, "invalid_response");
        previous = row.association.post_uuid;
      }
      return rows;
    },
    async review(post: string, media: string, signal?: AbortSignal) {
      const row = await request(
        `${mediaPath(post, media)}/review`,
        sourcePostSchema,
        undefined,
        signal,
      );
      if (
        row.requested_post_uuid !== post ||
        row.association.media_uuid !== media
      )
        throw new NativeArchiveError(0, "invalid_response");
      return row;
    },
    async urls(post: string, after?: string, signal?: AbortSignal) {
      const query = new URLSearchParams({ limit: String(pageLimit) });
      if (after) query.set("after", uuid.parse(after));
      const page = await request(
        `posts/${uuid.parse(post)}/urls?${query}`,
        z.object({
          requested_uuid: uuid,
          urls: z.array(sourceURLSchema).max(pageLimit),
        }),
        undefined,
        signal,
      );
      if (
        page.requested_uuid !== post ||
        new Set(page.urls.map((url) => url.url)).size !== page.urls.length
      )
        throw new NativeArchiveError(0, "invalid_response");
      const rows = page.urls;
      let previous = after ?? "";
      for (const row of rows) {
        if (row.uuid <= previous)
          throw new NativeArchiveError(0, "invalid_response");
        previous = row.uuid;
      }
      return rows;
    },
    async captures(post: string, after?: SourceCapture, signal?: AbortSignal) {
      const query = new URLSearchParams({ limit: String(pageLimit) });
      if (after) {
        const cursor = sourceCaptureSchema.parse(after);
        query.set("after_uuid", cursor.uuid);
        query.set("after_time", cursor.captured_at ?? cursor.recorded_at ?? "");
        query.set("after_clock", cursor.captured_at ? "observed" : "recorded");
      }
      const result = await request(
        `posts/${uuid.parse(post)}/capture-summaries?${query}`,
        capturesSchema,
        undefined,
        signal,
      );
      if (result.requested_uuid !== post)
        throw new NativeArchiveError(0, "invalid_response");
      return result;
    },
    async history(
      post: string,
      media: string,
      after = 0,
      signal?: AbortSignal,
    ) {
      const rows = await request(
        `${mediaPath(post, media)}/history?after=${z.number().int().nonnegative().parse(after)}&limit=${pageLimit}`,
        z.array(sourceDecisionSchema).max(pageLimit),
        undefined,
        signal,
      );
      let previous = after;
      for (const row of rows) {
        if (row.post_uuid !== post || row.post_revision <= previous)
          throw new NativeArchiveError(0, "invalid_response");
        previous = row.post_revision;
      }
      return rows;
    },
    async receipt(input: SourceLinkInput) {
      sourceLinkInputSchema.parse(input);
      try {
        return checkReceipt(
          input,
          await request(
            `post-media-decisions/${input.uuid}`,
            sourceDecisionSchema,
          ),
        );
      } catch (error) {
        if (error instanceof NativeArchiveError && error.status === 404)
          return null;
        throw error;
      }
    },
    async applySaved(body: string) {
      const input = sourceLinkInputSchema.parse(JSON.parse(body));
      const receipt = await request(
        mediaPath(input.post_uuid, input.media_uuid),
        sourceDecisionSchema,
        body,
        undefined,
        "PUT",
      );
      return checkReceipt(input, receipt);
    },
  };
}
export type SourceReviewAPI = ReturnType<typeof createSourceReviewAPI>;
