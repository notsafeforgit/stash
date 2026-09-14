import type { ApolloClient } from "@apollo/client";
import { FindSavedFilterDocument } from "@/core/generated-graphql";
import type { FrontPageContent } from "@/core/config";

/** Resolve only configured row definitions, so the first Home paint knows each
 * row's cover geometry. Entity queries still wait until their row is nearby. */
export async function preloadFrontPage(
  client: ApolloClient,
  rows: FrontPageContent[] | undefined,
) {
  const ids = new Set(
    rows?.flatMap((row) =>
      row.__typename === "SavedFilter" ? [String(row.savedFilterId)] : [],
    ),
  );
  // A failed warm-up must not block Home; the row owns its retry/error state.
  await Promise.allSettled(
    [...ids].map((id) =>
      client.query({
        query: FindSavedFilterDocument,
        variables: { id },
        fetchPolicy: "cache-first",
      }),
    ),
  );
}
