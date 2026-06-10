# Offline (in-PWA scene downloads)

This document specifies the design for in-PWA scene downloads — a top-level "Offline" view in v3 where users can download scenes to the device's PWA storage for offline playback. Scenes only for now; images / galleries / etc. are out of scope.

## Goals

- One-tap download from any scene's actions menu and from scene cards' context menus.
- Files stored inside the PWA via OPFS (Origin Private File System) so they survive PWA reload, are sandboxed per-origin, and don't pollute the user's general device storage until explicitly exported.
- Format chosen per-device for efficiency: codec-copy when the source's codecs are decodable on the device, otherwise transcode to the most efficient codec the device supports (HEVC if both client-decode and server-encode are available, else H.264).
- Resolution capped by a user-configurable max ("Original" / 4K / 1440p / 1080p / 720p / 480p / 240p) shared with the existing streaming-resolution setting where it makes sense.
- Re-downloading the same scene at a different resolution **replaces** the existing local copy (one entry per scene).
- Downloads serialize: at most one in-flight at a time. Subsequent requests queue.
- "Save to Files" exports a downloaded entry out of the PWA into the user's regular device storage via the platform's file save dialog.

## Non-goals

- Background fetch / continuing downloads while the PWA is closed. Browser support is too patchy (Chrome only via Background Fetch API; Safari has nothing). Foreground only; clearly labelled in the UI.
- Multi-version per scene (one downloaded copy per scene; resolution is part of the metadata, replacing on re-download).
- Auto-eviction by Stash. The browser may evict under storage pressure (especially iOS); we request `navigator.storage.persist()` and surface eviction warnings, but Stash itself never deletes a download the user didn't explicitly remove.
- Sharing downloads between devices. Each device's PWA has its own OPFS — downloads do not sync.
- Selective audio track / subtitle picking. Each download is a single MP4 with one video track and one audio track (whatever the source's primary tracks are, with audio re-encoded to AAC if needed for MP4 compatibility).
- Bulk save / multi-file zip export. Each downloaded entry is exportable on its own (per-card menu + a Save button on the offline player); chaining them into a single archive isn't worth the complexity for the use case.

## High-level data flow

```
┌─────────────────────────────────────────────────────────────────┐
│  Frontend (PWA)                                                 │
│                                                                 │
│  scene-card / scene-actions-menu                                │
│        │                                                        │
│        │ "Download" click                                       │
│        ▼                                                        │
│  useDownloadQueue.enqueue(scene)                                │
│        │                                                        │
│        ▼                                                        │
│  pickFormat(scene, deviceCapabilities, settings)                │
│        │                                                        │
│        │  ?format=copy|copy-aac|hevc|h264                       │
│        │  &resolution=ORIGINAL|FOUR_K|FULL_HD|...               │
│        ▼                                                        │
│  fetch('/scene/{id}/download?...')                              │
│  pipeTo OPFS file writer (via streams API)                      │
│        │                                                        │
│        │ progress events update IndexedDB metadata row          │
│        ▼                                                        │
│  IndexedDB('offline'): { scene_id, format, resolution,          │
│                          bytes, status, error, opfsPath }       │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│  Backend (Go)                                                   │
│                                                                 │
│  GET /scene/{id}/download?format=...&resolution=...             │
│        │                                                        │
│        ▼                                                        │
│  routes_scene.go → ServeDownload                                │
│        │                                                        │
│        ▼                                                        │
│  pkg/ffmpeg/download.go:                                        │
│    - format=copy:     -c:v copy -c:a copy -f mp4 -movflags ...  │
│    - format=copy-aac: -c:v copy -c:a aac  -f mp4 -movflags ...  │
│    - format=hevc:     -c:v <hevc-encoder> -c:a aac -f mp4 ...   │
│    - format=h264:     -c:v <h264-encoder> -c:a aac -f mp4 ...   │
│    All resolution-clamped via -vf scale filter.                 │
│  ffmpeg stdout → http.ResponseWriter (chunked transfer).        │
└─────────────────────────────────────────────────────────────────┘
```

## Storage layer

### OPFS for media bytes

Each downloaded scene gets one file at OPFS path `scenes/{scene_id}.mp4`. Streams are written via:

```ts
const root = await navigator.storage.getDirectory();
const scenesDir = await root.getDirectoryHandle("scenes", { create: true });
const handle = await scenesDir.getFileHandle(`${sceneId}.mp4`, { create: true });
const writable = await handle.createWritable();          // FileSystemWritableFileStream
await fetchResponse.body!.pipeTo(writable);              // streaming, no buffering in JS
```

`pipeTo` keeps memory flat (browser handles backpressure) so multi-GB files work without tab crashes on iOS.

### IndexedDB for metadata

OPFS doesn't store rich metadata, so a sibling IndexedDB table tracks each entry:

```ts
interface OfflineEntry {
  scene_id: string;             // primary key
  // Snapshot of scene fields at download time so the Offline view doesn't
  // need a fresh GraphQL fetch to render. Refreshed on Offline-view mount
  // when the server is reachable — see "Metadata refresh".
  title: string;
  studio_name: string | null;
  studio_id: string | null;
  performers: { id: string; name: string }[];
  tags: { id: string; name: string }[];
  duration: number;
  width: number;
  height: number;
  date: string | null;
  paths: {
    screenshot: string | null;  // remote URLs; fall back to placeholder if offline
    preview: string | null;
    sprite: string | null;
    vtt: string | null;
  };
  // Local playback state.
  last_position_seconds?: number;
  // Download metadata.
  format: "copy" | "copy-aac" | "hevc" | "h264";
  source_video_codec: string;
  source_audio_codec: string;
  resolution: string;           // "ORIGINAL" | "FOUR_K" | ...
  width_actual: number;         // post-scale
  height_actual: number;
  bytes: number;
  downloaded_at: number;        // unix ms
  status: "queued" | "downloading" | "complete" | "error";
  bytes_downloaded?: number;    // live during downloading
  error?: string;
  opfs_path: string;            // "scenes/{scene_id}.mp4"
  // Result of the most recent metadata-refresh pass; used by the card to
  // surface "Removed from server" badges. "unknown" until first refresh.
  server_status: "present" | "missing" | "unknown";
}
```

Object-store: `offline_scenes`, key path `scene_id`. Indexed on `downloaded_at` (for chronological list view) and `status` (for resuming queue on PWA reopen).

### Why not IndexedDB-only?

Storing the binary as a Blob in IndexedDB also works on every target browser, but it forces the whole file through V8's heap on read (no streaming reads of partial Blobs from IDB) and writes accumulate in memory until commit on some implementations. OPFS gives true streaming both ways and is the path the platform vendors are investing in. Keep IDB for metadata only.

### Persistence

On first download, call `navigator.storage.persist()`. If denied, surface a UI banner: "Browser may evict downloads under storage pressure. Add Stash to your Home Screen for better persistence." (iOS only grants persistent storage to PWAs added to Home Screen.)

Surface `navigator.storage.estimate()` in the Offline view header: "Using 4.2 GB of ~12 GB available."

## Server endpoint

### Route

```
GET /scene/{id}/download
    ?format=copy|copy-aac|hevc|h264
    &resolution=ORIGINAL|FOUR_K|...
    [&apikey=...]
```

Wired in `internal/api/routes_scene.go` next to the existing stream routes. Returns:
- `200 OK` with `Content-Type: video/mp4`, `Content-Disposition: attachment; filename="{title}.mp4"`, no `Content-Length` (chunked).
- `400 Bad Request` for invalid format/resolution combos.
- `503 Service Unavailable` if the requested encoder isn't available (e.g. `hevc` requested but no HEVC encoder configured).

### ffmpeg orchestration

New file `pkg/ffmpeg/download.go` with a `ServeDownload(w http.ResponseWriter, r *http.Request, opts DownloadOptions)` similar in shape to `ServeSegment` but writing to the response directly. ffmpeg pipes stdout → `http.ResponseWriter` via `cmd.Stdout = w` and we flush periodically. On client disconnect, kill ffmpeg via `r.Context().Done()`.

Per-format args:

```go
// copy: source codecs ride in MP4 cleanly, pure remux
//   -c:v copy -c:a copy -movflags +faststart+frag_keyframe -f mp4 pipe:1

// copy-aac: video copies, audio re-encodes to AAC (Opus → AAC etc.)
//   -c:v copy -c:a aac -ac 2 -af asetpts=PTS-STARTPTS
//   -movflags +faststart+frag_keyframe -f mp4 pipe:1

// hevc: HEVC transcode (HW where available)
//   -c:v <hevc_qsv|hevc_nvenc|libx265> -preset/quality/crf appropriate
//   -vf "scale=if-needed,setpts=PTS-STARTPTS"
//   -c:a aac -ac 2 -movflags +faststart+frag_keyframe -f mp4 pipe:1

// h264: H.264 transcode (HW where available)
//   -c:v <h264_qsv|h264_nvenc|libx264> ...
//   (same shape as hevc, different codec)
```

`+faststart` is a one-shot post-process that moves the moov atom to the front; doesn't work with `pipe:1` directly because faststart needs to seek backward. **Use `+frag_keyframe+empty_moov` instead** — emits a fragmented MP4 progressively, no post-process seek required, players (including iOS) handle these fine for offline playback. This was the same trade-off that drove the HLS pipeline to fMP4.

### Codec resolution server-side

A new helper `pkg/ffmpeg/encoder_capabilities.go`:

```go
type EncoderSet struct {
    H264 VideoCodec    // always non-zero (libx264 fallback)
    HEVC VideoCodec    // zero value if no HEVC encoder available
}

// Probe at server startup. Tests each candidate via `ffmpeg -hide_banner -encoders`
// and a tiny encode trial to filter out encoders that exist but can't run
// (e.g. `hevc_qsv` listed but no QSV device present).
func ProbeAvailableEncoders(ctx context.Context, ff *FFMpeg) EncoderSet
```

`H264` candidates in priority: `h264_qsv`, `h264_nvenc`, `h264_vaapi`, `h264_videotoolbox`, `libx264`. (Mirrors the existing HW-pick logic in `pkg/ffmpeg/codec.go:hwCodecHLSCompatible`.)

`HEVC` candidates in priority: `hevc_qsv`, `hevc_nvenc`, `hevc_vaapi`, `hevc_videotoolbox`, `libx265`. `libx265` is the universal-CPU fallback; we'll only fall back to it on systems with capable CPU because real-time 4K HEVC encode on libx265 even at `ultrafast` is borderline. The server probe records what's actually viable; if nothing usable, `HEVC` is zero and the client never sees `hevc` as an option.

Exposed via GraphQL on the existing `Configuration` query so the client can include / exclude `hevc` from format selection without a separate fetch:

```graphql
type Configuration {
  ...
  serverCapabilities: ServerCapabilities!
}
type ServerCapabilities {
  downloadFormats: [String!]!     # ["copy", "copy-aac", "h264"] or [..., "hevc"]
}
```

## Codec selection (client)

`src/components/offline/pick-download-format.ts`:

```ts
type DeviceCaps = {
  decodes: { av1: boolean; hevc: boolean; h264: boolean };
  decodesAudio: { aac: boolean; opus: boolean };
};

type ServerCaps = {
  encodes: { hevc: boolean; h264: boolean };
};

type SelectedDownload = {
  format: "copy" | "copy-aac" | "hevc" | "h264";
  resolution: StreamingResolution;     // capped by user setting
};

function pickDownloadFormat(
  scene: Scene,
  device: DeviceCaps,
  server: ServerCaps,
  maxResolution: StreamingResolution,
): SelectedDownload {
  // Effective resolution: source resolution clamped by user max.
  const resolution = clampResolution(scene, maxResolution);
  const willScale = effectiveResolution !== "ORIGINAL"
                 && resolutionSmallerThanSource(resolution, scene);

  // 1. Pure copy when the source codecs land in MP4 untouched and the device
  //    decodes both. No scaling possible (codec-copy can't filter), so this
  //    only applies when no scale is needed.
  if (!willScale
      && device.decodes[scene.video_codec]
      && device.decodesAudio[scene.audio_codec]
      && audioRidesInMp4(scene.audio_codec)) {
    return { format: "copy", resolution };
  }

  // 2. Video-copy + AAC re-encode. Same scale-precludes-copy constraint on
  //    video, but audio gets re-encoded so source-audio support doesn't
  //    matter.
  if (!willScale && device.decodes[scene.video_codec]) {
    return { format: "copy-aac", resolution };
  }

  // 3. Transcode. Pick the most efficient codec the device decodes AND the
  //    server can encode.
  if (device.decodes.hevc && server.encodes.hevc) {
    return { format: "hevc", resolution };
  }
  return { format: "h264", resolution };
}
```

`device.decodes.*` reuses the same MMS `isTypeSupported` probe used for streaming source selection (`useCodecsDecodableInMp4`).

For phase 1 the server doesn't expose HEVC so the `hevc` branch never fires. Phase 2 lights it up.

## Frontend structure

### Routes

New file `src/routes/offline.tsx` registered as a top-level TanStack route. Lists the contents of IndexedDB `offline_scenes`, sorted by `downloaded_at` desc by default (sortable in the UI).

### Nav entry

Add to `BUILTIN_NAV_ITEMS` in `src/components/layout/nav-items.tsx`:

```tsx
{
  label: "Offline",
  icon: <Download className="size-4" />,
  to: "/offline",
  hotkey: "g o",
}
```

Goes after Tags (last current entry). Also surfaces in `mobile-nav-sheet.tsx` and `bottom-tab-bar.tsx` automatically since they consume `useNavItems`.

### List view

`src/components/offline/offline-scene-list.tsx`:

- Uses the same `SceneCard` component as the regular scene list. Card data is built from the IndexedDB `OfflineEntry` (we snapshot scene fields at download time so the card renders without a GraphQL fetch).
- Card preview / poster comes from the snapshotted `paths.screenshot` URL when online; falls back to a static placeholder when offline (we don't snapshot binary preview data — adds significant storage cost for marginal value).
- Tapping a card opens scene playback against the local OPFS file (see "Playback from OPFS" below).
- Per-card context menu: "Save to Files…", "Re-download", "Delete".
- Bulk-select the same way regular scene cards do, with a bulk-action menu offering "Save to Files…" (zips multiple files), "Delete".
- Sticky header shows total size used and queue status ("3 queued, 1 downloading: 412 MB / 1.2 GB").

### Context menu integration

Two sites add a "Download" entry:

1. **Scene card context menu** (`src/components/cards/scene-card.tsx:contextMenu`) — add a `<ContextMenuItem onClick={() => enqueueDownload(scene)}>Download</ContextMenuItem>` after the existing items, separator above.
2. **Scene detail actions menu** (`src/components/detail/scene-actions-menu.tsx`) — same, in the dropdown.

Both call `useDownloadQueue().enqueue(scene)` from a shared hook (see below). Disabled with a tooltip when an entry already exists in `complete` status (the menu item then reads "Re-download" to make the replace behaviour explicit).

### Download queue

`src/components/offline/use-download-queue.ts`:

```ts
type QueueState = {
  queue: { sceneId: string; status: "queued" | "downloading" }[];
  active: { sceneId: string; bytesDownloaded: number; bytesTotal: number | null } | null;
};

interface UseDownloadQueue {
  state: QueueState;
  enqueue(scene: Scene, options?: { force?: boolean }): Promise<void>;
  cancel(sceneId: string): void;
  retry(sceneId: string): void;
  remove(sceneId: string): Promise<void>;     // deletes OPFS file + IDB row
}
```

Implementation:
- Singleton state in a Zustand-style store (or React Context with reducer; pattern already used elsewhere in v3 — pick existing convention).
- One worker promise pumps the queue: `while (queue.length) { await downloadOne(queue.shift()) }`.
- `enqueue` adds to queue if not already present; if a `downloading` or `queued` entry exists for the same scene the call is a no-op (returns same promise).
- `enqueue(scene, { force: true })` wipes the existing entry (OPFS + IDB) before queuing — used by the Re-download path when the user picks a different resolution.
- On PWA reload, scan IDB for `status: "downloading"` rows; mark them `error: "Interrupted by reload"` and let the user retry. (No partial-resume; downloads start over. Range-resume is a phase 3 enhancement.)
- All progress + status writes go to IDB via a debounced writer (1 Hz) to avoid trashing the browser's transaction log.

### Playback from OPFS

`src/components/offline/offline-scene-player.tsx` reuses `<ScenePlayer>` but with a constructed scene whose `streams` field points at a Blob URL backed by the local file:

```ts
const handle = await scenesDir.getFileHandle(`${id}.mp4`);
const file = await handle.getFile();
const url = URL.createObjectURL(file);              // blob: URL, no copy
```

Pass that URL through as the only direct stream source. Lifetime: revoke when the player unmounts or the route changes. The existing player components route blob URLs through the plain `<Video>` element since they aren't HLS playlists, so no further changes needed in the player.

Resume time: stored in IDB on the offline entry, NOT pushed back to the server. Offline playback is intentionally a local-only experience — sync would require a queued-mutation pipeline that doesn't exist in v3 yet, and the value is marginal (the user knows they were watching offline). On unmount, write `last_position_seconds` to the row.

### "Save to Files" button

`src/components/offline/save-to-files.ts`:

```ts
async function saveToFiles(entry: OfflineEntry) {
  const handle = await scenesDir.getFileHandle(entry.opfs_path);
  const file = await handle.getFile();

  // Tier 1: File System Access API (desktop Chrome).
  if ("showSaveFilePicker" in window) {
    const out = await window.showSaveFilePicker({
      suggestedName: `${entry.title}.mp4`,
      types: [{ description: "MP4 Video", accept: { "video/mp4": [".mp4"] } }],
    });
    const writable = await out.createWritable();
    await file.stream().pipeTo(writable);
    return;
  }

  // Tier 2: <a download> blob URL (Safari, Firefox). On iOS this triggers the
  // share sheet, which lets the user save to Files / send via AirDrop / etc.
  const url = URL.createObjectURL(file);
  const link = document.createElement("a");
  link.href = url;
  link.download = `${entry.title}.mp4`;
  document.body.append(link);
  link.click();
  link.remove();
  // Revoke on next tick so the browser can read the URL.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
```

A "Save to Files" button is also surfaced in the offline player route's header so it's one tap during playback, not just a context-menu entry. Bulk save isn't supported — see Non-goals.

## Settings additions

`src/routes/settings.tsx` gets a new "Offline" section with:

- **Maximum download resolution** — radio buttons matching the existing `StreamingResolutionEnum` verbatim: Original / 4K / 1080p (FullHd) / 720p (StandardHd) / 480p (Standard) / 240p (Low). 1440p is intentionally absent — adding `WqHd` would cascade through GraphQL schema + Go enum + every existing resolution menu and isn't worth it for MVP. Default: 1080p (sane bandwidth + storage default for mobile devices).
- **Storage usage** — read-only display of `navigator.storage.estimate()` results, plus a "Clear all offline scenes" button (with confirm dialog).
- **Persistent storage** — read-only "Granted ✓" / "Not granted (downloads may be evicted)" line, with a "Request" button if not granted. (Browsers handle this idempotently.)

Stored in `localStorage` under `stash-offline-max-resolution`. Not in server config — this is per-device.

## Metadata refresh

Snapshotted scene fields (`title`, `studio_name`, `performers`, `tags`, `paths.screenshot`, etc.) go stale if the user edits the scene server-side after downloading. To keep the Offline view honest:

- On Offline-view mount, batch-fetch the latest `findScenes(filter: { id: in [...] })` for every locally-stored scene, with a small debounce so a fast remount doesn't double-fire.
- For each result, diff the snapshot against the live row; if any visible field changed, write the new values to the IDB row. Leave `format`, `resolution`, `bytes`, etc. alone — those describe the local file, not the server scene.
- Failure is silent (offline / server unreachable) — render the snapshotted data as-is. No spinner, no banner; cards just continue showing the last-known state.
- Scenes deleted server-side: the GraphQL response excludes them. We mark the local entry with `server_status: "missing"` (visible as a small badge on the card: "Removed from server"). The local file stays — user can keep watching it but the entry can no longer be re-downloaded; the only option is local delete.

`server_status` field added to `OfflineEntry`: `"present" | "missing" | "unknown"`. Defaults to `"unknown"` on download (not yet refreshed); set to `"present"` or `"missing"` after the next refresh pass.

## Deletion semantics

Two paths:

1. **User deletes from list view** — removes IDB row + unlinks OPFS file. Idempotent if either is already gone.
2. **Browser evicts under quota pressure** — OPFS file vanishes; IDB row remains. On Offline view load, we walk IDB rows and check `getFileHandle(...).catch(() => "missing")`. Missing rows are marked `error: "Evicted by browser"` in-memory (not persisted) and rendered with an inline "File missing — re-download" affordance, so the user understands what happened.

## Error handling

| Failure | Behaviour |
|---|---|
| Server returns 5xx | Mark `error`, surface in card, "Retry" button |
| Server returns 503 (encoder not available) | Same, with explanatory message |
| Network drops mid-download | Same; partial OPFS file deleted |
| Quota exceeded mid-write | Same; partial OPFS file deleted; surface "Out of storage" with link to settings |
| User navigates away mid-download | Download continues if PWA is foregrounded; cancels if the tab is closed |
| PWA reloaded mid-download | On boot, mark interrupted entries `error: "Interrupted"`; queued entries stay queued and resume |

## Phasing

**Phase 1 (MVP, in scope for the first PR)**

- OPFS storage layer + IDB metadata
- Backend `/scene/{id}/download` route
- `pkg/ffmpeg/download.go` with `copy`, `copy-aac`, `h264` formats
- HW H.264 encoder selection (reuse existing `hwCodecHLSCompatible` logic)
- Format selection client-side (HEVC branch present but never picked; server doesn't advertise it)
- Top-level `/offline` route + nav entry
- `<OfflineSceneList>` list view (cards reuse `<SceneCard>`)
- Context-menu "Download" in scene card + scene actions menu
- Settings: max resolution, storage estimate, clear-all
- Offline playback via blob URL into existing `<ScenePlayer>`
- Save to Files via tier-1 / tier-2 path
- Single-download serial queue
- Re-download replaces

**Phase 2**

- HEVC backend stream type + encoder probe
- `serverCapabilities.downloadFormats` GraphQL field
- Client format-pick uses HEVC when available

**Phase 3**

Shipped:

- HW AV1 encoder support (`av1_vaapi`, `av1_qsv`, `av1_nvenc`) gated through `serverCapabilities.downloadFormats`. Client format-pick prefers AV1 > HEVC > H.264 when both device decode and server encode are available.
- Range-based resume on interrupted downloads. The server's `mode=copy` path now short-circuits to `http.ServeFile` when the source is an MP4-family container with copy-eligible codecs (the common case), giving Range / If-Range / ETag for free. The download worker checks the existing OPFS file size on retry, sends `Range: bytes=N-`, and appends to the partial when the server returns 206. `mode=copy-aac`, `mode=h264`, `mode=hevc`, `mode=av1` still flow through the ffmpeg pipe (no Range support — the server returns 200 + full body and the worker silently restarts from byte 0).
- Full ScenePlayer chrome for offline playback. `<OfflineScenePlayer>` reuses `<PlayerControls>` (timeline, hotkeys, fullscreen, frame zoom, loop) by spinning up a parallel `createPlayer({ features: videoFeatures })` factory and pointing it at the OPFS blob URL. Markers and captions are deferred (would require snapshotting `scene_markers` + downloading caption files into OPFS).

Deferred — Background Fetch on Chrome:

The PWA doesn't ship a service worker today, so Background Fetch is a green-field addition: SW build pipeline (Vite plugin), SW registration only in production, Background Fetch dispatch + lifecycle (`backgroundfetchsuccess` / `backgroundfetchabort` / `backgroundfetchclick`), main-thread ↔ SW message passing for live progress (the BG Fetch API doesn't push to the main thread directly — the SW has to poll `event.registration.downloaded` and forward), plus a feature-detect path so Safari and Firefox keep the in-tab worker. Realistic effort is ~half a day plus testing on actual mobile devices to validate the Chrome-Android background path. Splitting into its own PR keeps Phase 3's review scope manageable.

## Settled decisions

For reference — these were resolved during design and shouldn't be re-litigated without strong reason:

1. **No 1440p in the resolution picker.** Use the existing `StreamingResolutionEnum` verbatim; adding `WqHd` cascades through too many surfaces for the MVP.
2. **Snapshot scene fields to IDB at download time, refresh when next online.** See "Metadata refresh" above.
3. **Resume position is local-only.** No server sync.
4. **Quota refusal at 95% of `quota - usage`.** Leaves headroom for browser-internal metadata.

## File-by-file change inventory

Backend:

- `pkg/ffmpeg/download.go` — new
- `pkg/ffmpeg/encoder_capabilities.go` — new (phase 2 wires HEVC; phase 1 stubs `H264 = ...` from existing logic)
- `internal/api/routes_scene.go` — add `/scene/{id}/download` handler
- `graphql/schema/types/config.graphql` — phase 2: add `serverCapabilities { downloadFormats }` field

Frontend:

- `src/routes/offline.tsx` — new top-level route
- `src/components/offline/` — new directory
  - `offline-scene-list.tsx`
  - `offline-scene-card-data.ts` (adapter: `OfflineEntry` → `SceneCardScene`)
  - `offline-scene-player.tsx` (wraps `ScenePlayer` with blob URL)
  - `pick-download-format.ts`
  - `use-download-queue.ts`
  - `opfs-storage.ts` (writes / reads / unlinks scenes in OPFS)
  - `offline-db.ts` (IDB wrapper for `offline_scenes` object store)
  - `save-to-files.ts`
  - `download-button.tsx` (shared menu-item component for the two context menus)
- `src/components/cards/scene-card.tsx` — add Download menu item
- `src/components/detail/scene-actions-menu.tsx` — add Download menu item
- `src/components/layout/nav-items.tsx` — add Offline nav entry
- `src/routes/settings.tsx` — add Offline settings section
- `src/locales/en-GB.json` — new strings (download / offline / re-download / save to files / etc.)

Estimated phase 1 scope: ~1500 LOC across new files + ~100 LOC of edits.

## Notes

- The download endpoint reuses `pkg/ffmpeg`'s existing `CodecInit` and HW-encoder selection but writes to `pipe:1` instead of HLS-segmented disk output. It does NOT share the `runningStreams` machinery — that's tied to HLS segment lifecycle and per-segment caching, neither of which downloads need.
- iOS PWA OPFS uses Safari's WebKit storage, which sits under the "Manage Website Data" controls in Settings → Safari. Users can blow it away there. Worth surfacing in the Offline settings help text.
- We deliberately don't store scene previews / sprites / VTT in OPFS for phase 1. They add cost and the list view can either fetch them when online or fall back to a placeholder. If users complain about ugly offline cards, phase 2 / 3 can opt-in to download cover art.
- The `+frag_keyframe+empty_moov` MP4 we serve is a fragmented MP4. iOS plays these natively, Chrome does, all our targets do. The downside is that MP4 inspectors show no `moov.duration` until segment parsing — minor.
