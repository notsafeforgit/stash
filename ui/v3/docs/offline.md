# Offline scene downloads

This guide describes the implementation on `v3-rewrite`, checked on 2026-09-08.
The earlier phased proposal has been replaced by current behavior and explicit
limits. See the [architecture guide](architecture.md) for shared list/player
contracts and the [documentation index](../../../docs/README.md) for other guides.

## What is implemented

Scenes can be downloaded into browser-managed storage, played from the local
file, and exported with **Save to Files**. The download queue survives route
navigation, persists its entries across reloads, and supports retry, cancellation,
and deletion. The Offline view reuses the shared list and lightbox components.

“Offline” currently describes local media and metadata access after the app has
loaded. There is **no service worker or Background Fetch integration**. A cold
launch or reload without the server is not guaranteed to load the app shell or
pass its startup gates. Downloads run in the page and can be suspended when the
browser backgrounds or closes it. Installing the PWA does not remove these limits.

## Download and playback flow

1. The scene card/detail download action snapshots metadata and selects a format
   using device decode support, server encoder capabilities, and the device's
   maximum-resolution preference.
2. The shared queue writes an IndexedDB row, then its single active worker
   fetches the scene download endpoint and streams the response into OPFS.
3. The tray and list show queued, downloading, complete, or error state. A known
   `Content-Length`/`Content-Range` total enables percentage progress; otherwise
   the UI shows bytes transferred.
4. Completed scenes open at `/offline/$sceneId` or in the list's lightbox. The
   player uses a `blob:` URL backed by the OPFS file and a scene adapter built
   from the metadata snapshot. The URL is revoked when no longer needed.
5. **Save to Files** exports one scene through `showSaveFilePicker` when available,
   streaming to the chosen file. Otherwise it uses an anchor download with a
   `blob:` URL; the browser controls the resulting download/share UI.

Paths above are router paths relative to the application's deployment prefix.
Backend requests use `getPlatformURL`; do not hard-code origin-root URLs.

The list filters, sorts, and paginates its local entries in memory. Its
`EntityListPageConfig.source` has `kind: "local"`; list data does not require a
GraphQL list request. Optional online metadata refresh is a separate operation.
The preview lightbox follows the currently displayed item order. Bulk context
menu actions support re-download/retry, cancellation, and deletion from the
device. There is no bulk ZIP export.

## Storage contract

| Storage | Current contents |
| --- | --- |
| OPFS | MP4 bytes at `stash-offline/<deployment-hash>/scenes/<scene_id>.mp4` |
| IndexedDB | Database `stash-offline:v1:<deployment-url>`, version 1; `offline_scenes` keyed by `scene_id`, indexed by `downloaded_at` and `status`; `imports` and `settings` retain migration decisions |
| localStorage | Per-device maximum resolution under `stash-offline-max-resolution` |

[OfflineEntry](../src/components/offline/offline-db.ts) is the authoritative
metadata type. It stores title/details, studio/performer/tag snapshots, source
metadata and asset URLs, selected download format/resolution, size and status,
OPFS path, server-presence state, and the local playback position. Optional
fields allow older rows to remain readable. `width_actual`/`height_actual`
currently start from source dimensions; completion does not probe the downloaded
file, so these fields are not verified output measurements.

Use `offline-db.ts` for all metadata transactions and `opfs-storage.ts` for file
access. OPFS receives a stream rather than an accumulated multi-gigabyte Blob.
`patchEntry` merges fields inside one IndexedDB transaction so progress and
resume writes do not replace unrelated metadata. OPFS and IndexedDB are separate
stores: metadata alone does not prove that a playable file still exists.

Within a browser origin/profile, each backend deployment has its own catalog,
files, broadcasts, and locks. [offline-scope.ts](../src/components/offline/offline-scope.ts)
derives their identity from the normalized backend mount URL returned by
`getPlatformURL()`, independently of v3 route paths. Credentials, query strings,
fragments, and excess trailing slashes are excluded. OPFS uses a SHA-256 digest
of that URL as a bounded directory name. Two installations under `/stash-a/`
and `/stash-b/` can therefore download the same scene ID without collisions.

