import { z } from "zod";
import { createArchiveRequest, nativeArchiveEndpoint } from "./client";
import { accountUUIDSchema } from "./account-review-api";

const count = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
export const scrapeMetricsSchema = z.object({
  since: z.string().datetime({ offset: true }),
  metrics: z.array(
    z.object({
      source_scope: z.string(),
      class: z.enum(["routine", "initial", "recovery"]),
      operation: z.enum([
        "listing",
        "profile",
        "media-resolution",
        "other",
        "posts",
      ]),
      requests: count,
      responses: count,
      rate_limited: count,
      errors: count,
      new_posts: count,
      repeated_posts: count,
    }),
  ),
  accounts: z
    .array(
      z.object({
        collection_uuid: accountUUIDSchema,
        label: z.string(),
        url: z.string(),
        last_success_ms: count,
        overdue_seconds: count,
      }),
    )
    .max(50),
  total: count,
});
export function createScrapeMetricsAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  return (offset: number, signal?: AbortSignal) =>
    request(
      `activity/scrape-metrics?offset=${count.parse(offset)}`,
      scrapeMetricsSchema,
      undefined,
      signal,
    );
}
