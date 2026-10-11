import { z } from "zod";
import { createArchiveRequest, nativeArchiveEndpoint } from "./client";

export type ArchivePageQuery = {
  resource:
    | "media-roots"
    | "collections"
    | "accounts"
    | "posts"
    | "jobs"
    | "runs"
    | "job-attempts"
    | "run-attempts"
    | "imports"
    | "root-history"
    | "collection-history"
    | "policy-history"
    | "provider-history"
    | "shares"
    | "review-accounts"
    | "review-media"
    | "review-metadata";
  subject?: string;
  cursor?: string | number;
  q?: string;
  state?: string;
  kind?: string;
  namespace?: string;
  ownership?: string;
  scope?: string;
  uuid?: string;
  url?: string;
  value?: string;
  collection?: string;
};
const count = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
export const archivePageInfoSchema = z
  .strictObject({ total: count, offset: count })
  .refine((info) => info.offset <= info.total);
export type ArchivePageInfo = z.infer<typeof archivePageInfoSchema>;

export function archivePageInfoPath(query: ArchivePageQuery) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (
      value !== undefined &&
      value !== "" &&
      !(key === "ownership" && value === "all")
    )
      params.set(key, String(value));
  }
  params.sort();
  return `page-info?${params}`;
}

export function createArchivePaginationAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  return (path: string, signal?: AbortSignal) =>
    request(path, archivePageInfoSchema, undefined, signal);
}
