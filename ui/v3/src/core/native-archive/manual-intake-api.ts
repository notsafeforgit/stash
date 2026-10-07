import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
import type { Collection } from "./collection-api";

const count = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
const revision = count.positive();
const signature = z.string().regex(/^[0-9a-f]{64}$/);
const timestamp = z.string().datetime({ offset: true });
const relativePath = z
  .string()
  .min(1)
  .max(4096)
  .refine(
    (value) =>
      !/[\\\p{Cc}]/u.test(value) &&
      !/^[a-z]:/i.test(value) &&
      !value.split("/").some((part) => !part || part === "." || part === ".."),
  );
const directory = z.union([z.literal("."), relativePath]);
export const manualFileInputSchema = z
  .object({
    collection_uuid: uuid,
    relative_path: relativePath.refine(
      (value) => !value.toLowerCase().endsWith(".part"),
    ),
    media_kind: z.enum(["scene", "image"]),
  })
  .strict();
export const manualFileRequestSchema = manualFileInputSchema.extend({
  request_uuid: uuid,
  signature,
});
export const manualFilePreviewSchema = manualFileInputSchema
  .extend({
    collection_revision: revision,
    policy_revision: count,
    root_uuid: uuid,
    root_revision: revision,
    filename: z.string().min(1),
    size: count.positive(),
    modified_at: timestamp,
    existing_file_uuid: uuid.optional(),
    file_signature: signature,
    signature,
  })
  .refine((p) => p.filename === p.relative_path.split("/").at(-1));
const publicationSchema = z.object({
  file_uuid: uuid,
  generation: revision,
  content_uuid: uuid,
  media_uuid: uuid,
  media_kind: z.enum(["scene", "image"]),
  media_created: z.boolean(),
  file_linked: z.boolean(),
  source_media: z.string(),
  gallery_uuid: uuid.optional(),
  gallery: z.string(),
  review: z.array(z.string()).nullable(),
  metadata_state: z.string().optional(),
  metadata_fields: z.array(z.string()).optional(),
});
export const manualFileStatusSchema = manualFileRequestSchema
  .extend({
    job_uuid: uuid,
    resume_from_job_uuid: uuid.optional(),
    resume_from_revision: revision.optional(),
    state: z.enum(["queued", "running", "succeeded", "failed", "cancelled"]),
    revision,
    attempts: count,
    max_attempts: revision,
    available_at: timestamp,
    registration_committed: z.boolean(),
    media_ingested: z.boolean(),
    publication: publicationSchema.optional(),
    error_code: z.string().optional(),
    created_at: timestamp,
    updated_at: timestamp,
  })
  .refine(
    (s) =>
      Boolean(s.resume_from_job_uuid) ===
        (s.resume_from_revision !== undefined) &&
      s.registration_committed === !!s.publication &&
      s.media_ingested === (s.state === "succeeded") &&
      (!s.media_ingested || s.registration_committed) &&
      (!s.publication || s.publication.media_kind === s.media_kind),
  );
export const manualDirectoryEntrySchema = z
  .object({
    name: z.string().min(1),
    relative_path: relativePath,
    kind: z.enum(["directory", "scene", "image"]),
    size: count,
    modified_at: timestamp.optional(),
  })
  .refine(
    (entry) =>
      entry.name === entry.relative_path.split("/").at(-1) &&
      (entry.kind === "directory" ? entry.size === 0 : !!entry.modified_at),
  );
const directoryPageSchema = z.object({
  collection_uuid: uuid,
  collection_revision: revision,
  root_uuid: uuid,
  root_revision: revision,
  path_prefix: directory,
  directory,
  signature,
  entries: z.array(manualDirectoryEntrySchema).max(50),
  next_after: z.string().min(3).optional(),
});
export const manualCancelSchema = z
  .object({ expected_revision: revision })
  .strict();
