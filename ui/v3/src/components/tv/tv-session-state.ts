import type { ApolloClient } from "@apollo/client";
import type { TvFeedSnapshot } from "@/core/tv/feed-state";

const snapshots = new WeakMap<ApolloClient, Map<string, TvFeedSnapshot>>();
export function readTvSession(client: ApolloClient, identity: string) {
  return snapshots.get(client)?.get(identity);
}
export function saveTvSession(
  client: ApolloClient,
  identity: string,
  snapshot: TvFeedSnapshot,
) {
  let cache = snapshots.get(client);
  if (!cache) {
    cache = new Map();
    snapshots.set(client, cache);
  }
  cache.delete(identity);
  cache.set(identity, snapshot);
  while (cache.size > 3) {
    const key = cache.keys().next().value;
    if (key === undefined) break;
    cache.delete(key);
  }
}
