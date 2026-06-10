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

import { useEffect, useRef } from "react";
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
  const lastRunIdsRef = useRef<string>("");

  useEffect(() => {
    if (entries.length === 0) return;
    // Coalesce: don't re-fetch when the entry list hasn't changed
    // membership-wise. The list view re-renders on download progress
    // updates; we don't want a network round-trip on every byte.
    const ids = entries
      .map((e) => e.scene_id)
      .sort()
      .join(",");
    if (ids === lastRunIdsRef.current) return;
    lastRunIdsRef.current = ids;

    let cancelled = false;
    void (async () => {
      try {
        const sceneIds = entries
          .map((e) => parseInt(e.scene_id, 10))
          .filter((n) => Number.isFinite(n));
        const { data } = await client.query({
          query: FindScenesDocument,
          variables: { scene_ids: sceneIds },
          fetchPolicy: "network-only",
        });
        if (cancelled) return;
        const fresh = new Map<string, SlimSceneDataFragment>();
        for (const s of data?.findScenes?.scenes ?? []) {
          fresh.set(s.id, s);
        }
        for (const entry of entries) {
          const live = fresh.get(entry.scene_id);
          if (!live) {
            // Server returned the row-set we asked for; absence
            // means deleted. Mark missing without touching anything
            // else — the local file remains playable.
            if (entry.server_status !== "missing") {
              await patchEntry(entry.scene_id, { server_status: "missing" });
            }
            continue;
          }
          const patch = diffEntry(entry, live);
          if (patch) {
            await patchEntry(entry.scene_id, {
              ...patch,
              server_status: "present",
            });
          } else if (entry.server_status !== "present") {
            await patchEntry(entry.scene_id, { server_status: "present" });
          }
        }
      } catch (err) {
        // Silent: offline / server unreachable / transient. Cards
        // continue rendering the last-known snapshot. Log under
        // debug so it's available for diagnostics without spamming
        // the console for a normal "user is on a plane" situation.
        if (import.meta.env?.DEV) {
          console.debug("[offline] metadata refresh failed:", err);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [client, entries]);
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
  for (let i = 0; i < aSorted.length; i++) {
    if (aSorted[i].id !== bSorted[i].id) return false;
    if (aSorted[i].name !== bSorted[i].name) return false;
  }
  return true;
}