export const manualRetrySchema = manualCancelSchema.extend({
  request_uuid: uuid,
});
export type ManualFileInput = z.infer<typeof manualFileInputSchema>;
export type ManualFileRequest = z.infer<typeof manualFileRequestSchema>;
export type ManualFilePreview = z.infer<typeof manualFilePreviewSchema>;
export type ManualFileStatus = z.infer<typeof manualFileStatusSchema>;
export type ManualDirectoryEntry = z.infer<typeof manualDirectoryEntrySchema>;
export type ManualDirectoryPage = z.infer<typeof directoryPageSchema>;
export const manualBatchLimit = 25;
export function manualDirectoryKey(entry: ManualDirectoryEntry) {
  return `${entry.kind === "directory" ? "0" : "1"}/${entry.name}`;
}
// Go orders valid UTF-8 filenames by code point; JavaScript's UTF-16 order
// differs for supplementary characters next to high BMP characters.
function compareDirectoryKeys(a: string, b: string) {
  const left = Array.from(a, (value) => value.codePointAt(0)!);
  const right = Array.from(b, (value) => value.codePointAt(0)!);
  for (let i = 0; i < Math.min(left.length, right.length); i++) {
    const difference = left[i]! - right[i]!;
    if (difference) return difference;
  }
  return left.length - right.length;
}
export function sameManualFile(a: ManualFileInput, b: ManualFileInput) {
  return (
    a.collection_uuid === b.collection_uuid &&
    a.relative_path === b.relative_path &&
    a.media_kind === b.media_kind
  );
}
export function requireManualStatus(
  status: ManualFileStatus,
  input: ManualFileRequest,
) {
  if (
    !sameManualFile(status, input) ||
    status.request_uuid !== input.request_uuid ||
    status.signature !== input.signature
  )
    throw new NativeArchiveError(0, "mismatched_receipt");
  return status;
}
export function createManualIntakeAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  async function status(id: string, signal?: AbortSignal) {
    const value = await request(
      `manual-intake/requests/${uuid.parse(id)}`,
      manualFileStatusSchema,
      undefined,
      signal,
    );
    if (value.request_uuid !== id)
      throw new NativeArchiveError(0, "mismatched_receipt");
    return value;
  }
  return {
    endpoint,
    capabilities: (signal?: AbortSignal) =>
      request(
        "manual-intake/capabilities",
        z.object({ file_ingestion: z.boolean() }),
        undefined,
        signal,
      ),
    async directory(
      collection: Collection,
      folder: string,
      q = "",
      after?: ManualDirectoryPage,
      signal?: AbortSignal,
    ) {
      const params = new URLSearchParams({
        directory: directory.parse(folder),
        q,
        limit: "50",
      });
      if (after?.next_after) {
        params.set("after", after.next_after);
        params.set("signature", after.signature);
      }
      const page = await request(
        `collections/${uuid.parse(collection.uuid)}/intake-files?${params}`,
        directoryPageSchema,
        undefined,
        signal,
      );
      if (
        page.collection_uuid !== collection.uuid ||
        page.collection_revision !== collection.revision ||
        page.root_uuid !== collection.root_uuid ||
        page.path_prefix !== collection.path_prefix ||
        page.directory !== folder ||
        (after &&
          (page.signature !== after.signature ||
            page.root_revision !== after.root_revision)) ||
        page.entries.some(
          (entry, i) =>
            entry.relative_path !==
              (folder === "." ? entry.name : `${folder}/${entry.name}`) ||
            compareDirectoryKeys(
              manualDirectoryKey(entry),
              page.entries[i - 1]
                ? manualDirectoryKey(page.entries[i - 1]!)
                : (after?.next_after ?? ""),
            ) <= 0,
        ) ||
        (page.next_after &&
          (page.entries.length !== 50 ||
            page.next_after !== manualDirectoryKey(page.entries.at(-1)!)))
      )
        throw new NativeArchiveError(0, "directory_mismatch");
      return page;
    },
    async preview(input: ManualFileInput, signal?: AbortSignal) {
      const checked = manualFileInputSchema.parse(input);
      const result = await request(
        "manual-intake/preview",
        manualFilePreviewSchema,
        JSON.stringify(checked),
        signal,
      );
      if (!sameManualFile(result, checked))
        throw new NativeArchiveError(0, "mismatched_preview");
      return result;
    },
    status,
    async receipt(id: string, signal?: AbortSignal) {
      try {
        return await status(id, signal);
      } catch (error) {
        if (
          error instanceof NativeArchiveError &&
          error.status === 404 &&
          error.code === "not_found"
        )
          return null;
        throw error;
      }
    },
    async submitSaved(body: string) {
      const input = manualFileRequestSchema.parse(JSON.parse(body));
      return requireManualStatus(
        await request("manual-intake/apply", manualFileStatusSchema, body),
        input,
      );
    },
    async retrySaved(prior: ManualFileStatus, body: string) {
      const input = manualRetrySchema.parse(JSON.parse(body));
      const result = await request(
        `manual-intake/requests/${uuid.parse(prior.request_uuid)}/retry`,
        manualFileStatusSchema,
        body,
      );
      requireManualStatus(result, {
        ...prior,
        request_uuid: input.request_uuid,
      });
      if (
        result.resume_from_job_uuid !== prior.job_uuid ||
        result.resume_from_revision !== input.expected_revision ||
        (prior.publication &&
          JSON.stringify(prior.publication) !==
            JSON.stringify(result.publication))
      )
        throw new NativeArchiveError(0, "mismatched_receipt");
      return result;
    },
    async cancelSaved(prior: ManualFileStatus, body: string) {
      manualCancelSchema.parse(JSON.parse(body));
      return requireManualStatus(
        await request(
          `manual-intake/requests/${uuid.parse(prior.request_uuid)}/cancel`,
          manualFileStatusSchema,
          body,
        ),
        prior,
      );
    },
  };
}
export type ManualIntakeAPI = ReturnType<typeof createManualIntakeAPI>;
