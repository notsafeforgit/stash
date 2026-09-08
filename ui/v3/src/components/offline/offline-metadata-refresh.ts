/**
 * Refresh-when-online pass for the Offline view.
 *
 * Snapshotted scene fields go stale when the user edits the scene
 * server-side after downloading. On Offline-view mount we re-fetch
 * the latest `findScenes` for every locally-stored scene and patch
 * the IDB rows where anything visible changed. Failures are silent
 * (offline / server unreachable) — the cards keep showing the last-
 * known snapshot.
 *
 * Side effect of the same pass: scenes deleted server-side are
 * marked `server_status: "missing"` so the card can render a
 * "Removed from server" badge. The local file isn't touched —
 * playback still works; only re-download is no longer possible.
 *
 * Caller invokes via `useOfflineMetadataRefresh()` (a hook that fires
 * a single fetch on mount + when the entry list changes substantially).
 */

import { useEffect } from "react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { useApolloClient } from "@apollo/client/react";
import {
  FindScenesDocument,
  type SlimSceneDataFragment,
} from "src/core/generated-graphql";
import { patchEntry, type OfflineEntry } from "./offline-db";

interface RefreshDeps {
  entries: OfflineEntry[];
}

export function useOfflineMetadataRefresh({ entries }: RefreshDeps): void {
  const client = useApolloClient();
  const membership = entries
    .map((entry) => entry.scene_id)
    .sort()
    .join(",");
  const currentEntries = useCommittedRef(entries);

  useEffect(() => {
    const sceneIds = membership
      .split(",")
      .filter((id) => /^[1-9]\d*$/.test(id))
      .map(Number)
      .filter(Number.isSafeInteger);
    if (sceneIds.length === 0) return;
    const requested = new Set(sceneIds.map(String));
    let disposed = false;
    let generation = 0;
    async function refresh() {
      const request = ++generation;
      try {
        const { data } = await client.query({
          query: FindScenesDocument,
          variables: { scene_ids: sceneIds },
          fetchPolicy: "network-only",
        });
        if (disposed || request !== generation || !data?.findScenes) return;
        const fresh = new Map(
          data.findScenes.scenes.map((scene) => [scene.id, scene]),
        );
        for (const entry of currentEntries.current) {
          if (disposed || request !== generation) return;
          if (!requested.has(entry.scene_id)) continue;
          const live = fresh.get(entry.scene_id);
          if (!live) {
            if (entry.server_status !== "missing")
              await patchEntry(entry.scene_id, { server_status: "missing" });
          } else {
            const patch = diffEntry(entry, live);
            if (patch || entry.server_status !== "present") {
              await patchEntry(entry.scene_id, {
                ...patch,
                server_status: "present",
              });
            }
          }
        }
      } catch (error) {
        if (!disposed && import.meta.env.DEV)
          console.debug("[offline] metadata refresh failed:", error);
      }
    }
    const onOnline = () => {
      void refresh();
    };
    onOnline();
    window.addEventListener("online", onOnline);
    return () => {
      disposed = true;
      window.removeEventListener("online", onOnline);
    };
  }, [client, membership]);
}

function diffEntry(
  entry: OfflineEntry,
  live: SlimSceneDataFragment,
): Partial<OfflineEntry> | null {
  const patch: Partial<OfflineEntry> = {};
  if ((live.title ?? "") !== entry.title) {
    patch.title = live.title ?? "";
  }
  // Treat undefined (pre-existing entries from before details was
  // snapshotted) and the empty string as equivalent to null so we
  // don't fire a redundant patch on every refresh once the field
  // settles to no description.
  const liveDetails = live.details ?? null;
  const currentDetails = entry.details ?? null;
  if (liveDetails !== currentDetails) {
    patch.details = liveDetails;
  }
  const liveStudioName = live.studio?.name ?? null;
  const liveStudioId = live.studio?.id ?? null;
  if (liveStudioName !== entry.studio_name) patch.studio_name = liveStudioName;
  if (liveStudioId !== entry.studio_id) patch.studio_id = liveStudioId;
  if ((live.date ?? null) !== entry.date) patch.date = live.date ?? null;

  const livePerformers = (live.performers ?? []).map((p) => ({
    id: p.id,
    name: p.name,
  }));
  if (!sameIdNameList(livePerformers, entry.performers)) {
    patch.performers = livePerformers;
  }

  const liveTags = (live.tags ?? []).map((t) => ({ id: t.id, name: t.name }));
  if (!sameIdNameList(liveTags, entry.tags)) {
    patch.tags = liveTags;
  }

  const liveScreenshot = live.paths?.screenshot ?? null;
  const livePreview = live.paths?.preview ?? null;
  const liveVtt = live.paths?.vtt ?? null;
  if (
    liveScreenshot !== entry.paths.screenshot ||
    livePreview !== entry.paths.preview ||
    liveVtt !== entry.paths.vtt
  ) {
    patch.paths = {
      screenshot: liveScreenshot,
      preview: livePreview,
      sprite: entry.paths.sprite, // not in SlimSceneData fragment
      vtt: liveVtt,
    };
  }

  return Object.keys(patch).length > 0 ? patch : null;
}

function sameIdNameList(
  a: { id: string; name: string }[],
  b: { id: string; name: string }[],
): boolean {
  if (a.length !== b.length) return false;
  const aSorted = [...a].sort((x, y) => x.id.localeCompare(y.id));
  const bSorted = [...b].sort((x, y) => x.id.localeCompare(y.id));
  return aSorted.every(
    (item, index) =>
      item.id === bSorted[index]?.id && item.name === bSorted[index]?.name,
  );
}
