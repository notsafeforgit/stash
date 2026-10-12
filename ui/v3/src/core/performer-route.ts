import { redirect, type ParsedLocation } from "@tanstack/react-router";
import { createMetadataReviewAPI } from "./native-archive/metadata-review-api";
import { NativeArchiveError } from "./native-archive/client";

/** Keep database keys out of the address bar while retaining numeric bookmarks.
 * Resolving on entry also follows performer merges without stale alias caches. */
export async function resolvePerformerRoute(
  id: string,
  location: Pick<ParsedLocation, "search" | "hash" | "state">,
  signal?: AbortSignal,
) {
  let identity;
  try {
    identity = await createMetadataReviewAPI().identity(
      "performer",
      id,
      signal,
    );
  } catch (error) {
    if (error instanceof NativeArchiveError && error.status === 404)
      return null;
    throw error;
  }
  if (id !== identity.uuid)
    throw redirect({
      to: "/performers/$performerId",
      params: { performerId: identity.uuid },
      search: location.search,
      hash: location.hash,
      state: location.state,
      replace: true,
    });
  return String(identity.local_id);
}
