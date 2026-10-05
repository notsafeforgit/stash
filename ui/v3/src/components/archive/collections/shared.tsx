import { useMsg } from "@/hooks/message";
import type { Collection } from "@/core/native-archive/collection-api";

export function useCollectionLabels() {
  const msg = useMsg();
  const kinds: Record<Collection["kind"], string> = {
    account: msg("collections.kind_account", "Account"),
    feed: msg("collections.kind_feed", "Feed"),
    subreddit: msg("collections.kind_subreddit", "Subreddit"),
    search: msg("collections.kind_search", "Search"),
    manual_batch: msg("collections.kind_manual_batch", "Manual batch"),
    directory: msg("collections.kind_directory", "Folder"),
    legacy_catalog: msg("collections.kind_legacy_catalog", "Imported catalog"),
    collection: msg("collections.kind_collection", "Collection"),
  };
  const states: Record<Collection["state"], string> = {
    active: msg("collections.state_active", "Active"),
    disabled: msg("collections.state_disabled", "Disabled"),
    retired: msg("collections.state_retired", "Retired"),
  };
  return { kinds, states };
}