Identity is address-based, not a server UUID or library fingerprint. Replacing
the server library at the same URL cannot be detected by this namespace.
Changing its prefix creates a new namespace; use recovery below to copy the
old downloads. Another origin or browser profile has separate storage that this
app cannot inspect.

Settings expose storage usage, clear-all, and a persistent-storage request.
Usage, quota, and persistence apply to the whole browser origin, including other
deployments and retained migration sources. Persistence is subject to the
browser's decision and is not a backup. The queue
refuses to start another download when the reported origin usage is at least
95% of quota, but does not reserve the final file size. A write can still fail
mid-download with a quota error. Missing/evicted files surface a retry action.

## Migration and recovery

The earlier database `stash-offline` and OPFS directory `scenes/` remain intact.
On queue initialization, the worker copies legacy entries whose saved artwork
URLs consistently identify the current backend mount. It recognizes the
server's scene screenshot/preview and sprite/VTT routes; missing, conflicting,
or unrecognized URLs require an ownership decision. Metadata is validated before
import; malformed rows remain untouched in their original store. Legacy entries
marked `downloading` are not automatically imported because an older tab may
still own them.

**Restore saved downloads**, available on the Offline page and in settings,
lists legacy storage and other deployment namespaces on this browser origin.
Select a source and scenes, then confirm that they belong to the current server.
This also recovers downloads after a prefix change and rebases recognized asset
URLs. Browsers without IndexedDB database enumeration offer a previous-address
field; inspection only reads local storage and makes no server request.

Recovery streams copies into the current namespace and preserves metadata and
the playback position. **Original metadata and files are kept**, so sufficient
free space for another copy is required. Existing destination entries win
collisions. Incomplete or missing files produce a recoverable error entry and
require explicit retry; saved work never automatically queues against a newly
chosen server.

Migration holds source and destination locks, checks the source again before
committing, and commits an import receipt alongside the new metadata. Busy
sources ask you to close their other tabs and retry. This recheck also catches
changes made during copying by older clients without locks, but it is not a
content hash. Failed or cancelled copies remove their partial destination and
remain retryable. Migration failure does not prevent new downloads.

Receipts survive deletion, so retained legacy copies do not silently reappear
on reload. Clear-all removes only the current deployment's files and catalog
and disables further automatic imports there. Explicit recovery remains
available. It does not remove original migration sources or another
deployment's downloads; browser-wide storage clearing is outside this command.

## Queue recovery and byte-range resume

[use-download-queue.ts](../src/components/offline/use-download-queue.ts) owns a
page store observed through `useSyncExternalStore`. IndexedDB owns the durable
queue and a BroadcastChannel named after the deployment database announces
committed changes to its other pages. The `<database-name>:worker` Web Lock
permits one worker per deployment at a time; different deployments can download
concurrently. Migration and interrupted-download recovery run only after
obtaining that lock, so opening a second tab does not mark the first tab's
active download as interrupted.

Each scene command/writer owns `<database-name>:scene:<id>`. A download establishes
its AbortController before asynchronous initialization. Cancellation records the
attempt's request ID and a durable cancellation flag, then signals its owner.
Deletion waits for the writer and its progress checkpoints to settle before
removing the file/row. Enqueue and retry recheck status under the scene lock.
Clear-all requests cancellation and owns the worker lock while clearing OPFS
and IndexedDB, including orphan files.

Without Web Locks or BroadcastChannel, existing namespaced downloads and
identifiable legacy downloads remain playable, while library mutations and
migration are unavailable. Legacy playback is read-only and respects current
entries, import receipts, and clear-all decisions; it cannot save a new resume
position back into the old store. The UI disables the download action and
reports unsupported commands. This fallback avoids starting competing writers.
Browsers also need IndexedDB/OPFS in a secure
context for offline storage; the queue is not a service worker or Background
Fetch task.

