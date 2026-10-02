# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

Stash is a self-hosted media organizer written in Go (backend) + React/TypeScript (frontend). It exposes a GraphQL API, manages a SQLite database, and wraps FFmpeg for video processing.

This repository is now an **independent fork** of upstream stashapp/stash. Work continues on `v3-rewrite` under the [native archive transition plan](docs/native-archive-transition-plan.md); merge into `develop` follows migration verification and the owner's success review. Read [FORK.md](FORK.md) for migration, release, and upstream-import policy. V2.5 compatibility is frozen at `v2.5-compatible-final`, not a constraint on new code. [Transition progress](docs/native-archive-progress.md) distinguishes completed work from the still-running compatible production deployment. The [documentation index](docs/README.md) links current guides.

New plugins use the independent [`apiVersion: 3` contract](docs/plugin-manifests.md). Preserve its settings, UI contributions, capability checks, and after-success notifications. Existing unversioned adaptation is scheduled for removal after actual callers are converted; do not extend it. Native catalog invariants belong in core services.

The build commands and storage-bridge descriptions below describe the code as
it is converted. They are not requirements to keep two UIs, legacy API shims,
or dual migration tracks. Update each section with its implementation change;
do not remove a required caller without converting and testing it.

## Development quickstart

```bash
make pre-ui       # Install v2.5 dependencies for the embedded fallback
make pre-ui-v3    # Install v3 dependencies
make pre-producer # Install the isolated gallery-dl producer test runtime
make generate     # Generate Go and v2.5 GraphQL bindings
make ui           # Build the embedded v2.5 UI
make ui-v3-only   # Generate v3 bindings and build its embedded assets
# Terminal 1:
STASH_PORT=9999 STASH_ENABLE_V3_UI=true make server-start
# Terminal 2:
VITE_APP_PLATFORM_URL=http://127.0.0.1:9999 make ui-v3-start
```

Open v3 at `http://localhost:3002/`. Set the backend URL explicitly; v3's proxy
otherwise defaults to port 8010. `make ui-start` runs the v2.5 reference UI on
port 3000. See the [v3 development guide](ui/v3/docs/development.md) for setup,
generation, and validation, and the [deployment runbook](docs/v3-deployment.md)
for image publication and Quadlet restarts.

## Build commands

| Command | Purpose |
|---|---|
| `make build` | Build `stash` and `phasher` binaries |
| `make build-release` | Release build (stripped debug info + PIE) |
| `make stash` | Build only the main binary |
| `make ui` | Build v2.5 embedded assets and login locales |
| `make ui-v3-only` | Generate, type-check, and build v3 embedded assets |

## Testing and linting

```bash
make validate-fork     # Fork gate: backend generation, v3 validation, Go lint/tests
make it                # Go unit + integration tests only
make validate-producer # Python delivery, lease and gallery-dl lifecycle tests
make lint              # CI-pinned golangci-lint via go run
make fmt               # Format Go source
make validate-ui-v3    # Biome, generation/types, formatting, locales, tests, native contracts
make fmt-ui-v3         # Format v3 source
make validate          # Upstream/v2.5 UI validation and backend checks; excludes v3
make validate-ui       # v2.5 Biome, Stylelint, TypeScript, and formatting checks
```

