/**
 * Lightbox controller for the Offline view. Mirrors the streaming
 * `useSceneLightbox` API (open / index / slides driven by
 * `onCardPreviewClick(item, allItems, index)`) so the lightbox renders
 * scenes in the same order the grid currently shows — sorted, filtered,
 * paginated by `EntityListPage`. Earlier revisions seeded slides from
 * the raw `entries` array, which was IDB-insertion order and did not
 * match the user's displayed sort.
 *
 * Differences from the online lightbox:
 *   - All entries live in memory (`useOfflineEntries`), so the
 *     paged-sentinel plumbing the streaming hook needs for forward /
 *     backward page loads is not required — slides are scoped to the
 *     items the chrome handed us at click time.
 *   - Each slide carries the `OfflineEntry` directly, which the
 *     `<ActiveOfflineSceneSlide>` branch in `scene-slide-content.tsx`
 *     uses to skip the GraphQL `findScene` round-trip and render the
 *     OPFS file via a blob URL.
 */

import { useCallback, useState } from "react";
import {
  SceneLightbox,
  type SceneSlide,
} from "src/components/lightbox/scene-lightbox";
import type { OfflineCardItem } from "./offline-list-source";

function offlineItemToSlide(item: OfflineCardItem): SceneSlide {
  const entry = item.entry;
  return {
    type: "scene",
    sceneId: entry.scene_id,
    title: entry.title || undefined,
    posterSrc: entry.paths.screenshot ?? undefined,
    offlineEntry: entry,
  };
}

export function useOfflineSceneLightbox() {
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const [lightboxIndex, setLightboxIndex] = useState(0);
  const [slides, setSlides] = useState<SceneSlide[]>([]);

  const onCardPreviewClick = useCallback(
    (item: OfflineCardItem, allItems: OfflineCardItem[], _index: number) => {
      // Only `complete` rows have a usable OPFS file. Filter the page
      // list to playable entries, then resolve the click target's index
      // inside that filtered list (so navigating left/right in the
      // lightbox skips queued / errored / missing rows seamlessly).
      const playable = allItems.filter((i) => i.entry.status === "complete");
      const targetIdx = playable.findIndex((i) => i.id === item.id);
      if (targetIdx < 0) return;
      setSlides(playable.map(offlineItemToSlide));
      setLightboxIndex(targetIdx);
      setLightboxOpen(true);
    },
    [],
  );

  const lightboxElement = lightboxOpen ? (
    <SceneLightbox
      open
      onClose={() => setLightboxOpen(false)}
      slides={slides}
      index={lightboxIndex}
      finite
    />
  ) : null;

  return {
    onCardPreviewClick,
    lightboxElement,
    lightboxOpen,
  };
}
