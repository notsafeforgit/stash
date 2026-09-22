import { useCallback, useEffect, useState } from "react";
import { z } from "zod";
import type { SharedContent, SharedMedia } from "./share-contract";

export const shareTabSchema = z.enum(["scenes", "images", "galleries"]);
export type ShareTab = z.infer<typeof shareTabSchema>;

const locationSchema = z.object({
  tab: shareTabSchema.optional().catch(undefined),
  gallery: z
    .string()
    .regex(/^gallery-[1-9][0-9]*$/)
    .optional()
    .catch(undefined),
  media: z
    .string()
    .regex(/^(scene|image)-[1-9][0-9]*$/)
    .optional()
    .catch(undefined),
});
export type ShareLocation = z.infer<typeof locationSchema>;

export type SharedMediaItem = SharedMedia & { id: string };
export interface SharedGallery {
  id: string;
  kind: "GALLERY";
  title: string;
  media: SharedMediaItem[];
}
export type SharedItem = SharedMediaItem | SharedGallery;

export function shareCollections(content: SharedContent) {
  const media = content.media.map((item) => ({ ...item, id: item.key }));
  const byKey = new Map(media.map((item) => [item.key, item]));
  const galleries: SharedGallery[] = content.entries.flatMap((entry, index) =>
    entry.kind === "GALLERY"
      ? [
          {
            id: `gallery-${index + 1}`,
            kind: "GALLERY",
            title: entry.title,
            media: entry.media_keys.flatMap((key) => {
              const item = byKey.get(key);
              return item ? [item] : [];
            }),
          },
        ]
      : [],
  );
  return {
    scenes: media.filter((item) => item.kind === "SCENE"),
    images: media.filter((item) => item.kind === "IMAGE"),
    galleries,
    byKey,
  };
}

export function readShareLocation(search: string): ShareLocation {
  return locationSchema.parse(Object.fromEntries(new URLSearchParams(search)));
}

export function shareLocationURL(base: URL, location: ShareLocation) {
  const url = new URL(base);
  for (const [key, value] of Object.entries(location)) {
    if (value !== undefined) url.searchParams.set(key, value);
  }
  return `${url.pathname}${url.search}`;
}

const historySchema = z.object({
  stashShareDepth: z.number().int().nonnegative(),
});

/** Public navigation stays under the share URL; the fragment is only a grant. */
export function useShareNavigation(base: URL) {
  const [location, setLocation] = useState(() =>
    readShareLocation(window.location.search),
  );
  useEffect(() => {
    const onPopState = () =>
      setLocation(readShareLocation(window.location.search));
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);
  const navigate = useCallback(
    (next: ShareLocation, replace = false) => {
      const current = historySchema.safeParse(window.history.state);
      const depth = current.success ? current.data.stashShareDepth : 0;
      window.history[replace ? "replaceState" : "pushState"](
        { stashShareDepth: replace ? depth : depth + 1 },
        "",
        shareLocationURL(base, next),
      );
      setLocation(next);
    },
    [base],
  );
  const back = useCallback(
    (parent: ShareLocation) => {
      const current = historySchema.safeParse(window.history.state);
      if (current.success && current.data.stashShareDepth > 0)
        window.history.back();
      else navigate(parent, true);
    },
    [navigate],
  );
  return { location, navigate, back };
}
