# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

Stash is a self-hosted media organizer written in Go (backend) + React/TypeScript (frontend). It exposes a GraphQL API, manages a SQLite database, and wraps FFmpeg for video processing.

This repository is an **independent fork** of upstream stashapp/stash. Work continues on `v3-rewrite`; read [FORK.md](FORK.md) for release and upstream-import policy. V2.5 compatibility is frozen at `v2.5-compatible-final`, not a constraint on new code. A fresh installation is a local media library with source tracking off. Personal FileFlows workflows, host backup orchestration and production records are maintained separately in `notsafeforgit/stash-ops`, not as application build dependencies. The [documentation index](docs/README.md) links current guides.

The owner requires GitHub Actions-built, GHCR-published production images. Do not
deploy another local-image fallback or treat a Git push as publication. Follow
the [deployment runbook](docs/v3-deployment.md), retain the actual run URLs and
image digest, and report any GitHub-side execution block explicitly. The main
image includes the native runtime and hardware support; do not dispatch a second
`stash-s6` build. Its sole default configuration layout is `/config`.

Fresh installations have source tracking off. The saved
`source_tracking_enabled` option in Settings → Services takes effect on restart;
GraphQL `sourceTrackingActive` reports the running capability. Keep ingestion
routes/workers and source UI gated together without disabling library folders,
StashDB/providers or deletion recovery. The owner's deployment explicitly enables
it. S3 backup is separate host setup, not a default Stash service. See
[optional integrations](docs/optional-integrations.md).