Build both UIs before full Go tests or `make validate-fork`; embedded-asset tests
require real v3 route chunks. Use the [validation sequence](ui/v3/docs/development.md#validation).
To run a single Go test: `go test ./pkg/models/... -run TestFilterAST`.
Native producer tests require Python 3.12 or newer. `make pre-producer` installs
the pinned gallery-dl/yt-dlp test dependencies into `.local/native-producer`;
`PRODUCER_PYTHON` can select another prepared environment. The supported package
in `integrations/gallery-dl` uses only the standard library for delivery.
Host/n8n launch paths have not switched to the native adapter.
The producer CLI can validate a portable worker profile and execute one claimed
source attempt with concurrent outbox delivery. Website-access references stay
local, and source completion remains distinct from verified media intake.
`stash-ingest-config` stages new profiles from ordered gallery-dl JSON layers.
Preserve conditional map order and layered private references; conversion does
not activate a worker or replace the existing configuration files.
`stash-ingest dispatch` discovers scoped work through the native API. Producer
schema 7 retains pagination/backoff, caller URL snapshots and each ticket's original submission
assignments. `ticket-status` checks completed source windows against those
assignments; later rescans cannot complete an earlier cancelled ticket. Discovery
and admission do not certify completion. Preserve existing request/event state
when upgrading older outboxes.
`lookup-collections` resolves exact current source URLs through the producer's
collection/root grants. Preserve missing and ambiguous candidates for review;
historical URLs must not silently redirect scheduled work to a new target.
An explicit root grant covers registered collections at that logical root,
including later additions; existing collection grants do not widen on migration.
It grants no source administration or unbound metadata access. Bounded lookup
responses expose truncation so a partial candidate set never appears unique.
`queue-sources` freezes a caller's URL list, policy and absolute window before
network access. Resolution commits each collection binding and ticket together;
never reinterpret a bound target after a retry. `call-status` checks every
original ticket and cannot certify media intake. Staged Twitter/Reddit host
launchers now expand the existing lists/modes/dates into source calls. Full-history
and Reddit top mode require a separate reviewed global `skip=true` profile.
Local recording is not source completion; strict inspection returns pending until
all original tickets finish. The staged n8n adapter records stable execution
identities, checks permanent history before source admission and retains its
completion proof before submission. Pending workflow results must wait and
inspect the same token; local recording cannot reach a success branch.
`source_backfill_decisions` now retains permanent account-backfill history in
the native database. Historical acceptance/skip imports retain their entire
source row without creating source-run coverage. Native completion proves every
component URL through the producer's original request hashes and completed
windows. The maintenance importer validates a read-only journal snapshot and
uses application-authorized batches; producer tokens cannot import historical
assertions. Producer schema 7 commits each n8n history check and its source
snapshot together; later account history cannot replace that caller's original
tickets. `stash-ingest-n8n-config` stages the known workflow graphs, preserving
IDs, credential references and result branches while adding a durable wait.
`stash-import-n8n-receipts` explicitly imports a reviewed frozen receipt snapshot
into the producer outbox. Exact bytes and manifests survive replay; old tokens
can be inspected locally but cannot become native completion proof or new work.
`stash-import-scan-journal` retains a reviewed frozen journal in the native
database with atomic receipts, bounded inspection and explicit dispositions.
It does not activate jobs. `stash-activate-scan-journal` separately previews and
applies an explicit source/profile/cutoff binding, consolidating exact target
requests while retaining deferrals and retry delays. Recovery never transfers
old process ownership or creates completion proof. The native worker understands
the original archive-key cursor; widened windows replay without archive stopping.
Production source registration, final cutover bindings and recovery callers
remain open.
`stash-backfill-source-albums` saves reviewed per-post previews and request UUIDs
before historical album admission. Reuse the same plan/digest after interruption;
status must match its original post, policy and signature. Explicit retry prepares
a new plan and preserves committed publication/event identity. Indexed selected-post
discovery retains exclusions, but multiple pages do not establish a global snapshot
under concurrent writers. Album plan completion is not catalog migration completion.

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
- **`pkg/sqlite/`** — SQLite implementation of the repository interfaces, including migrations.
- **`graphql/schema/`** — GraphQL schema. After editing, run `make generate` to regenerate Go bindings.

### Frontend (`ui/v2.5/`)

**Read-only reference — do not modify.** All active development is in `ui/v3/`.

- React + Apollo Client for GraphQL
- State: Apollo Client cache is the primary state layer; component state for UI-only concerns
- Routing: React Router v5
- UI: Bootstrap + custom SCSS components
- All GraphQL queries/mutations live in `ui/v2.5/graphql/`; generated TypeScript types in `ui/v2.5/src/core/generated-graphql.ts`

### Frontend (`ui/v3/`)

Active development target. A ground-up rewrite sharing the same GraphQL API.

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

**iOS Safari seeks & buffer behaviour — measured, not throughput-bound** (`pkg/ffmpeg/codec_hardware.go`, `pkg/ffmpeg/stream_segmented.go`, `src/components/player/use-scene-player-sources.tsx`, `src/components/player/scene-video.tsx`)

Two iOS-specific quirks that look like server slowness but aren't, with the workarounds wired in:

1. **Far-forward seeks must route through a source reload on iOS.** On desktop MSE we can call `engine.stopLoad() → BUFFER_FLUSHING → engine.startLoad(target)` in place and the SourceBuffer recovers cleanly. On iOS Safari's `ManagedMediaSource` (iOS 17+) the same sequence leaves `video.buffered` stuck at empty after the flush — `readyState` pins at 1, the seek never resolves. Behaviour is the same in or out of native fullscreen. `use-scene-player-sources.tsx` passes `ios: isIOS()` to the pure planners in `scene-player-transitions.ts`, which choose a source reload for distant iOS seeks and restarts. A new `?start=<target>` lets a fresh hls.js engine cold-start at the right fragment; the player root remains mounted and the freeze frame masks the transition.

2. **MMS rate-limits hls.js fetches; visible "stalls" are buffer cycles, not transcode-bound.** Confirmed end-to-end on an Intel Arc A380 transcoding a 4K HDR HEVC source to H.264 via QSV: ffmpeg produces 2 s segments in ~800 ms (**~2.1× realtime**), full HW path engaged (`-hwaccel qsv -hwaccel_output_format qsv`, `-c:v h264_qsv`, `scale_qsv=format=nv12`). On the iOS client during the same playback window before tuning, `bufEnd` advanced only ~10 s of content per 18 s of wall clock — MMS releases bytes in pulses, hls.js pauses fragment loading whenever its quota is full, server sits idle ~half the time waiting. `bufEnd` plateaus for 4–5 s, then jumps forward when MMS lets more in. The visible "stall" is the tail of one of those plateaus when the safety margin gets close to zero before the next refill. To narrow the dips, `scene-video.tsx` bumps hls.js's `maxBufferLength=60`, `maxMaxBufferLength=120`, `maxBufferSize=240 MB` on the MMS path only — the default 60 MB byte cap binds first on 4K H.264 at ~50 Mbps, not the time cap. After tuning: buffer peaks at ~30 s, plateau cycle ~20 s, **margin floor ~10 s** (was ~5 s) — playback never drains close enough to zero to halt. The remaining `ev:stalled` events are iOS's "no bytes in 3 s" network warnings; `currentTime` keeps advancing through them. Desktop MSE path is untouched.

**Audio continuity and lightbox lifetime**: `scene-carousel.tsx` uses YARL's public module API for a keyed poster track and one persistent `ScenePlayer`, store and native video. During swipes the outgoing player retains its visual position and source, while the decoded incoming poster stays mounted. Scene changes wait for the actual track animation to finish; obsolete completions and outgoing EOF cannot load or advance another selection. YARL still owns dragging and animation. `playbackKey` resets scene/marker state without resetting media ownership. Pending queries/OPFS and loading sentinels suspend the retained player and release its source; late query results cannot replace the selected scene. Audio, playback rate and an active held-speed gesture survive navigation. Keep WebKit's explicit `canplay` resume. Only the initial entrance uses `usePlayDelay`, which retries muted only after a current `NotAllowedError`; new selections cancel old deadlines and seeks. Chromium/WebKit tests exercise the actual lightbox, but physical iPhone autoplay permission and MMS behavior still require device verification.

**Sizing the iOS buffer config**: the 240 MB / 60 s ceilings are headroom MMS rarely uses, not targets. At 4K (~50 Mbps) the byte cap is the only one that matters — 60 s × 50 Mbps ≈ 375 MB would exceed the cap, so we hit 240 MB first; in practice MMS caps us at ~30 s well below either ceiling. Stepping down to 1080p (~5–8 Mbps), 60 s of buffer is 38–60 MB — far under the byte cap, so the time cap is the only ceiling. Same shape at 720p / 480p. Slower networks are protected by the ~10 s floor between MMS refills, which absorbs WiFi drops and congestion spikes shorter than that. Sustained throughput below the chosen resolution's bitrate is unrecoverable by any buffer size — the user has to pick a lower resolution from the source list (no ABR in this codebase). Memory ceiling of 240 MB is comfortable on modern iPhones (≥ 6 GB RAM); MMS will trim SourceBuffer under memory pressure on older devices regardless of what we ask for.

To verify hardware transcode performance (vs. blaming the server again next time): set `STASH_TRANSCODE_DEBUG=1` and look for `[transcode] running ffmpeg ...`. Expect `-hwaccel qsv -hwaccel_output_format qsv -i ... -c:v h264_qsv ... -vf scale_qsv=format=nv12,setpts=PTS-STARTPTS` for Intel. If `-hwaccel qsv` is missing (only `-init_hw_device qsv=hw`) then `hwCanFullHWTranscode`'s 1 s probe failed for that source and the pipeline fell back to CPU decode → QSV encode — slow at 4K. Otherwise the server is fine and the bottleneck is downstream.

**Known performance trade-off — card aspect mismatch detection**
`EntityCard.Preview` (`src/components/cards/entity-card.tsx`) detects whether an image is naturally portrait or landscape by reading `e.currentTarget.naturalHeight / naturalWidth` in an `onLoad` handler. This causes one extra re-render per card after its image loads whenever a forced aspect ratio (`"portrait"` or `"landscape"`) is active. Each re-render is cheap and isolated to a single card, so it is unnoticeable at typical paginated list sizes (40–80 items). If profiling on lists with 100+ items reveals jank, the fix is:
1. Skip the `onLoad` handler in auto mode (add `cardAspect !== "auto" ? handler : undefined`).
2. For scene and image cards, pass a `naturalIsPortrait` prop derived from the file's `width`/`height` query fields (already available on `SceneCardScene` and `SlimImageDataFragment`) so no `onLoad` measurement is needed for those types — only performer/gallery/studio/tag covers would still use the `onLoad` path.

### Filter AST

The codebase has a two-filter system:
- **Legacy object filter** (`FindFilter`, `*FilterType`) — existing filter arguments passed directly as GraphQL input types
- **Filter AST** (`FilterAST`, `FilterASTNode`) in `pkg/models/filter_ast*.go` — a newer tree-based filter representation that can serialize to/from the legacy object filter format via `ToObjectFilter()`/`FilterASTFromObjectFilter()`

### Saved filters — AST canonical, v2.5 API compat as a removable layer

Saved filters persist criteria directly in `saved_filters.filter_ast` as of native schema 1000001. Historical conversion handles the transitional `__filter_ast` key, v2.5 excluded-value splits, and conflicting legacy edits once. Normal startup no longer reconciles database sidecars, and the old object-filter column and shadow table are removed. Pending legacy conflicts survive as review evidence in `saved_filter_import_conflicts`, with the canonical AST selected. Condition values inside the AST use the **labeled saved-criterion shape** (`{value, modifier, field?}`, e.g. `[{id, label}]` items), distinct from the GraphQL input shape used by `*_filter_ast` query arguments. See [native schema promotion](docs/native-schema.md).

Default filters persist in `default_filters` as of native schema 1000003. `internal/manager/default_filter_migration.go` imports historical `defaultFilters` and `forkDefaultFilterState` once, retains conflicting alternatives, and uses a durable database/configuration publication checkpoint so interruption cannot overwrite later native edits. The old startup reconciler and persistent compatibility shadow are removed.

`internal/manager/default_filter_update.go` updates one view transactionally through `configureDefaultFilter`. Choosing an imported alternative or keeping the current criteria requires the reviewed revision. The UI configuration response derives `defaultFilters` and `defaultFilterConflicts` from native records; these fields are not written back to YAML. Clients never replace the full UI configuration to change one default filter. See [native schema promotion](docs/native-schema.md) for import and recovery details.

### Performer names

Native schema 1000002 stores all names in `performer_names`, with ordered positions, a derived primary flag, and per-name auto-tag policy. Position zero is canonical. `performers.name`, `performer_aliases`, and the policy sidecar are removed; the model/API's name and aliases are read from this one set. Selecting an existing alias moves its policy with it and retains the previous canonical spelling. Performer merge and scrape-merge preserve name policies. Search and the `names` AST criterion include every name; `name` and `aliases` target their respective roles. Different performers may share the same name and disambiguation: matching must consider all candidates, never treat a display name as identity.

The v2.5 saved-filter API keeps working for mainline clients through two shims (`pkg/models/filter_ast_compat.go` + resolvers), both deletable once v2.5 support is dropped:
- **Read**: `SavedFilter.object_filter` resolves by flattening the AST into the flat v2.5 criteria map (`FlatObjectFilter`). Lossy for OR groups, nesting, and repeated fields; modifier-less wrapper values (custom_fields) unwrap to their bare legacy shape.
- **Write**: legacy `saveFilter` inputs convert via `FilterASTFromLegacySavedFilter`. A legacy-path save targeting an existing filter whose AST is not flat-representable is **silently dropped** (logged, returns the filter unchanged) so v2.5 clients can't destroy filter structure they never saw.

v3 encodes/decodes the persisted shape via `encodeFilterASTNodeToSaved`/`decodeSavedFilterASTNode` (`filter-ast.ts`); `makeFilterAst()` folds non-builder criteria (custom_fields etc.) into the root AND group and `configureFromSavedFilter` splits them back out. The compact encoding (`{k,o,c}`/`{k,f,m,v,cf}`) survives only in URLs (`fa=` param) and its Go decoder (`DecodeCompactFilterAST`); its int tables mirror `COMPACT_OPERATORS`/`COMPACT_MODIFIERS` in `filter-ast.ts` and are append-only.

### Code generation

Running `make generate` runs gqlgen (Go GraphQL bindings) and graphql-codegen for **v2.5**. After modifying `graphql/schema/`, run it and regenerate v3 with `pnpm --dir ui/v3 gqlgen` (or its dev/build/check scripts). See the [generation guide](ui/v3/docs/development.md#generation-and-builds).

Agent note for `make validate-ui-v3`: `ui/v3/src/core/generated-graphql.ts` is ignored, but `pnpm run gqlgen` still rewrites it. In a read-only sandbox, graphql-codegen can print `[SUCCESS]` for every step and then `pnpm run check` exits 1 with no TypeScript diagnostics because the hidden error is `EROFS: read-only file system, open '.../ui/v3/src/core/generated-graphql.ts'`. When validation fails with that exact shape, rerun `make validate-ui-v3` with workspace write access before chasing TypeScript, package scripts, or generated GraphQL content.

### Job system

Background tasks (scan, generate, identify, etc.) run through `pkg/job`. Jobs are queued and can be monitored via the GraphQL subscription `jobsSubscribe`.

New archive work uses `pkg/job.Durable` with native jobs, stable submission
acknowledgements, attempt history, and fenced worker leases. Domain writes and
attempt completion use its `Publish` transaction; hashing/probing happens before
publication and retains descriptor checks. This service supports `media.verify`
and, in native schema 1000040,
`album.backfill`. The HTTP server runs its file worker when media tools are
configured; `Checkpoint` preserves committed registration across effect retries.
Its independent metadata-only album worker checkpoints historical media choices
and gallery membership before durable plugin notifications. Application-only
preview/apply, status, cancellation and retry routes retain request identities
and never reapply a committed publication when resuming hooks.
Admission receipts stay immutable and status reports actual completion. Keep this
work out of the legacy in-memory queue. Existing scheduled scrapes have not switched. See [native ingestion](docs/native-ingestion.md).

`ingest.RunCoordinator` owns source traversal windows and producer-scoped fenced
leases. Its native records coalesce equivalent requests, retain missing ranges,
checkpoint retries, defer repeated failures and exclude overlapping targets and
destinations. This mutable traversal state is separate from immutable file jobs.
External workers still need the supported adapter, durable outbox and shared
filesystem locks before host/n8n launch paths switch to it. Website credentials
remain in those worker environments.

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
Unknown schemas cannot silently disappear from a manifest. Use frozen copies and
the coordinated cutover boundary; upload does not activate jobs or metadata.

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
infer the publisher from a folder, feed owner or historical catalog association.
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
`post-id_media-id_...` filename convention. Filenames never establish album
identity or order. Preserve every existing attachment decision, including a
deliberately undecided review. Ambiguous candidates, changed file generations,
changed ownership and media-kind disagreements remain unresolved. A bounded
query must fail rather than present a truncated candidate set as unique.
The managed-transaction `Backfill` method validates its preview, records legacy
attachment evidence without fabricated captures, and calls the shared gallery
sync service. Its caller must checkpoint publication with durable after-success
work before exposing an Apply operation. That application worker/API is still
pending; the core method is currently exercised on isolated rehearsal copies.

### v3 extension points

See `ui/v3/docs/architecture.md` for the current module map and compatibility rules. List configurations require a discriminated GraphQL/local `source`; generated query variables remain typed through the data hook. Layout, preferences, query state, and cache refill live in separate list modules. Player transition policy consumes plain buffered/seekable state in `scene-player-transitions.ts`; media effects, recovery, and transcode leases are separate hooks. Keep the stable player root and existing iOS seek/timeline behavior when extending these modules.

Accessibility lint is enabled in v3. Use named native controls, associate labels with unique input IDs, and document narrow exceptions at component wrappers or gesture delegation. General page pinch and double-tap zoom remain disabled by product choice; retain custom image/video zoom.

Interface text is non-selectable by default. Keep selection in inputs, editable regions, and technical `code`/`pre` output. Mark other copyable values (file paths, URLs, hashes, IDs, logs) with `data-selectable-text`, or use `selectableText` on `MetaRow`/`textColumn`. Do not opt whole panels or control labels into selection.
