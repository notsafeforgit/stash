import { useCallback, useMemo } from "react";
import { createSourceAlbumAPI } from "@/core/native-archive/source-album-api";
import { useAlbumPages } from "./read";

// The summary and Source albums tab share one bounded, paginated lookup.
export function useGallerySources(localId: string) {
  const api = useMemo(() => createSourceAlbumAPI(), []);
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      const identity = await api.identity(localId, signal);
      const page = await api.posts(identity.uuid, after, signal);
      return {
        signature: JSON.stringify(page.gallery),
        items: page.posts,
        next:
          page.posts.length === api.pageLimit
            ? (page.posts.at(-1)?.uuid ?? null)
            : null,
        header: page,
      };
    },
    [api, localId],
  );
  return { ...useAlbumPages(load), endpoint: api.endpoint };
}

export type GallerySources = ReturnType<typeof useGallerySources>;