| Event | Behavior |
| --- | --- |
| Reload with queued entries | Rebuild the queue and start processing |
| Reload with an in-flight entry | Recover only after the previous owner releases its worker lock; mark an orphan `error` and allow retry |
| Enqueue an already queued/downloading scene | No duplicate download |
| Enqueue a new download for an existing completed/error row | Replace the prior local file with a fresh metadata/format snapshot |
| Retry an existing entry | Re-queue with its saved format and metadata; inspect any existing file bytes |
| Cancel a queued entry | Remove it from the queue and local stores |
| Cancel an active download | Abort the fetch/write and expose an error state for retry |
| Delete from device | Cancel the matching attempt, wait for its writer, then remove the OPFS file and IndexedDB entry |

Range retry is **implemented, but best effort**:

- If a partial file is present, the worker sends `Range: bytes=N-` using its
  existing size. It appends only when the server responds with `206` and its
  `Content-Range` starts at the requested offset.
- A full `200` response rewrites from byte zero. This is expected for FFmpeg
  remux/transcode output, whose bytes cannot safely be resumed across runs.
- Explicit cancellation and a failed fresh/full-body write trigger partial-file
  cleanup. Failed append attempts retain the existing file for another retry.
  Browser writable-file behavior determines which bytes survive interruption;
  reload does not guarantee that progress was committed to disk.
- The worker does not persist a source validator or send `If-Range`, and does not
  automatically recover from `416`. Resume assumes the source bytes are unchanged.
  Remove the local entry and download again if the source changed or a range
  retry fails repeatedly.

## Backend endpoint and capability query

With `STASH_ENABLE_V3_UI=true` (or `--enable-v3-ui`), the backend adds:

```text
GET  /scene/{id}/download.mp4?mode=copy
GET  /scene/{id}/download.mp4?mode=h264&resolution=FULL_HD
HEAD /scene/{id}/download.mp4?mode=copy
```

[routes_scene.go](../../../internal/api/routes_scene.go) selects the scene's
primary file and calls [stream_download.go](../../../pkg/ffmpeg/stream_download.go).
Accepted modes are `auto`, `copy`, `copy-aac`, `h264`, `hevc`, and `av1`:

- `copy` preserves source video/audio codecs; `copy-aac` preserves video and
  converts audio to AAC. The client requests Original resolution for both.
- `h264`, `hevc`, and `av1` request an encoding, with server-side copy shortcuts
  when suitable. `auto` chooses compatible copy or H.264.
- If the resolved copy path can serve the MP4-family source directly,
  `http.ServeFile` provides length and byte-range support. Other paths stream
  fragmented MP4 from FFmpeg without a total length or range support.
- The response is a video MP4 attachment. Disconnect/abort cancels the FFmpeg
  request context. Missing source files, disabled live transcoding, and invalid
  copy requests surface HTTP errors to the queue.

Capabilities are a **top-level GraphQL query**, not a Configuration field:

```graphql
query ServerCapabilities {
  serverCapabilities {
    downloadFormats
  }
}
```

The query is declared in [schema.graphql](../../../graphql/schema/schema.graphql),
with `ServerCapabilities` in
[metadata.graphql](../../../graphql/schema/types/metadata.graphql). The actual
advertisement is defined in
[server_capabilities.go](../../../internal/manager/server_capabilities.go):
universal modes are `auto`, `copy`, `copy-aac`, and `h264`; automatic HEVC/AV1
selection is advertised only when the corresponding hardware encoder is
available. An explicit HEVC request can still use the slower libx265 fallback.
The client uses Apollo's cached capability result and conservative defaults
until it arrives. Restart the backend after changing encoder availability and
reload the client to refresh its cached result.

## Client format and resolution choice

[pick-download-format.ts](../src/components/offline/pick-download-format.ts)
contains the shared decision function:

