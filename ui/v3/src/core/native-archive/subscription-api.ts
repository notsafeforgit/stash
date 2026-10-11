import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { collectionSchema } from "./collection-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

const subscriptionSchema = z.object({
  collection: collectionSchema,
  revision: z.number().int().nonnegative(),
  enabled: z.boolean(),
});
const pageSchema = z.object({
  items: z.array(subscriptionSchema).max(25),
  total: z.number().int().nonnegative(),
});
export type SourceSubscription = z.infer<typeof subscriptionSchema>;
export type SubscriptionScope = "performer" | "account" | "collection";

export function createSubscriptionAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  async function list(
    scope: SubscriptionScope,
    id: string,
    page: number,
    signal?: AbortSignal,
  ) {
    z.number().int().min(1).max(40001).parse(page);
    const query = new URLSearchParams({
      [scope]: uuid.parse(id),
      limit: "25",
      offset: String((page - 1) * 25),
    });
    const result = await request(
      `subscriptions?${query}`,
      pageSchema,
      undefined,
      signal,
    );
    if (
      scope === "collection" &&
      result.items.some((item) => item.collection.uuid !== id)
    )
      throw new NativeArchiveError(0, "subscription_mismatch");
    return result;
  }
  return {
    list,
    async save(item: SourceSubscription, enabled: boolean) {
      const id = item.collection.uuid;
      try {
        const result = await request(
          `collections/${uuid.parse(id)}/subscription`,
          subscriptionSchema,
          JSON.stringify({ expected_revision: item.revision, enabled }),
          undefined,
          "PUT",
        );
        if (
          result.collection.uuid !== id ||
          result.enabled !== enabled ||
          result.revision !== item.revision + Number(item.enabled !== enabled)
        )
          throw new NativeArchiveError(0, "subscription_mismatch");
        return result;
      } catch (error) {
        // Recover a lost response without toggling again or overwriting a later edit.
        const current = (await list("collection", id, 1)).items[0];
        if (
          current?.enabled === enabled &&
          current.revision === item.revision + Number(item.enabled !== enabled)
        )
          return current;
        throw error;
      }
    },
    stopPerformer: (id: string) =>
      request(
        `entities/${uuid.parse(id)}/stop-monitoring`,
        z.object({ stopped: z.number().int().nonnegative() }),
        "{}",
      ),
  };
}