New plugins use the independent [`apiVersion: 3` contract](docs/plugin-manifests.md). Preserve its settings, UI contributions, capability checks, and after-success notifications. Only `apiVersion: 3` manifests load. Unversioned manifests and legacy UI injection are rejected and reported through `pluginLoadErrorsV3` and Settings → Plugins. Native catalog invariants belong in core services. New Twitter captures retain numeric thread/parent/publisher relationships in schema 1000105; evidenced self-replies share a source gallery while retaining each post and its attachment order. See [source albums](docs/native-source-albums.md#twitter-threads-and-replies).

Library folders and ingestion roots share the native `media_roots` records from
schema 1000113. Manage them in Settings → Library; do not restore a parallel YAML
`stash` writer or a separate root navigation page. Scan options have their own
revision so edits cannot invalidate running scrape bindings. Read one folder
snapshot per operation through `manager.LibraryPaths`, or `ReadLibraryPaths`
inside an existing repository transaction. See the
[folder contract](docs/native-ingestion.md#library-folders-and-logical-roots).

Some implementation notes below describe intermediate staging gates. Use the
dated progress/cutover records for current deployment status; do not repeat a
completed migration, reconciliation or restore because an older note calls it
pending. These notes do not require two UIs, legacy API shims or dual migration
tracks. Do not remove a required caller without converting and testing it.

## Development quickstart

```bash
make pre-ui       # Install v3 dependencies
make generate     # Generate Go and v3 GraphQL bindings
make ui           # Build the embedded v3 app and login locales
# Terminal 1:
STASH_PORT=9999 make server-start
# Terminal 2:
VITE_APP_PLATFORM_URL=http://127.0.0.1:9999 make ui-v3-start
```

Open v3 at `http://localhost:3002/`. Set the backend URL explicitly; v3's proxy
otherwise defaults to port 8010. `make ui-start` and `make ui-v3-start` both
run the native application on port 3002. See the [v3 development guide](ui/v3/docs/development.md) for setup,
generation, and validation, and the [deployment runbook](docs/v3-deployment.md)
for image publication and Quadlet restarts.

## Build commands

| Command | Purpose |
|---|---|
| `make build` | Build `stash` and `phasher` binaries |
| `make build-release` | Release build (stripped debug info + PIE) |
| `make stash` | Build only the main binary |
| `make ui` | Generate, type-check and build v3 assets and login locales |
| `make ui-v3-only` | Generate, type-check, and build v3 embedded assets |

## Testing and linting

```bash
make validate-fork     # Parallel full gate; includes generation and validated UI build
make it                # Go unit + integration tests only
make pre-clients test-clients # Optional real-server tests of pinned external clients
make lint              # CI-pinned golangci-lint via go run
make fmt               # Format Go source
make validate-ui-v3    # Biome, generation/types, formatting, locales, tests, native contracts
make fmt-ui-v3         # Format v3 source
make validate          # Alias for validate-fork; no downloader dependency
make validate-ui       # Native UI validation; validate-ui-v3 is an alias
```

Build the native UI before standalone full Go tests; embedded-asset tests
require real v3 route chunks. `make validate-fork` builds and validates those
assets itself, then runs Go checks while independent suites overlap, using two
workers by default (`VALIDATION_JOBS` controls local concurrency). Use the
[validation sequence](ui/v3/docs/development.md#validation).
To run a single Go test: `go test ./pkg/models/... -run TestFilterAST`.
The shared `make test`/`make it` package timeout is twenty minutes for the growing
migration suite; override `GO_TEST_TIMEOUT` when needed. Individual operation,
lease and provider deadlines remain independently tested.
Reusable Python clients live in [stash-integrations](https://github.com/notsafeforgit/stash-integrations),
with independent tests, packages and releases. They are not needed to develop or
validate the core application. `make pre-clients test-clients` fetches the exact
commit in `integration-clients.json` and runs focused real-server compatibility
tests. These tests require Python 3.12+ and uv; ordinary `make it` does not.
CI selects this suite for API, schema and other contract dependencies, and skips
it for unrelated UI and standalone media-engine changes. When changing a client
contract, run this suite locally as well as the normal `make validate-fork` gate.

The external `stash-native-archive` package provides `stash-archive`. Keep the
[archive contract](docs/native-archive-format.md), ingestion APIs, domain integrity
and focused contract fixtures here. Website credentials belong to external
workers. The server never executes ZFS, Podman or S3 commands for a client.

Installation-specific schedules, destinations, FileFlows code and release records
belong in the operations repository. Its image and host packages have independent
CI and deployment. Preserve active outboxes, identities, locks and captured backup
views when changing a deployment; never treat an installed timer as proof that a
job completed. Ordinary library use must continue with every optional integration
disabled; see [optional integrations](docs/optional-integrations.md).

## Architecture

Start with the [system overview](docs/ARCHITECTURE.md) and
[v3 frontend module map](ui/v3/docs/architecture.md). Detailed player and layout
contracts live in [player.md](ui/v3/docs/player.md) and
[interactions.md](ui/v3/docs/interactions.md).

### Request path

Browser → Vite dev server (dev) / embedded HTTP server (prod) → Chi router (`internal/api/server.go`) → GraphQL handler → resolvers (`internal/api/resolver_*.go`)

### Key layers

- **`internal/api/`** — GraphQL resolvers and HTTP routes. `generated_*.go` files are auto-generated by gqlgen; don't edit them. Custom logic lives in `resolver_*.go`. Loaders pattern (`loaders/`) handles N+1 avoidance.
- **`internal/manager/`** — Singleton `Manager` struct that owns all services: FFmpeg, JobManager, PluginCache, Database, etc. Accessed via `manager.GetInstance()`.
- **`pkg/models/`** — Domain models and repository interfaces. `Repository` struct holds all `ReaderWriter` interfaces. All database access goes through these interfaces within transactions (`WithTxn`/`WithReadTxn`).
- **`internal/ingest/`** — Native producer capture validation and atomic receipt/domain transactions. Its HTTP router uses collection/root-scoped Stash access tokens, separate from application sessions and third-party service credentials. See [native ingestion](docs/native-ingestion.md) for the event, receipt, and worker contracts.
- **Verified file content** — `files.generation` fences location/byte changes; updates require the generation that was read. `media_contents` shares server-verified SHA-256 identities, and `file_content_versions` retains immutable per-generation evidence. Use the shared scanner preparation and `PreparedMedia.RecordContent` with a held descriptor through transaction completion. Content equality does not authorize merging scenes or images. See [native schema](docs/native-schema.md).
- **File publication** — `ingest.CaptureFileTarget` records an existing file's lifetime and persistent path removal history. `PreparedMedia.PublishFile`/`PublishMedia` reuse concurrent scans and unique verified-byte owners inside the caller's managed transaction. `PublishIntake` adds collection provenance, source attachment evidence and album membership, honoring selected media and explicit unlinks. Keep the descriptor open through commit. Ambiguous owners, removals, and changed generations require review; the durable file worker checkpoints registration before previews and media/gallery hooks; collection policies apply in that checkpoint through `pkg/metadata`. The same service handles explicit folder defaults in ordinary scans; preserve field choices and report ambiguous name/alias matches. Policy definitions and preview/apply endpoints are documented in [native ingestion](docs/native-ingestion.md#native-metadata-policies).
- **Fclones-backed physical deduplication** — The external client uses `fclones group` for duplicate discovery. `pkg/dedup` previews those exact root-relative pairs and verifies complete bytes before journaled removal; it does not replace fclones with a new library-wide duplicate finder. Both locations must have the same single scene/image owner; retain original source evidence and derived survivor matches. Keep backup/download exclusion in the external caller and recover uncertain requests by their UUID. Client packages live in `stash-integrations` and personal orchestration in `stash-ops`. See [fclones-backed file deduplication](docs/native-file-deduplication.md).
- **`pkg/sqlite/`** — SQLite implementation of the repository interfaces, including migrations.
- **`graphql/schema/`** — GraphQL schema. After editing, run `make generate` to regenerate Go bindings.

### Frontend (`ui/v3/`)

The sole application UI, embedded with its share and offline entry points.
Native routes, preview generation and ingestion workers do not require a UI
opt-in flag. Historical UI code is preserved at `v2.5-compatible-final`.

`Scene.sceneStreams` is the sole direct/segmented playback catalog. Native
fragments request it directly; the former `sceneStreamsV3` alias, root stream
query and parallel legacy catalog are removed. Keep signed media URLs, source
MIME types, codec/GOP/resolution checks and original-file share behavior intact.

**Routing — TanStack Router v1**
- File-based routes under `src/routes/`. Route files use `createFileRoute`.
- Routes validate search parameters with `validateSearch`; existing detail/settings routes pass Zod schemas directly. List filter parsing belongs in the shared list/filter modules.
- Navigate with `useNavigate`; access params with `Route.useParams()`, search with `Route.useSearch()`.
- Access history (back/forward) via `useRouter().history`.
- Smart back: use `useSmartBack(defaultPath)` from `src/hooks/use-smart-back.ts` on detail pages. It navigates to the explicit `returnTo` URL, the last Home/list page visited by the current router (including an initial filtered visit), or `defaultPath`. TanStack scroll restoration uses the full public URL so this action and browser Back recover the same list position; keep new lists in the shared `EntityListPage` shell.

**Data fetching — Apollo Client v4**
- `useQuery` / `useMutation` from `@apollo/client/react`.
- Generated TypeScript types and operation documents live in `src/core/generated-graphql.ts`; run `pnpm --dir ui/v3 gqlgen` after schema changes (also included in v3 dev/build/check scripts).
- Generated operations use `TypedDocumentNode` from `@graphql-typed-document-node/core`; import the generated documents at call sites.
- List pages use `use-list-data.ts` for GraphQL/local dispatch and independent count queries, and `useCachedQueryResult` to retain usable data during refreshes. SearchInput owns typing debounce; the data hook adds no further delay. Preserve pending/error/retry state without treating stale data from another filter as a successful result. See the [list module map](ui/v3/docs/architecture.md#lists).

**Forms — TanStack Form v1**
- Use `useForm` from `@tanstack/react-form` for all forms.
- Validate with Zod schemas passed to `validators: { onChange: schema }`.
- Access fields via `form.Field` render-prop pattern; never manage form state manually with `useState`.

**UI components — shadcn + Base UI**
- shadcn components live in `src/components/ui/`. These wrap `@base-ui/react` primitives with Tailwind styling.
- **Never use bare HTML form elements.** Always use the shadcn wrappers:
  - `<input>` → `<Input>` from `src/components/ui/input.tsx` (wraps `@base-ui/react/input`)
  - `<select>` → `<Select>` from `src/components/ui/select.tsx` (wraps `@base-ui/react/select`)
  - `<textarea>` → `<Textarea>` from `src/components/ui/textarea.tsx`
  - `<button>` → `<Button>` from `src/components/ui/button.tsx`
  - `<input type="checkbox">` → `<Checkbox>` from `src/components/ui/checkbox.tsx`
  - Searchable dropdowns → `<Combobox>` from `src/components/ui/combobox.tsx` (wraps `@base-ui/react/combobox`)
  - Custom selects / dropdowns → `<Select>` or `<DropdownMenu>` accordingly
- Loading state → `<Spinner>` from `src/components/ui/spinner.tsx` (CSS border animation, no icon dependency)
- Overlays: `<Sheet>` (side panel), `<Drawer>` (Base UI drawer), `<BottomSheet>` for mobile
- When using Base UI `Popup`/`Positioner`, add `data-base-ui-swipe-ignore=""` to fix scroll issues on mobile

**Styling — Tailwind CSS v4**
- Utility-first; no custom SCSS.
- Use `cn()` from `src/lib/utils` (combines `clsx` + `tailwind-merge`) for conditional classes. Never use `clsx` or `classnames` directly.
- Design tokens are complete CSS colors, usually OKLCH. Use semantic utilities such as `bg-primary` or `var(--color-primary)` in CSS, without an `hsl()` wrapper. Both `--color-*` and some unprefixed variables exist; runtime overrides must account for both. Runtime `custom.css` is plain CSS; see [theming](ui/v3/docs/theming.md).

**Icons — Lucide React**
- Import named icons from `lucide-react`. Do not use other icon libraries.

**Video player — Video.js v10 RC.2 (`@videojs/react`)**
- `createPlayer({ features: videoFeatures })` is called once at module scope to produce a typed `Player` root plus `usePlayer` / `useMedia` hooks. State lives in the root's internal store; access it via `Player.usePlayer(selector)` from any descendant. Import the shared `Container` separately from `@videojs/react`.
- `SceneVideo` configures the packaged `<HlsJsVideo>` from `@videojs/react/media/hlsjs-video` for both direct and HLS sources. Its `HlsJsAdapter` retains one native video element while switching playback engines. Pass the direct source MIME type explicitly, including for blob URLs. The playback adapter and casting extension now live in separate, version-aligned `@videojs/hlsjs-video` and `@videojs/google-cast` packages. Compose `<GoogleCast>` from `@videojs/react/extensions/google-cast`.
- RC.2’s split HLS package pins hls.js 1.6.7. A version-scoped pnpm override retains this fork’s 1.7.3 engine; remove/reassess it when upgrading the adapter rather than silently downgrading playback fixes.
- Controls use a renderless `<Controls.Root>` around `<Controls.Content>`, which owns DOM props, styling and layout.
- Player implementation: `src/components/player/scene-player.tsx` (player shell) and `src/components/player/player-controls.tsx` (controls overlay).

**Tables — TanStack Table v9**
- Use `useTable` with `entityTableFeatures` from `src/components/list/entity-table.ts`. Core rows are included by default; sorting and pagination remain server controlled.
- Type columns with `EntityColumnDef<TItem>` so feature and metadata types stay aligned. Column visibility and order are persisted to `localStorage` per entity type.

**Drag and drop — dnd-kit**
- Use `@dnd-kit/core` + `@dnd-kit/sortable` for any drag-to-reorder UI.

**Internationalisation — react-intl**
- All user-visible strings wrapped in `intl.formatMessage({ id, defaultMessage })`.
- Only edit the `en-GB` locale file; other locales are generated by an external translation service.

**Key conventions**
- No bare stock HTML interactive elements (`<input>`, `<select>`, `<button>`, `<textarea>`) anywhere in component code — always use the `src/components/ui/` wrappers.
- No `react-bootstrap` or any Bootstrap dependency in v3.
- Use shared pending states, spinners, and skeletons as appropriate; preserve usable list content during refreshes and expose errors with retry actions. Layout-only changes must not trigger query loading states.
- Biome is authoritative for v3 linting, including recommended accessibility rules. Fix underlying issues; keep necessary suppressions narrow and explain wrapper/render-prop or gesture-delegation constraints inline. Keep hook dependencies complete instead of hiding reactive values in refs to evade checks.

**HLS via hls.js on all browsers** (`pkg/ffmpeg/stream_segmented.go`, `src/components/player/player-utils.ts:canPlaySource`, `src/components/player/hls.ts`, `src/components/player/scene-player.tsx`, `src/components/player/scene-video.tsx`)

All scene sources render through `<SceneVideo>`, which configures Video.js’s packaged `<HlsJsVideo>`. `HlsJsAdapter` selects hls.js for HLS on MSE-capable browsers, native HLS otherwise, and native playback for direct files. `canPlaySource` gates HLS on `hasNativeHLS() || hasMSE()`. The wrapper supplies the source type, start position and ManagedMediaSource buffer ceilings together in one typed `HlsSource`; attachment, cleanup and native element ownership remain with Video.js. The old custom attachment bridge is no longer needed.

Full-scene HLS playlists (`?end=` absent) expose scene time directly. Clipped playlists (`?end=` present) start at the segment containing `?start=`; hls.js rebases their media timeline to zero. `EXT-X-MEDIA-SEQUENCE` identifies segments, not their playback-time offset. The player retains the fixed clip origin in `offsetStart` and converts between media time and scene time in `hls.ts`. Both playlist shapes use the same buffer-aware seek policy: buffered targets stay in place, while distant unbuffered targets restart loading at the requested position.

This replaces an earlier `@videojs/spf` (with a pnpm patch to honour `MEDIA-SEQUENCE`) attempt — accumulating bugs in resolution-switching and source-change handling tipped the cost-benefit toward hls.js, whose mature implementation handles those scenarios out of the box. The earlier concerns about hls.js's single `initPTS` and SPS/PPS init-segment lock are mitigated by the encoder pipeline below (per-track `setpts`/`asetpts` aligns the two tracks' start PTS, and pinned profile/level + `-forced_idr 1` + GOP locked to segment length keeps SPS/PPS stable across ffmpeg restarts), so hls.js sees a well-formed bitstream that doesn't trip its failure modes.

**Encoder-level PTS normalization** — Safari's native HLS plays audio whose start PTS differs from video's start PTS by going silent-then-audible on audio while freezing video at its first frame (exactly what iOS-recorded M4V sources produce after `-ss` input seek; their audio edit list shifts the first post-seek audio frame ~1.2 s later than the first post-seek video frame). hls.js on every browser is similarly sensitive once segments cross ffmpeg-run boundaries — it computes one `initPTS` from one track and applies the same `timestampOffset` to both audio and video SourceBuffers, so per-track skew at the input would offset audio relative to video for the rest of the session. So the HLS args carry encoder-level PTS normalization that v2.5 didn't need:

- `-vf setpts=PTS-STARTPTS` on video, `-af asetpts=PTS-STARTPTS` on audio — each track's first emitted frame is at PTS 0 inside the run, regardless of what the source demux produced after seek.
- `-avoid_negative_ts make_non_negative` normalizes encoder delay on every full-transcode run before scene time is added. Keep this protection: negative AAC priming cannot be represented by the unsigned MP4 `tfdt` box.
- `-hls_segment_options movflags=+frag_discont:output_ts_offset=N*segDur` applies a full transcode's scene offset in the child MP4 muxers, after that normalization. Applying it on the outer HLS muxer instead makes only the run at zero receive the encoder-delay shift; restarted runs overlap its cached video frames. The child offset retains the initial stream's existing timestamp origin. Codec-copy variants retain their separate offset policy.
- `frag_discont` preserves absolute fragment timestamps; keep edit lists enabled. This does not add `EXT-X-DISCONTINUITY` to the playlist. `stream_v3_timestamps_test.go` checks unsigned decode timestamps, every video frame's timing, and decoding a cache assembled from multiple AV1/Opus transcode runs with the first init segment.
- `-force_key_frames expr:gte(n,n_forced*GOP)` counts frames. Comparing fractional timestamps can emit 61 frames in a nominal 60-frame segment (also reproduced on QSV), shifting later segments away from the playlist and overlapping cached frames from a different run.
- `-copyts` — keeps the demuxer's input timestamps flowing through the pipeline so the source's frame-to-frame PTS deltas survive intact. Without it, ffmpeg's muxer can rebuild timestamps from its internal frame counter; on a VFR source (common with AV1 / iOS-recorded video) that effectively coerces output to a synthetic cadence and the result reads as visible frame-pacing stutter even though the segment list is well-formed. The `setpts`/`asetpts` filters above still pin the per-track baseline to 0 (necessary for the iOS edit-list skew); `-copyts` is what preserves the deltas between successive frames.
- `-forced_idr 1` on QSV H.264 (`pkg/ffmpeg/codec_init.go`) — h264_qsv otherwise emits non-IDR I-frames at the GOP boundary requested by `-g`, and the HLS muxer can only split at IDR boundaries. Without this the segmenter's GOP requests are silently ignored and segments end up far longer than `segmentLength` (causing foreground tab stalls when the player runs out of buffered content while waiting for the next IDR). `-g` + `-keyint_min` are computed from the source frame rate × segmentLength in `pkg/ffmpeg/stream_segmented.go:hlsGopSize`.

**iOS startup regression (2026-09-21)** — The frame-count keyframe / disabled-negative-timestamp change in `4f1180550`, together with the fallback-rate follow-up in `1abd82f7f`, was reverted after iOS HLS transcodes were reported to freeze around one second. The initial AAC fragment contained an unsigned `tfdt` of `2^64-1024` after negative encoder priming wrapped around. ffprobe interpreted the value as a signed packet timestamp, so packet-continuity tests missed it; Linux Chromium/WebKit playback checks also passed. The current correction keeps negative-timestamp protection and moves the full-transcode seek offset to the child MP4 muxers. Packet and decoded-frame tests, including QSV with AV1/Opus input, reproduce and eliminate the overlapping video timeline. Physical iOS confirmation of the reported playback failure remains necessary; do not equate Linux browser tests with that confirmation. Recovery and Retry must retain the native video element.

**Backward seeks before the trim point** — `?start=<sceneTime>` only emits segments from the containing one onward, so the trimmed playlist's `seekable` range starts at the trim. A seek target before that range needs a fresh playlist with a smaller `?start=`. `hlsStrategy.canSeekDirectly` (`src/components/player/hls.ts`) checks the plain `MediaSeekState.seekable` ranges supplied by the transition coordinator and signals for a source-URL change that rebuilds the HLS delegate, rather than letting the player issue an MSE seek into an unbuffered region.

**Stable player root with in-place source changes** (`src/components/player/scene-player.tsx`)

`<Player.Player>`, its store and the native `<video>` stay mounted for a scene session, including direct↔HLS switches. `SceneVideo` assigns a structured source; the adapter reloads the source or rebuilds the playback engine as needed without replacing the element. `pendingResumeRef` preserves playhead, play state, and playback rate while the freeze-frame canvas masks any HLS engine-rebuild gap. URL-reload paths also increment a cache-busting nonce, because a restart at zero can otherwise reproduce the current URL and never trigger the promised reload.

**iOS Safari seeks and buffering** (`pkg/ffmpeg/codec_hardware.go`, `pkg/ffmpeg/stream_segmented.go`, `src/components/player/use-scene-player-sources.tsx`, `src/components/player/scene-video.tsx`)

Keep client buffering and server throughput separate when diagnosing iOS playback:

1. **Far-forward seeks must route through a source reload on iOS.** On desktop MSE we can call `engine.stopLoad() → BUFFER_FLUSHING → engine.startLoad(target)` in place and the SourceBuffer recovers cleanly. On iOS Safari's `ManagedMediaSource` (iOS 17+) the same sequence leaves `video.buffered` stuck at empty after the flush — `readyState` pins at 1, the seek never resolves. Behaviour is the same in or out of native fullscreen. `use-scene-player-sources.tsx` passes `ios: isIOS()` to the pure planners in `scene-player-transitions.ts`, which choose a source reload for distant iOS seeks and restarts. A new `?start=<target>` lets a fresh hls.js engine cold-start at the right fragment; the player root remains mounted and the freeze frame masks the transition.

2. **MMS controls loading windows; unlimited VOD retention adds memory pressure.** `SceneVideo` keeps 10 seconds of played media, targets 30 seconds ahead, and caps forward buffering at 60 seconds. Its 60 MiB `maxBufferSize` is a byte target, **not a memory ceiling**: hls.js takes the larger of that target converted to seconds and `maxBufferLength`, then clamps to `maxMaxBufferLength`. The previous MMS override (60/120 seconds, 240 MiB) also left `backBufferLength=Infinity`, allowing played 4K content to accumulate until browser eviction. Do not restore unlimited retention or infer device memory safety from installed RAM. Browser tests exercise actual buffer eviction past 100 seconds; physical iOS playback is still needed to establish whether this fixes a reported device stall.

**Audio continuity and lightbox lifetime**: `scene-carousel.tsx` uses YARL's public module API for a keyed poster track and one persistent `ScenePlayer`, store and native video. During swipes the outgoing player retains its visual position and source, while the decoded incoming poster stays mounted. Scene changes wait for the actual track animation to finish; obsolete completions and outgoing EOF cannot load or advance another selection. YARL still owns dragging and animation. `playbackKey` resets scene/marker state without resetting media ownership. Pending queries/OPFS and loading sentinels suspend the retained player and release its source; late query results cannot replace the selected scene. Audio, playback rate and an active held-speed gesture survive navigation. Keep WebKit's explicit `canplay` resume. Only the initial entrance uses `usePlayDelay`, which retries muted only after a current `NotAllowedError`; new selections cancel old deadlines and seeks. Chromium/WebKit tests exercise the actual lightbox, but physical iPhone autoplay permission and MMS behavior still require device verification.

**HDR HLS**: `/stream.master.m3u8` is the H.264/8-bit SDR compatibility path and tone maps HDR input. `/stream.hevc.master.m3u8` preserves HLG/PQ in HEVC Main 10 with AAC and two-second GOPs, including HDR-aware scaling. Advertise it only for scanned HDR sources when the server's separate Main 10 hardware test passes. The player requires an HDR video display and Main 10 support in the active MSE/MMS engine; direct/remux remains preferred when usable. Both HDR and SDR choices respect the saved resolution ceiling. HEVC uses a bounded live bitrate and declares its output codec level and video range in the master playlist. Verify actual bit depth, transfer/primaries, decoded pixels and cross-seek timestamps, not only FFmpeg arguments.

For hardware performance diagnosis, `STASH_TRANSCODE_DEBUG=1` logs the selected pipeline. A passed capability probe and faster-than-real-time encoding establish server headroom; they do not establish smooth browser playback. Measure progressing `currentTime`, buffered ranges, decoded/dropped frames and loading events separately. Network `stalled` events alone do not prove the playback clock stopped.

**Known performance trade-off — card aspect mismatch detection**
`EntityCard.Preview` (`src/components/cards/entity-card.tsx`) detects whether an image is naturally portrait or landscape by reading `e.currentTarget.naturalHeight / naturalWidth` in an `onLoad` handler. This causes one extra re-render per card after its image loads whenever a forced aspect ratio (`"portrait"` or `"landscape"`) is active. Each re-render is cheap and isolated to a single card, so it is unnoticeable at typical paginated list sizes (40–80 items). If profiling on lists with 100+ items reveals jank, the fix is:
1. Skip the `onLoad` handler in auto mode (add `cardAspect !== "auto" ? handler : undefined`).
2. For scene and image cards, pass a `naturalIsPortrait` prop derived from the file's `width`/`height` query fields (already available on `SceneCardScene` and `SlimImageDataFragment`) so no `onLoad` measurement is needed for those types — only performer/gallery/studio/tag covers would still use the `onLoad` path.

### Filter AST

The native entity, file/folder and scene/image duplicate queries accept
only their `*_filter_ast` expressions. `FindFilterType` remains the pagination,
sorting and search envelope; nonempty string `ids` selects explicit entities.
List aggregates cover all matching rows independently of pagination. See
[native queries](docs/native-queries.md).

`FilterAST` and `FilterASTNode` live in `pkg/models/filter_ast*.go`. Historical
object conversion remains at legacy upgrade boundaries. Internal SQL builders
and repository callers still use typed condition structures; those are not a
second public query format. Do not delete these models solely because the old
GraphQL query arguments have been removed.

Group is the sole native API collection type. Scene inputs, filename parser
results and notifications use `groups`/`group_id`; the Movie API and duplicate
Movie hooks are removed. Provider movie-shaped input and historical filename
patterns normalize into native groups at the input boundary. Historical export
files and saved configuration are separate migration inputs.

### Saved filters — canonical AST contract

Saved filters persist criteria directly in `saved_filters.filter_ast` as of native schema 1000001. Historical conversion handles the transitional `__filter_ast` key, v2.5 excluded-value splits, and conflicting legacy edits once. Normal startup no longer reconciles database sidecars, and the old object-filter column and shadow table are removed. Pending legacy conflicts survive as review evidence in `saved_filter_import_conflicts`, with the canonical AST selected. Condition values inside the AST use the **labeled saved-criterion shape** (`{value, modifier, field?}`, e.g. `[{id, label}]` items), distinct from the GraphQL input shape used by `*_filter_ast` query arguments. See [native schema promotion](docs/native-schema.md).

Default filters persist in `default_filters` as of native schema 1000003. `internal/manager/default_filter_migration.go` imports historical `defaultFilters` and `forkDefaultFilterState` once, retains conflicting alternatives, and uses a durable database/configuration publication checkpoint so interruption cannot overwrite later native edits. The old startup reconciler and persistent compatibility shadow are removed.

`LEGACY-UPGRADE-ONLY` files follow the [frozen historical-input policy](FORK.md#legacy-upgrade-code). Do not update their codecs or ordinal tables to mirror new native UI/API features or add runtime callers. `pkg/filtermigration/boundary_test.go` enforces the conversion entry points. Removal also requires replacing the historical fresh-database bootstrap chain and preserving supported interrupted configuration upgrades.

`internal/manager/default_filter_update.go` updates one view transactionally through `configureDefaultFilter`. Choosing an imported alternative or keeping the current criteria requires the reviewed revision. The UI configuration response derives `defaultFilters` and `defaultFilterConflicts` from native records; these fields are not written back to YAML. Clients never replace the full UI configuration to change one default filter. See [native schema promotion](docs/native-schema.md) for import and recovery details.

The saved-filter API reads and writes only `filter_ast`. `SavedFilter.filter`,
`object_filter`, `findDefaultFilter`, `setDefaultFilter` and the obsolete
`migrateLegacySavedFilters` task are removed. Omitted/null saved criteria clear
the filter. Supported historical database/configuration conversion lives in
`pkg/filtermigration`; new native imports reject flat `object_filter` files.
Do not restore a live flat projection or silently discard edits to nested filters.

v3 encodes/decodes the persisted shape via `encodeFilterASTNodeToSaved`/`decodeSavedFilterASTNode` (`filter-ast.ts`); `makeFilterAst()` folds non-builder criteria (custom_fields etc.) into the root AND group and `configureFromSavedFilter` splits them back out. The compact encoding (`{k,o,c}`/`{k,f,m,v,cf}`) remains in URLs (`fa=` param). Its current `COMPACT_OPERATORS`/`COMPACT_MODIFIERS` tables in `filter-ast.ts` are append-only. The separate Go decoder in `pkg/filtermigration` reads frozen historical inputs; do not update its tables to follow new UI features.

### Performer names

Native schema 1000002 stores all names in `performer_names`, with ordered positions, a derived primary flag, and per-name auto-tag policy. Position zero is canonical. `performers.name`, `performer_aliases`, and the policy sidecar are removed; the model/API's name and aliases are read from this one set. Selecting an existing alias moves its policy with it and retains the previous canonical spelling. Performer merge and scrape-merge preserve name policies. Search and the `names` AST criterion include every name; `name` and `aliases` target their respective roles. Different performers may share the same name and disambiguation: matching must consider all candidates, never treat a display name as identity.

### Code generation

Running `make generate` runs gqlgen (Go GraphQL bindings) and graphql-codegen for v3. Run it after modifying `graphql/schema/`. The v3 dev/build/check scripts also regenerate their own bindings. See the [generation guide](ui/v3/docs/development.md#generation-and-builds).

Agent note for `make validate-ui-v3`: `ui/v3/src/core/generated-graphql.ts` is ignored, but `pnpm run gqlgen` still rewrites it. In a read-only sandbox, graphql-codegen can print `[SUCCESS]` for every step and then `pnpm run check` exits 1 with no TypeScript diagnostics because the hidden error is `EROFS: read-only file system, open '.../ui/v3/src/core/generated-graphql.ts'`. When validation fails with that exact shape, rerun `make validate-ui-v3` with workspace write access before chasing TypeScript, package scripts, or generated GraphQL content.

### Job system

Background tasks (scan, generate, identify, etc.) run through `pkg/job`. Jobs are queued and can be monitored via the GraphQL subscription `jobsSubscribe`.

New archive work uses `pkg/job.Durable` with native jobs, stable submission
acknowledgements, attempt history, and fenced worker leases. Domain writes and
attempt completion use its `Publish` transaction; hashing/probing happens before
publication and retains descriptor checks. This service supports `media.verify`
and, in native schema 1000040,
`album.backfill`, with `text.translate` added in schema 1000045. The HTTP server
runs its file worker when media tools are configured; `Checkpoint` preserves committed registration across effect retries.
Its independent metadata-only album worker checkpoints historical media choices
and gallery membership before durable plugin notifications. Application-only
preview/apply, status, cancellation and retry routes retain request identities
and never reapply a committed publication when resuming hooks.
Admission receipts stay immutable and status reports actual completion. Keep this
work out of the legacy in-memory queue. External gallery-dl workers use the
native API; their packages and deployment state live outside this repository.
See [native ingestion](docs/native-ingestion.md).

`ingest.RunCoordinator` owns source traversal windows and producer-scoped fenced
leases. Its native records coalesce equivalent requests, retain missing ranges,
checkpoint retries, defer repeated failures and exclude overlapping targets and
destinations. This mutable traversal state is separate from immutable file jobs.
External workers use the supported adapter, durable outbox and shared filesystem
locks. Host, n8n and sidecar launch policies belong to their installation;
website credentials remain in those worker environments.

`CatalogIdentityImport` imports a frozen performer registry through a reviewed
plan in native schema 1000028. It adopts saved catalog UUIDs, preserves historical
redirects, and imports explicitly bound account choices without overwriting
native metadata or later ownership decisions. Every original row has an outcome;
unbound/conflicting identities remain review evidence. One registry/namespace
cutover has an immutable replay receipt. Never infer account services or ownership
from directory labels or restore old plugin links over explicit unlinks. See
[registry import](docs/native-source-identity.md#importing-the-performer-registry).

`CatalogRegistryImport` follows with the same frozen registry in schema 1000029.
It imports qualified captured account identifiers and disabled catalog groupings,
retains all routing/history evidence, and resolves saved ownership where possible.
Directory labels and reused handles cannot silently establish account identity.
Mirror and native service accounts remain separately qualified. Original registry
account/catalog mappings support later catalog-content import; they never become
a second live ownership authority. No collection root or scrape target is inferred,
and import does not activate work. Preserve exact replay and atomic rollback.

`stash-prepare-catalog` reads recognized individual catalog layouts into bounded,
hashed snapshots with complete row/schema inventories and capture reconstruction
proofs. Original JSON strings, binary sidecar bytes and profile references survive;
the normalized sidecar view is not a second physical copy. Keep shared revisions
distinct from captures and validate references before reconstructing payloads.
Preparation reports `imported:false`. `stash-upload-catalog` sends those exact
bytes to application-authorized `CatalogSnapshot` services in native schema
1000030. Bounded chunk transactions commit staging records and receipts together;
the same frozen manifest resumes after lost responses or restart. `received`
means the snapshot's bytes/counts match, never that its source graph or native
domain mappings are complete. All families remain pending for the subsequent
importers. Staging is temporary migration input, not another live catalog writer.

`stash-prepare-automation` and `stash-upload-automation` retain a frozen operating
database through the same bounded receipt model in native schema 1000046. Its
source must match an imported registry. Preserve exact raw values and one frozen
input per source; retries use the original manifest and committed chunk position.
The receipt reports all ten families pending and `imported:false`, even when all
bytes have arrived. Receiving historical rows must not create native jobs, claim
work completed, or activate providers. Domain mapping and explicit activation
follow separately. See [frozen automation input](https://github.com/notsafeforgit/stash-integrations/blob/main/integrations/gallery-dl/README.md#frozen-automation-input).
Unknown schemas cannot silently disappear from a manifest. Use frozen copies and
the coordinated cutover boundary; upload does not activate jobs or metadata.

`stash-import-automation-translations` maps the received translation jobs and
targets in schema 1000047. Preserve exact request text, cached outcomes, raw
attempt/error history and original priority/retry deadlines. Unapplied targets
remain held until separately reviewed activation. Historical completions require
retained cache evidence and carry migration provenance with unknown provider
capture time; never fabricate a worker attempt. Legacy English rewrites remain
in the source receipt while the native unchanged result preserves the original.
Resolve source-qualified post identifiers only after known catalog evidence
imports finish. Aliases may share one target; completion can promote only an
untouched hold from this same import. Preserve later native edits, conflicting
caches and unmatched references. Mapping still reports `imported:false`; other
automation families, activation and overall reconciliation remain separate.

Schema 1000048 exposes reviewed translation activation through application-only
preview/apply/status routes. Each immutable receipt releases at most 100 exact
held revisions, preserving priorities and retry deadlines. Save the operation
UUID and reviewed plan before applying; retries return the original receipt
without releasing later holds or changing completed targets. Optional snapshot
bindings permit only original imported holds. Keep activation distinct from
worker admission and provider completion. `stash-activate-automation-translations`
saves private, hashed candidate pages and operation identities before Apply;
resume with the same plan, digest and endpoint. Preserve excluded native choices
and surface stale batches. Schema 1000049 adds independent collection translation
policies. Capture acceptance records the policy decision and at most two target
references in its own transaction. Repeated captures retain existing holds,
deadlines and outcomes; receipt replay never applies a later policy. A changed
collection requires policy review. Provider execution remains separately enabled,
and scheduling never selects scene/image fields. Existing policy migration and
enrichment/discovery caller conversion remain transition work.

`stash_ingest.metadata_fetch` is the isolated metadata-only extractor for native
enrichment. It shares retained post fields in bounded resumable transcripts,
keeps parent observation times across child retries, and excludes download jobs,
postprocessors, archives and cookie writes. Validate post identity before turning
these transcripts into native captures; extraction is not job completion.
Schema 1000050 now retains native enrichment targets and completion evidence.
Targets bind an existing post URL and exact collection revision. Repeated
retention preserves holds, exclusions, deadlines and completion; rescheduling
requires the current target revision. Completion requires retained gallery-dl
captures for that same post and collection revision. It must be part of the
calling worker's fenced publication transaction, and has no public mutation
endpoint. Connecting extraction to execution, importing legacy enrichment state,
and scheduled-service conversion remain pending.
Schema 1000051 binds enrichment jobs to exact target/source revisions and each
attempt to its authenticated producer. The internal coordinator checks the
recorded logical-root scope, current eligibility and lease/credential validity
through commit. Checkpoints retain one compact body per job, small immutable
receipts and original per-record producers across failover. Preserve the bounded
staging budget, historical acknowledgement replay, retry backoff and explicit
owner retry operation. Schedule changes cancel active jobs atomically. These
checkpoints are not captures or completion proof. Schema 1000052 adds publication
receipts and indexed record-to-capture associations. The internal publisher
consumes saved checkpoints, verifies each existing post identity, preserves the
observing producer and reuses capture/publisher/album/translation services in the
same transaction as target/job completion. Generic success cannot bypass it.
Keep publication replay independent of current source edits and normalization;
new publication must recheck its original deadline and current credentials/source
through commit. Schema 1000053 releases verified completed staging atomically
with publication, retaining native captures, original receipts/provenance,
unresolved references and a versioned integrity proof. Older publications retain
their bodies until verified release. Failed/cancelled evidence stays staged.
Preserve acknowledgement replay, exact storage accounting and validation before
opening a writer. Public worker routes, additional post adapters, shared
cooldown/fairness and stale-job maintenance remain pending. See
[enrichment work](docs/native-schema.md#post-enrichment-targets-and-completion)
and [metadata-only extraction](https://github.com/notsafeforgit/stash-integrations/blob/main/integrations/gallery-dl/README.md#metadata-only-extraction-for-enrichment).

Schema 1000071 adds immutable account listing definitions and one-page
`account.list_page` jobs. Definitions pin account/profile, collection revision,
root, runtime/policy and retry deadline; imported cursors require frozen mapping
proof. Page retention, job success and source reservation release commit
together. Preserve original producer/fence receipts, cursor continuity and the
shared download/enrichment cooldown and fairness rules. A successful page is
neither a completed listing nor matched/published post metadata. The scoped
worker API now exposes existing-definition admission, owned attempts and page/
failure acknowledgements. Preserve the original requested cursor and reject
receipts belonging to a failed job's later replacement. The Python
`DiscoveryClient` validates canonical definitions, original cursors and exact
page acknowledgements; it shares server-clock ownership with enrichment through
`JobLease`. Discovery claims use the full description to bind the listing's
policy/runtime. Producer schema 11 journals the original claim, exact page bytes
and checked receipt; discovery, enrichment and file events share one outbox byte
budget. Preserve saved bodies across restart and lease failover. A later native
job status cannot discard unacknowledged local data. `execute-discovery` now
executes one admitted page using a separately pinned account-listing profile;
`deliver-discovery` recovers original intents without website access or a new
claim. Save returned evidence before testing possibly expired ownership. Page
delivery is not completed enumeration or a published match. Scoped readiness
uses bounded active-job and collection-definition indexes. Its listing cursor
tracks inspected rows, so an empty filtered page can still have more work.
Readiness must not load page bodies or confer ownership. Application maintenance
ends stale source definitions and expired claims while retaining pages, receipts
and retry deadlines. Producer schema 12 adds `dispatch-discovery` for an explicit
collection/profile, with durable cursors and backoff. Saved delivery precedes
new source work; delivery-only mode requires no website profile. Idle traversal
is not completed enumeration or matching. Producer schema 13 includes discovery
in `dispatch-all`, with independent saved-delivery cursors for enrichment and
discovery before local profile loading. Collection lookup follows current grants
and discovers later registrations under a root; returned containers still need
policy/runtime readiness checks. Preserve profile/collection rotation across
restart and both journals when upgrading an outbox. Reviewed fresh searches now
recover missing-history searches while preserving earlier evidence. Actual
coverage, weak-candidate details and native page release remain transition work. See
[listing storage](docs/native-schema.md#durable-account-listing-pages).
`scrape.MatchDiscoveryListing` and `MatchDiscoveryPage` implement the pure
`retained-discovery-listing-v1` candidate policy. Preserve unchanged original
evidence when checking weak candidates after a detail fetch; inferred URLs must
not acquire the lookup-only filename/account shortcuts. Multiple media/context
records from one post share a candidate and retain record ordinals. Candidates
do not select identities or establish completion; complete-enumeration
reconciliation, native choices and atomic publication remain necessary.

Schema 1000072 retains reviewed discovery target bindings, atomic per-page
comparison receipts and candidates grouped across pages. Evidence points to
original page record ordinals rather than copying source payloads. Bind the
original held record SHA and native post revision; unconverted historical
candidates must block activation. `DiscoveryComparisonWorker` uses bounded
pending-target inspection and compares retained pages under a read transaction
before committing their small result. Native edits between preparation and
commit must fail. Keep original receipts replayable and preserve every competing
post ID; candidate limits must roll back rather than truncate. Enumeration
completion only describes the retained listing cursor, never historical
coverage, accepted identity or import completion. This worker does not fetch,
admit source jobs, publish metadata or release staging.

Schema 1000073 adds application-only discovery activation previews and immutable
operation receipts. Select original target ordinals/hashes and the exact legacy
listing definition; the preview derives and pins current native post revisions.
Apply repeats the plan check and commits the listing, all bindings and receipt
together. Lost responses must reuse the saved operation and hash, even after
later native edits or comparison progress. Never interpret activation as job
admission, source coverage, accepted identity or import completion. Producer
tokens cannot use the archive review routes. The saved operator command is
implemented; the review UI and detail/staging-release workflow
remain open. Target reviews derive coverage and blockers in one read transaction,
without loading page bodies. Preserve missing-history, weak/competing-candidate,
changed-source/post and existing-identifier distinctions. Empty blockers do not
accept an identity or create a durable approval token.

Schema 1000074 publishes complete, unique strong discovery matches through the
application API. Decode retained evidence under a read transaction, recheck the
original revision and current native choices, then commit the verified identity,
canonical URL evidence, shared capture/publisher/album/translation effects and
receipt together. Preserve the corroborating observation's actual time and all
selected-post records from the strongest page. Other page records remain staged.
Reuse original receipts after response loss or later native edits. Missing
history, unverified/competing candidates and identifiers owned by another post require
resolution; do not silently merge posts. Verify the retained receipt graph on
reopen and preserve it through backup. This does not release pages, assign
depicted performers or complete the catalog import.
`DiscoveryPublicationWorker` inspects at most 32 target rows per batch and uses
that same service for eligible matches. Readiness reads receipts and references,
not page bodies, and grants no ownership. Published targets and unresolved
reviews cannot starve later work. Both discovery workers must expose an idle
boundary after a full traversal of waiting targets; never wrap an empty final
batch directly to the start and keep polling busy. The server owns their
cancellable lifetimes; restart recovers from durable receipts.

Schema 1000075 retains reviewed discovery recovery as a new listing with
`recovery_of`, preserving the original saved cursor and page count. Use the same
activation preview/apply receipts. A recovery starts at the first page and
inherits the original account, collection, profile and frozen source references.
Keep retry delays, wait for a running producer, and cancel queued predecessor
work atomically. The predecessor cannot admit more pages, but its retained pages
remain comparable. New targets point to the original bindings: uncompared
earlier batches and differing candidates must block publication. Never
erase earlier evidence or claim that a fresh search recovers posts the source
no longer exposes. Startup and backup must retain both searches and their links.

Discovery detail comparison reuses the compact metadata-only fetch transcript.
It requires a weak candidate from the original retained listing and corroborates
the original frozen target evidence. The inferred URL/account/filename alone
cannot establish identity. Keep all competing candidates, pending children,
unresolved references and each record's original observing producer/time.
Application `detail-preview` routes are read-only: supplied bytes do not clear
blockers or become authenticated producer evidence. Schema 1000076 adds separate
`post.verify_candidate` jobs with pinned target/candidate/page/source evidence,
authenticated checkpoint provenance, immutable comparison results and scoped
worker HTTP. Preserve original records across failover and old acknowledgements
after source changes. Detail work shares metadata/download pacing, including
saved child-service cooldowns. Job success retains a comparison, including an
uncorroborated result; it cannot accept identity. Startup must
rederive its saved proof, and anonymisation must remove its private transcripts.
Producer schema 14 adds the Python detail journal, selected execution and scoped
collection/profile dispatch. Keep its `comparison` receipts distinct from
enrichment `publication` receipts. Reuse the shared lease/checkpoint workflow,
but preserve old outbox receipt shapes when extending it. Empty successful
extractor responses can become uncorroborated detail evidence; extractor failures
remain failures. Saved delivery must work before loading website profiles, and
all metadata journals share the outbox byte budget. Readiness visits only the
bounded active detail jobs and preserves current grants, runtime and retry delays.
Schema 1000077 connects authenticated detail results to the shared publication
service and background worker. Review exposes the latest completed result for a
sole weak candidate; negative results cannot be ignored in favour of older
corroboration. A completed detail can survive later listing comparisons without
changing the frozen source, original candidate evidence or native post revision.
Explicit publication pins `detail_job_uuid`; preparation reconstructs that
result's comparison and per-record provenance. All coverage, competing-candidate,
recovery and native-edit guards still apply. Preserve publication and checkpoint
replay after later edits. Automatic detail admission uses a separately selected
producer profile, complete unique-candidate review and existing indexed
references. Bound inspection by both definitions and targets and advance past
empty/blocked rows. Any previous job for the same candidate prevents automatic
readmission, including older target revisions and negative results. Recheck
automatic admission before commit while preserving exact replay. Producer schema
15 persists the candidate cursor without changing prior journals or retry
deadlines; saved delivery and existing jobs run first, and completed collection
traversals pause before polling again. Staging release remains to be integrated;
ordinary enrichment cannot treat an inferred URL as accepted.

`stash-import-catalog-evidence` maps a received snapshot's posts, profiles and
captures through core services in schema 1000031. It uses exact manifest/ordinal
checkpoints, bounded transactions and immutable per-row outcomes. Preserve mirror
namespaces, local-key scope, forgotten posts and conflicts; shared observations
must not invent an extra capture. Exact copied events can share native storage
without losing each original row or collection provenance. `mapped`/`review`
still reports `imported:false`; remaining catalog families and final semantic
reconciliation are required before retiring temporary staging. These historical
evidence mappings never select entity metadata or activate jobs.

`SourcePostLinks` in schema 1000032 retains deduplicated post URLs with separate
observations, evidence for post identifiers, and unselected publisher claims.
Claims preserve original account UUIDs through consolidation; they never become
capture publisher choices or performer attribution. Keep request replay, forgotten
post guards, revision checks for identifier changes and atomic rollback. Import
of catalog relationships uses these services through schema 1000033.

`SourceDocument` in schema 1000041 retains shared original document bytes,
exact parser evidence, literal collection/path/post associations, historical
head claims and guarded native selections. Preserve malformed and empty source
documents, unknown fields and explicit unlinks. Historical paths never authorize
filesystem reads. Application document routes expose bounded associations and
separate byte downloads; selecting a head does not apply entity metadata.
`stash-import-catalog-documents` maps frozen catalog rows through these services
in schema 1000042 after the evidence pass. It retains explicit selected heads,
or the historical reader's text-timestamp/hash fallback where no head exists.
Import receipts and the processed-record checkpoint commit with each bounded
batch. Preserve existing native choices, the original collection revision and
exact document bytes; paths do not authorize filesystem access. This pass does
not apply scene/image metadata or complete the whole catalog migration.
Native document UI remains separate transition work.
See [retained documents](docs/native-schema.md#retained-source-documents).

`stash-import-catalog-cleanup` retains the optional `metadata_prune_queue` in
schema 1000080 after snapshot receipt and evidence mapping. Preserve its source
scope and timestamp as immutable held intent, with bounded atomic progress and
raw review evidence for malformed rows. The legacy queue completed background
metadata cleanup after a catalog prune; it must not delete media, forget a
recreated post or cancel current native work during import. No execution/release
route is supplied. Keep whole-catalog completion false until reconciliation and
the remaining transition work pass. See
[historical cleanup](docs/native-schema.md#historical-metadata-cleanup).

`SourceTranslation` in schema 1000043 shares exact original/output text and
nullable language/provider facts, with separate immutable post provenance.
`stash-import-catalog-translations` imports frozen rows in bounded transactions
after the evidence pass, resuming by last source ordinal. Keep the historical
JSON-based input hash separate from the native original UTF-8 hash. Unknown
languages do not imply English; conflicting declared hashes retain review
evidence. Results, provenance and receipts commit together, without applying
entity metadata or scheduling provider work. See
[retained translations](docs/native-schema.md#retained-source-translations).

`TranslationWork` in schema 1000044 retains shared versioned requests and cached
outcomes, with per-post/field targets and immutable scheduling history. Cached
results do not release held targets or reset retry deadlines. Publication checks
the target revision, respects its due time and retains separate post provenance;
it does not apply scene/image metadata. No-text outcomes complete without a
fabricated translation. Scheduled callers must use the durable job lease to
fence publication. Bounded application inspection returns references instead of
repeating text. Schema 1000045 binds each translation job to at most fifty target
revisions, with one active batch per request, bounded admission, fenced cache
checkpoints and atomic publication. Cancelled or exhausted work requires an
explicit target retry; retry delays and holds cannot be bypassed by later
targets. The HTTP-owned translate-shell/Bing worker is opt-in through
`translation_worker_enabled` and `translation_shell_path`; migration and normal
startup leave it disabled by default. Provider calls have bounded output and
timeouts, and cancellation terminates the Unix process group. Translation history
import, reviewed activation and capture scheduling are implemented; remaining
operational imports, policy migration and review UI remain transition work. See
[translation work](docs/native-schema.md#translation-requests-cache-and-targets).

Schema 1000120 adds optional namespace translation defaults and selected post
display languages. `apply_to_library` queues bounded, restartable propagation
when translations complete, including inherited source-album text. It also uses
cached translations during later media intake. Preserve exact original metadata,
explicit edits/clears, and unrelated custom mappings. Historical text repair is
an explicit API backfill; it makes no source-site requests. See
[automatic translations](docs/native-ingestion.md#automatic-source-translations).

`stash-import-catalog-relations` advances a received snapshot after its evidence
pass, retaining accounts, handle history, post URLs/aliases and unselected
post/account claims. Frozen registry mappings and original account UUIDs remain
authoritative; legacy IDs must not become stronger native-service identities.
Mirror handle fields are retained as legacy labels. Every source row retains its
original values and mapped/review/unassigned disposition, including oversized or
invalid values. API summaries are bounded; full evidence is retrieved one row at
a time. Completed passes remain `imported:false`. Captured publisher selection,
remaining catalog families and final reconciliation are separate work.

`stash-import-catalog-publishers` runs after both evidence and relationship
passes in schema 1000034. It applies the shared captured-account policy to actual
capture rows, linking qualified IDs or creating accounts when the core policy
allows it. Shared observation parents are not additional captures. Preserve
existing publisher choices and explicit unlinks; missing author IDs remain
unavailable, while ambiguous or invalid evidence retains review context. Never
infer the publisher from a folder, feed owner or historical catalog association
in this automatic capture-ID pass.
The explicit [catalog association repair](docs/catalog-association-repair.md)
can subsequently use unambiguous imported post-account and known author-folder
evidence, preserving conflicting authors and explicit choices. This is an
owner-requested historical backfill, not the automatic new-capture policy.
Exact manifest/ordinal checkpoints commit decisions, account evidence and
immutable receipts together. This pass selects source publishers, not depicted
performers or media metadata. Whole-catalog status remains `imported:false`.

`stash-import-catalog-attachments` maps explicit source attachment lists from
received captures in schema 1000035. It requires completed evidence mapping,
shares identical manifests, and combines compatible partial lists through the
core selection service. Automatic migration choices preserve pinned/disabled
selections just as ingestion does. Keep source positions, missing slots and
manifest completeness distinct from downloaded-file availability. Missing
original lists, unsupported formats and download numbering cannot establish an
album. Bounded immutable receipts retain conflict context; source payloads stay
shared. Media associations, gallery construction and the remaining catalog
families still need their subsequent import work.

Schema 1000036 lets `SourceAttachment.RecordMediaEvidence` retain post-level
legacy/review associations without invented attachments, capture provenance or
source order. Supply `PostUUID` explicitly. Optional capture/attachment references
must belong to that post; together they must prove capture membership. Verified
file evidence retains the full attachment/capture/file requirements. Recording
evidence advances review revisions but never selects media, changes attribution
or constructs galleries. `PostMediaEvidence` pages all associations for one post.

Schema 1000081 adds reviewed `SourcePostMedia` associations independently of
attachment slots. Decisions require current post/media revisions and the complete
current decision set, retain immutable replacement history across media merges,
and use a durable request UUID for exact replay. A whole-post rejection suppresses
attachment-derived metadata and automatic gallery membership; conflicting merged
choices require review. Preserve manual gallery membership and already selected
metadata. Metadata policy source choices accept a capture and exactly one current
attachment or reviewed post decision, retaining that decision in field provenance.
This permits historical NFO captures without inventing albums. The migration
selects no associations. See [the association API](docs/native-ingestion.md#reviewed-post-to-media-associations).

Schema 1000082 adds guarded historical post/file matching with durable request
receipts and foreign-key-linked proof references. The `catalog-files-v1` policy
uses current file/archive generations, retained path/content matches and unique
media ownership. Preserve all explicit post choices and hold competing matches
or unresolved attachment rejections for review. Batch decisions synchronize an
existing gallery once per post; no attachment or album is fabricated. Revalidate
proofs before commit, and replay original receipts before inspecting changed
state. `stash-backfill-post-media` prepares immutable bounded plan parts and
resumes through the application API. Populated matching and source-policy preview
reconciliation remain release gates recorded in the progress document.

Scene/image Sources review uses targeted, indexed media-to-post lookups and
compact capture summaries. Initial reads never reconstruct payloads or apply
retained evidence. Expanded history groups shared post metadata by revision and
distinguishes observation time from historical recording time. Link, unlink and
attachment-default choices save exact requests in deployment-scoped IndexedDB
before delivery; recover original receipts before retrying. A rejected revision
guard permits fresh review, while UUID reuse or a mismatched receipt must retain
the pending intent. Refresh the affected card after a successful change. Preserve
both desktop tabs and mobile section navigation, and do not infer performers from
the publishing account.

Schema 1000037 adds `SourceFile` records for shared source content claims,
root-relative file observations and guarded matches to existing library files.
Claims are unverified source evidence, including path-derived legacy asset IDs;
never turn them into verified `media_contents`. Observations retain missing,
pending, deduplicated and converted paths without creating playable files.
Matches fence both the member and archive generations for ZIP members. Exact
replay remains valid after deletion or UUID adoption, while new matches must
validate current files. Post-file evidence can retain unavailable appearances
without inventing attachment/capture scope. Logical roots can remain disabled
and unbound during migration; a historical mount prefix is not a filesystem grant.

`stash-import-catalog-media` uses schema 1000038 to bind an exact received snapshot
to a reviewed logical root/revision and historical library mount. Its asset,
file and appearance phases advance by processed-record count, not source ordinal.
Native facts and receipts commit atomically in bounded batches. Preserve source
states, converted survivors, unavailable media and ambiguous candidates. Stash
file timestamps have whole-second precision; retain original catalog nanoseconds
without turning precision loss into a conflict. Actual matches must validate
current file/archive generations and unique media ownership. This pass records
post associations without inventing attachment order or changing selected
metadata. Whole-catalog completion and gallery construction remain separate.

Schema 1000039 retains post collection membership independently of captures,
files, publishers and depicted performers. `stash-import-catalog-memberships`
maps historical collection keys to disabled native groups shared across catalogs
from the same registry. Legacy `creator` memberships were inferred from folder
punctuation; import them as directory groups without owner attribution. Retain
historical definition revisions, labels and original receipts; later native
renames or retirement do not rewrite membership evidence. Do not activate
scrapes, infer URLs or create galleries from these group labels.

`SourceGallery.PreviewBackfill` proposes attachment-to-media choices from the
selected post's imported appearance/file evidence. `source-identifiers-v1`
requires qualified retained Reddit/Twitter media IDs; the explicit
`legacy-reddit-filename-v1` policy also accepts the original Reddit
`post-id_media-id_...` filename convention. The explicit historical
`legacy-twitter-filename-v1` policy can recover a missing partial list from the
known `<tweet-id>_<number>` original-filename convention. It follows retained
file matches through deduplication, preserves gaps, leaves total count unknown,
and uses `legacy:twitter:filename` slot references rather than invented native
media IDs. It creates no captures or copied post payloads. Existing selections
and captured lists win; recovery's existing capture reference supplies metadata
only. See [source albums](docs/native-source-albums.md#recovering-historical-twitter-albums).
The `linked-post-media-v1` policy also recovers an album from two or more
distinct, active, confirmed post-media links when no source list was retained.
It records an empty partial album declaration with unknown order and total;
membership comes from those existing decisions, never invented attachment IDs.
Automatic album sync includes confirmed unordered members while protecting
pinned lists, rejected attachments, gallery exclusions and manual members.
Preserve every existing attachment decision, including a
deliberately undecided review. Ambiguous candidates, changed file generations,
changed ownership and media-kind disagreements remain unresolved. A bounded
query must fail rather than present a truncated candidate set as unique.
The managed-transaction `Backfill` method validates its preview, records legacy
attachment evidence without fabricated captures, and calls the shared gallery
sync service. The application preview/apply API admits its durable album worker,
which checkpoints publication before retryable after-success notifications.
Keep publication and notification replay separate when extending this workflow.

### v3 extension points

See `ui/v3/docs/architecture.md` for the current module map and compatibility rules. List configurations require a discriminated GraphQL/local `source`; generated query variables remain typed through the data hook. Layout, preferences, query state, and cache refill live in separate list modules. Player transition policy consumes plain buffered/seekable state in `scene-player-transitions.ts`; media effects, recovery, and transcode leases are separate hooks. Keep the stable player root and existing iOS seek/timeline behavior when extending these modules.

Accessibility lint is enabled in v3. Use named native controls, associate labels with unique input IDs, and document narrow exceptions at component wrappers or gesture delegation. General page pinch and double-tap zoom remain disabled by product choice; retain custom image/video zoom.

Interface text is non-selectable by default. Keep selection in inputs, editable regions, and technical `code`/`pre` output. Mark other copyable values (file paths, URLs, hashes, IDs, logs) with `data-selectable-text`, or use `selectableText` on `MetaRow`/`textColumn`. Do not opt whole panels or control labels into selection.
