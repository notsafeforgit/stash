import { z } from "zod";
import { accountSchema, accountUUIDSchema as uuid } from "./account-review-api";
import { postLibraryItemSchema, postSummarySchema } from "./source-post-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

export const reviewQueueKindSchema = z.enum(["accounts", "media", "metadata"]);
export const reviewQueueSearchSchema = z.object({
  kind: reviewQueueKindSchema.default("accounts"),
  after: uuid.optional(),
});
export type ReviewQueueKind = z.infer<typeof reviewQueueKindSchema>;
export type ReviewQueueSearch = z.infer<typeof reviewQueueSearchSchema>;

export const reviewQueueReasons = {
  accounts: ["account_owner_undecided"],
  media: [
    "media_unselected",
    "post_link_conflict",
    "attachment_link_undecided",
    "attachment_link_conflict",
    "review_limit",
  ],
  metadata: ["retained_metadata"],
} as const;
const reasonSchema = z.enum([
  ...reviewQueueReasons.accounts,
  ...reviewQueueReasons.media,
  ...reviewQueueReasons.metadata,
]);
const itemSchema = z.strictObject({
  uuid,
  reasons: z.array(reasonSchema).min(1).max(5),
  account: accountSchema.optional(),
  post: postSummarySchema.optional(),
  media: postLibraryItemSchema.optional(),
});
const pageSchema = z.strictObject({
  kind: reviewQueueKindSchema,
  items: z.array(itemSchema).max(25),
  checked: z.number().int().min(0).max(100),
  next: uuid.optional(),
});
export type ReviewQueueItem = z.infer<typeof itemSchema>;
export type ReviewQueueReason = z.infer<typeof reasonSchema>;
export type ReviewQueuePage = z.infer<typeof pageSchema>;

function validItem(kind: ReviewQueueKind, item: ReviewQueueItem) {
  if (
    new Set(item.reasons).size !== item.reasons.length ||
    item.reasons.some(
      (reason) =>
        !reviewQueueReasons[kind].some((allowed) => allowed === reason),
    )
  )
    return false;
  switch (kind) {
    case "accounts":
      return (
        item.account?.uuid === item.uuid &&
        item.account.canonical_uuid === item.uuid &&
        !item.account.redirect_to &&
        (!item.account.ownership ||
          item.account.ownership.state === "undecided") &&
        !item.post &&
        !item.media
      );
    case "media":
      return (
        item.post?.uuid === item.uuid &&
        item.post.requested_uuid === item.uuid &&
        item.post.state === "active" &&
        !item.account &&
        !item.media
      );
    case "metadata":
      return (
        item.media?.uuid === item.uuid &&
        item.media.state === "active" &&
        item.media.kind !== "gallery" &&
        !item.account &&
        !item.post
      );
  }
}

export function createReviewQueueAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  async function page(kind: ReviewQueueKind, after = "", signal?: AbortSignal) {
    reviewQueueKindSchema.parse(kind);
    const query = new URLSearchParams({ limit: String(pageLimit) });
    if (after) query.set("after", uuid.parse(after));
    const result = await request(
      `review-queue/${kind}?${query}`,
      pageSchema,
      undefined,
      signal,
    );
    const invalid = () => new NativeArchiveError(0, "invalid_response");
    if (result.kind !== kind || result.checked < result.items.length)
      throw invalid();
    let previous = after;
    for (const item of result.items) {
      if (item.uuid <= previous || !validItem(kind, item)) throw invalid();
      previous = item.uuid;
    }
    if (
      result.next &&
      (result.checked === 0 ||
        result.next <= after ||
        result.next < previous ||
        (kind !== "media" && result.next !== previous))
    )
      throw invalid();
    if (kind !== "media" && result.checked !== result.items.length)
      throw invalid();
    return result;
  }
  return { endpoint, pageLimit, page };
}
export type ReviewQueueAPI = ReturnType<typeof createReviewQueueAPI>;