1. If no downscale is needed and the device decodes the source video and audio
   in MP4, choose `copy`.
2. If only the source video is decodable in MP4 and no downscale is needed,
   choose `copy-aac`.
3. Otherwise choose AV1, then HEVC, when both device decode and advertised server
   encode support allow it; fall back to H.264.

The resolution ceiling uses the source's shorter dimension to decide whether
scaling is required. It does not request upscaling. The device preference
defaults to 1080p and offers Original, 4K, 1080p, 720p, 480p, and 240p, matching
the existing `StreamingResolutionEnum`. There is no 1440p option. Use the
`downloadQueryString` helper rather than inventing resolution or mode strings.

## Metadata and local playback position

On Offline-view mount and catalog membership changes, the metadata refresh hook
attempts a network query for the stored scene IDs. Successful results update the
snapshots; missing scenes gain a “Removed from server” indication without deleting
local media. Progress updates do not cancel a membership refresh; Strict Mode
cleanup and superseded requests cannot publish stale responses. Only valid
numeric scene IDs are queried, and an empty set never becomes an unfiltered
query. Connectivity returning triggers another attempt. Network failures leave
the snapshots intact. Screenshots, previews,
sprites, and VTT are stored as remote URLs, not copied into OPFS, so artwork and
preview assets can be unavailable offline.

`useOfflineResumeWriter` writes the local playhead about every five seconds
when it moves at least 0.5 seconds, plus a best-effort final write on unmount or
`beforeunload`. Both the detail player and offline lightbox use local resume
state. Browser termination can prevent the final asynchronous write.

## Settled decisions

- Offline playback position stays in IndexedDB; it does not synchronize the
  server's resume/watch activity.
- Device deletion never deletes server media. Server deletion never automatically
  deletes the downloaded copy.
- Downloading the same scene replaces its local entry; there is one current copy
  per scene ID per deployment, not a catalog of different encodings.
- Reuse the list/player extension points and shared format picker when extending
  offline behavior. Keep local data independent of GraphQL list fetching.

## Module map and future work

All frontend filenames below are under `src/components/offline/` unless noted.

| Module | Responsibility |
| --- | --- |
| `download-action.ts`, `scene-download-input.ts`, and scene menu wrappers | Typed download actions and one shared metadata snapshot projection |
| `pick-download-format.ts`, `use-server-capabilities.ts`, `offline-settings.ts` | Codec/resolution policy and preferences |
| `use-download-queue.ts`, `download-tray.tsx`, `download-notifications.tsx` | Serial worker, recovery, progress, and notifications |
| `offline-db.ts`, `opfs-storage.ts`, `use-offline-entries.ts` | Persistent stores and subscriptions |
| `offline-scope.ts` | One deployment identity for the database, directories, broadcasts, and locks |
| `offline-entry-schema.ts`, `offline-migration-policy.ts`, `offline-migration.ts` | Source validation, ownership checks, copy migration, receipts, and progress |
| `offline-recovery-control.tsx`, `offline-source-address-form.tsx` | Explicit source/scene selection and previous-address recovery |
| `offline-scene-list-page.tsx`, `offline-list-source.ts`, `offline-filter-sidebar.tsx` | Local list, selection, sorting, and filtering |
| `offline-scene-card-data.ts`, `offline-scene-adapter.ts` | Shared card/player data adapters |
| `use-opfs-blob.ts`, `use-offline-resume-writer.ts`, `use-offline-scene-lightbox.tsx` | Local playback lifetime and resume |
| `offline-metadata-refresh.ts` | Optional server metadata refresh |
| `save-to-files.ts`, `offline-settings-section.tsx` | Export, quota display, persistence, and clear-all |
| `src/routes/offline/` | List and detail routes |

Service-worker shell caching, Background Fetch, source validation for range
retries, cached artwork, and multi-file export remain
unimplemented. They require separate feature design and validation; the old
proposal's phase labels are not a completion checklist.
