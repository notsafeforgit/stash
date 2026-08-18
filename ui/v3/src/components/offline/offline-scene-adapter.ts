/**
 * Adapt an `OfflineEntry` into a `SceneDataFragment`-shaped object so
 * the regular `<ScenePlayer>` + `<SceneDetailContent>` components can
 * render an offline scene with no special-casing on their side. The
 * adapter sits at the boundary between IDB-snapshotted scene metadata
 * (a small subset of the GraphQL `Scene` type) and the deep, fully-
 * typed shape the detail components consume.
 *
 * Trade-off: the snapshot only carries the player-essential fields
 * (title, performer / tag names + ids, studio name + id, screenshot
 * path, file dimensions / codecs / duration). Fields the GraphQL
 * shape requires but the snapshot doesn't carry are filled with
 * sensible empties (`""`, `0`, `[]`, `null`). Read-only mode in the
 * detail page hides every surface that would otherwise read or
 * mutate those empties (edit form, marker editor, mutating actions),
 * so the holes are invisible to the user.
 *
 * The cast to `SceneDataFragment` is the price for not snapshotting
 * the full graph at download time. If we ever do that snapshot — to
 * support, say, full marker / performer detail in the offline view —
 * the adapter shrinks to almost nothing and the cast can come off.
 */

import type * as GQL from "src/core/generated-graphql";
import type { OfflineEntry } from "./offline-db";

/**
 * Build a `SceneDataFragment` from an `OfflineEntry` plus a resolved
 * blob URL pointing at the OPFS file. The blob URL becomes the only
 * `sceneStreams` entry; the existing player source machinery picks
 * it up via the `blob:` exception in `filterSources`.
 *
 * `paths.stream` carries the same blob URL so anything that reads
 * the legacy `paths.stream` field (e.g. older list-card preview
 * code) lands on the offline file too.
 */
export function offlineEntryToSceneData(
  entry: OfflineEntry,
  blobUrl: string,
): GQL.SceneDataFragment {
  const downloadedAtIso = new Date(
    entry.downloaded_at || Date.now(),
  ).toISOString();

  // Performer / tag / studio entries: snapshot carries id + name. The
  // full GraphQL shape wants many more fields per entity (counts,
  // image paths, stash_ids, etc.). Filled with empty defaults so the
  // type checker is satisfied without us having to fetch live data
  // for every offline scene.
  const performers = entry.performers.map((p) => ({
    __typename: "Performer" as const,
    id: p.id,
    name: p.name,
    created_at: downloadedAtIso,
    updated_at: downloadedAtIso,
    disambiguation: null,
    urls: null,
    gender: null,
    birthdate: null,
    ethnicity: null,
    country: null,
    eye_color: null,
    height_cm: null,
    measurements: null,
    fake_tits: null,
    penis_length: null,
    circumcised: null,
    career_start: null,
    career_end: null,
    tattoos: null,
    piercings: null,
    favorite: false,
    ignore_auto_tag: false,
    ignore_primary_name_auto_tag: false,
    image_path: null,
    scene_count: 0,
    image_count: 0,
    gallery_count: 0,
    group_count: 0,
    performer_count: 0,
    o_counter: null,
    rating100: null,
    details: null,
    death_date: null,
    hair_color: null,
    weight: null,
    custom_fields: {},
    aliases: [],
    tags: [],
    stash_ids: [],
  }));

  const tags = entry.tags.map((t) => ({
    __typename: "Tag" as const,
    id: t.id,
    name: t.name,
    created_at: downloadedAtIso,
    updated_at: downloadedAtIso,
    sort_name: null,
    aliases: [],
    image_path: null,
    parent_count: 0,
    child_count: 0,
    stash_ids: [],
  }));

  const studio = entry.studio_id
    ? {
        __typename: "Studio" as const,
        id: entry.studio_id,
        name: entry.studio_name ?? "",
        created_at: downloadedAtIso,
        updated_at: downloadedAtIso,
        image_path: null,
        details: null,
        rating100: null,
        aliases: [],
        favorite: false,
        ignore_auto_tag: false,
        organized: false,
        o_counter: null,
        stash_ids: [],
        parent_studio: null,
        tags: [],
      }
    : null;

  // Single-file offline scene: dimensions + codecs come from the
  // entry; everything else (path, fingerprints) is best-effort.
  const file = {
    __typename: "VideoFile" as const,
    id: `offline-${entry.scene_id}`,
    // Prefer the snapshot of the source file's server path so
    // `objectTitle(scene)` falls back to the source filename for
    // untitled scenes (matches the streaming view). The previous
    // `entry.title || entry.scene_id` fallback put the scene id in
    // the path field, which then surfaced in the file-info tab and
    // in the title bar for untitled scenes.
    path: entry.source_file_path || entry.title || entry.scene_id,
    size: entry.bytes || 0,
    mod_time: downloadedAtIso,
    updated_at: downloadedAtIso,
    duration: entry.duration || 0,
    video_codec: entry.source_video_codec || "",
    audio_codec: entry.source_audio_codec || "",
    duration_mismatch: false,
    width: entry.width_actual || entry.width || 0,
    height: entry.height_actual || entry.height || 0,
    frame_rate: 0,
    bit_rate: 0,
    fingerprints: [],
  };

  return {
    __typename: "Scene",
    id: entry.scene_id,
    title: entry.title,
    code: null,
    details: entry.details ?? null,
    director: null,
    urls: [],
    date: entry.date,
    rating100: null,
    o_counter: null,
    organized: false,
    interactive: false,
    interactive_speed: null,
    created_at: downloadedAtIso,
    updated_at: downloadedAtIso,
    resume_time: entry.last_position_seconds ?? null,
    last_played_at: null,
    play_duration: null,
    play_count: null,
    play_history: [],
    o_history: [],
    custom_fields: {},
    captions: [],
    files: [file],
    paths: {
      __typename: "ScenePathsType",
      screenshot: entry.paths.screenshot,
      preview: entry.paths.preview,
      stream: blobUrl,
      webp: null,
      vtt: null,
      sprite: null,
      funscript: null,
      interactive_heatmap: null,
      caption: null,
    },
    scene_markers: [],
    galleries: [],
    studio,
    groups: [],
    tags,
    performers,
    stash_ids: [],
    sceneStreams: [
      {
        __typename: "SceneStreamEndpoint",
        url: blobUrl,
        mime_type: "video/mp4",
        label: "Offline",
      },
    ],
    // SceneDataFragment includes a `__typename` inferred from the
    // root selection; the cast below covers any drift between the
    // snapshot's nested entity shape (Performer/Tag/Studio counts,
    // stash_ids, etc.) and the live GraphQL shape.
  } as GQL.SceneDataFragment;
}
