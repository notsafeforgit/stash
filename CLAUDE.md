# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

Stash is a self-hosted media organizer written in Go (backend) + React/TypeScript (frontend). It exposes a GraphQL API, manages a SQLite database, and wraps FFmpeg for video processing.

This repository is now an **independent fork** of upstream stashapp/stash. Work continues on `v3-rewrite` under the [native archive transition plan](docs/native-archive-transition-plan.md); merge into `develop` follows migration verification and the owner's success review. Read [FORK.md](FORK.md) for migration, release, and upstream-import policy. V2.5 compatibility is frozen at `v2.5-compatible-final`, not a constraint on new code. The native application is running in production; [transition progress](docs/native-archive-progress.md) records the remaining worker rollout and restore verification. The [documentation index](docs/README.md) links current guides.

The owner requires GitHub Actions-built, GHCR-published production images. Do not
deploy another local-image fallback or treat a Git push as publication. Follow
the [deployment runbook](docs/v3-deployment.md), retain the actual run URLs and
source/wrapper digests, and report any GitHub-side execution block explicitly.

New plugins use the independent [`apiVersion: 3` contract](docs/plugin-manifests.md). Preserve its settings, UI contributions, capability checks, and after-success notifications. Only `apiVersion: 3` manifests load. Unversioned manifests and legacy UI injection are rejected and reported through `pluginLoadErrorsV3` and Settings → Plugins. Native catalog invariants belong in core services. New Twitter captures retain numeric thread/parent/publisher relationships in schema 1000105; evidenced self-replies share a source gallery while retaining each post and its attachment order. See [source albums](docs/native-source-albums.md#twitter-threads-and-replies).

The build commands and storage-bridge descriptions below describe the code as
it is converted. They are not requirements to keep two UIs, legacy API shims,
or dual migration tracks. Update each section with its implementation change;
do not remove a required caller without converting and testing it.

## Development quickstart

```bash
make pre-ui       # Install v3 dependencies
make pre-producer # Install the isolated gallery-dl producer test runtime
make pre-backup   # Install the isolated host backup test runtime
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
make validate-fork     # Fork gate: backend generation, v3 validation, Go lint/tests
make it                # Go unit + integration tests only
make validate-producer # Python delivery, lease and gallery-dl lifecycle tests
make validate-library  # Native manual tag/performer helper contracts
make validate-archive  # Portable archive integrity, restore and offline access
make validate-backup   # Host publication, native/media bindings and restore policy
make lint              # CI-pinned golangci-lint via go run
make fmt               # Format Go source
make validate-ui-v3    # Biome, generation/types, formatting, locales, tests, native contracts
make fmt-ui-v3         # Format v3 source
make validate          # Alias for validate-fork, including producer tests
make validate-ui       # Native UI validation; validate-ui-v3 is an alias
```

Build the native UI before full Go tests or `make validate-fork`; embedded-asset tests
require real v3 route chunks. Use the [validation sequence](ui/v3/docs/development.md#validation).
To run a single Go test: `go test ./pkg/models/... -run TestFilterAST`.
The shared `make test`/`make it` package timeout is twenty minutes for the growing
migration suite; override `GO_TEST_TIMEOUT` when needed. Individual operation,
lease and provider deadlines remain independently tested.
Native producer tests require Python 3.12 or newer. `make pre-producer` installs
the pinned gallery-dl/yt-dlp test dependencies into `.local/native-producer`;
`PRODUCER_PYTHON` can select another prepared environment. The supported package
in `integrations/gallery-dl` uses only the standard library for delivery.
Producer/backup installation uses Python `-I` so workspace `PYTHONPATH` cannot
redirect pip's uninstall discovery. Producer wheel builds discard obsolete
`build/lib` modules, and both setup targets verify installed source files with
`scripts/verify_producer_install.py`. A staged worker policy must fingerprint the
same code as the actual installed runtime; source-import tests alone cannot prove it.
Host/n8n launch paths now select the native adapter. The n8n initial-profile
queue and native download worker are running; the remaining host schedules are
still held for their controlled rollout checks. Do not equate an installed
launcher, active timer or admitted job with completed ingestion.
`integrations/archive` provides the standard-library `stash-archive` tool for
compressed native SQLite snapshots, original artwork and explicit configuration
or operating-state components. See the [portable archive format](docs/native-archive-format.md).
Restore verifies complete contents into a new directory and never activates a
server or worker. `export --server` captures the application's fixed WAL view,
configuration, runtime overrides, TLS assets and deletion recovery trees after
the declared producer/download snapshots. The authorized publisher can release
temporary components after durable publication; retained checkpoint/release
records prevent the same UUID from capturing newer state. Complete external-file/
media inventory, publisher/abandoned-stage retention integration and S3/restore activation remain required;
declared component coverage does not establish a complete production backup.
External SQLite state such as n8n executions uses the `operating_database` role.
Capture committed WAL contents with SQLite backup, retain its original staged
bytes on retry and verify database metadata again on restore. Include matching
encryption/configuration files and external execution payloads separately;
worker publication locks alone do not stop unrelated n8n writers or pruning.
The host backup's optional `quiesce_containers` setting briefly pauses explicit
rootless Podman containers after taking worker barriers. Separate bounded
systemd guards resume their exact IDs even if the publisher dies; sealed retries
validate the original evidence without pausing again. Enable this for n8n only
at the coordinated native handoff, with its database and payloads inventoried.
Worker inventories can declare `state_directories` for changing external payload
trees, including n8n `binaryData`. Enumerate and hash their regular files with
bounded traversal, retain empty directories, and reject changes before sealing.
Keep live SQLite databases outside opaque trees and declare them explicitly.
The optional external-boundary handshake keeps the native writer guard until a
bounded host confirmation and seals that evidence with the checkpoint. Establish
producer barriers before requesting that guard; the server never executes
filesystem provider commands. Sealed retries must reuse the original view.
The host-side `ArtworkPins` provider retains original artwork inodes during that
callback and binds export/retry to the sealed evidence. Retire pins through
publication-aware release or server-fenced unsealed abandonment, keeping small
receipts and resuming interrupted cleanup. Native gallery-dl mutations share a cooperative publication
lock. The host acquires `PublicationBarrier` across the inventoried worker lock
roots before the database guard, then releases it after retaining immutable
views. Existing backup/dedupe exclusion is still required; legacy workers do not
participate. `ZFSMedia` retains a held, GUID-bound snapshot of an explicitly
inventoried Linux ZFS dataset; nested datasets/mounts fail closed.
`HostFilesystemCapture` coordinates that view and artwork pins with the native
handshake, releasing producer barriers before the large database copy and on
sealed replay. The enclosing host backup keeps its existing backup/dedupe lock.
Daily-script integration, complete component/media-manifest inventory and remote
publication verification remain required; the providers do not upload to S3.
Host backup retries use `HostFilesystemCapture.prepare` to persist the original
download archives, producer outboxes and external configuration in a private
`ComponentStage`. Seal that stage before media publication, then pass it to the
exporter. Reopened or uncertain attempts only GET an already sealed server view;
they cannot create a later snapshot with old worker state. The stage inventory
is bound into the server's filesystem receipt and checked again while packing.
Publication-aware component release validates the complete archived inventory,
supports interrupted cleanup and keeps permanent identity/receipt files.
The tracked host tools in `integrations/backup` now publish v4 native/media
manifests through host-owned S3 clients, require verified object checksums,
and support native download/audit plus historical media restore inputs. Their
isolated runtime (`make pre-backup`) and regression gate (`make validate-backup`)
are separate from the server. Installed scripts and scheduling remain unchanged.
Native archive publication reuses full-checksum receipts from a private,
rebuildable `object-receipts.sqlite3` index after a complete paginated inventory
confirms unchanged object identity. New/unknown/changed objects still require
checksum verification; explicit audits and downloads bypass cached evidence.
Use 64 MiB raw chunks for new portable exports while retaining 1 MiB input support.
Keep database/record images in Standard and bulk media in Deep Archive, with
bounded retention and request-count tests. New cold-media objects use immutable
content keys in Deep Archive; existing verified filename objects can be reused.
Do not require bucket versioning to compensate for mutable filename keys.
The media readers accept v4 object descriptors with separate restore paths and
a bound cold store, retaining v2/v3 historical input. Native selection v2 binds
those identities into the portable archive. Deduplicate remote requests by
object key; different source paths can share bytes. Offline restore verifies all
selected bytes before extraction and rejects changed inputs and path collisions.
Cold checksum audits are explicit (`--media-checksums`), never a routine daily
HEAD sweep. The cold ledger now retains verified checksum receipts, path bindings
and interrupted-upload associations. One complete inventory reuses unchanged
proofs; matching renamed/duplicate videos share objects. Cleanup protects all
current references and rechecks returning or changed live paths before tagging.
Historical filename videos without additional S3 checksums may use matching
plaintext/SSE-S3 single-part MD5 or 5/16 MiB multipart ETags plus independently
computed local SHA-256. Verify complete local bytes before adoption; shape,
part count and uploader metadata cannot prove integrity. Reject KMS/SSE-C and
unusable modern checksums. New uploads, content keys and archive publication
still require full-object S3 checksums. Durable receipts avoid repeated hashing
or per-object HEAD requests for unchanged media. Whole-library adoption remains
a deployment check; passing sampled matches does not complete it.
Retained selections verify every selected object without rescanning. Historical
v3 in-flight native attempts require their original writer. Production inventory,
Standard reclamation, full cost measurements and cutover review remain open.
Successful native commits now publish immutable history receipts before releasing
their captures. Retain seven successful snapshots by default plus configured
archive UUID pins. Retirement decisions are durable; every retained snapshot's
cold-media references protect cleanup, and deferred historical deletion intents
remain queued. History caches rebuild from remote receipts and verified masters
using paginated listings without daily per-media HEAD requests. The real 64 MiB
encoder measurement is recorded in `docs/native-backup-cost-measurement.json`;
synthetic title edits do not establish daily production churn. Opt-in Standard
cleanup now protects all retained native graphs, records chunk retirement before
expiring per-run inventories, and reclaims known large local metadata after
release/retirement. Tag state is invalidated durably before mutation; publication
cancels expiration and verifies presence before reusing an object. Unchanged
runs avoid per-object tag/HEAD sweeps. Native archives already contain the host
ledgers; duplicate mutable/per-run ledger uploads are removed. Existing legacy
ledger backups remain untouched. Review and verify the scoped lifecycle and
versioning configuration before enabling `standard_cleanup`; the publisher
cannot install policy. Actual policy activation and expiration remain open.
Host publication/history/recovery/audit/restore share a 512 MiB master JSON
limit, with 1 MiB extra for the archived media-binding envelope. Native artifact
inventories have a separate 1 GiB streamed transport limit; the portable small
manifest and record bounds remain unchanged. Download inventories and encoded
objects in bounded reads, verify their complete digest/length, then publish a
new local file. The measured 272,373-video shape exceeds the former 128 MiB cap
with SHA-256 descriptors; see `docs/native-backup-manifest-scale.json` for the
isolated publication/history/restore proof and its synthetic-data limits.
An optional `worker_inventory` declaration resolves download/metadata worker
profiles, layered private references, helper assets, cookie files and archive
templates under the declared worker barriers. The host retains the closure per
run and verifies parsed dependency hashes against the actual component snapshots
before sealing. Retries use the original closure. `stash-s3-inventory` performs
read-only inspection with explicit container mappings and never prints resolved
credentials. Match declarations to actual launchers and published workflows;
automatic dependency resolution does not prove that every deployment was listed.
The host now journals one active attempt and resumes its original sealed view,
media selection or published cleanup after restart. Owned scratch is reclaimed
only under backup exclusion, which upload/validator children inherit. The S3
current pointer uses a retained conditional-write token and cannot replace a
newer publication on retry. Complete production worker/config inventory,
media-generation reconciliation, full capture measurements and relocated
restore/cutover review remain required.
Unsealed retries use the authenticated checkpoint status/abandon protocol before
scoped host cleanup. Admission and abandonment records permanently reserve UUIDs;
neither partial output nor a missing GET authorizes recapture. Preserve unknown
files, reject foreign mounts/holds/clones, and never force ZFS cleanup. ZFS command
supervisors retain the existing backup lock through sudo and caller timeout;
close the caller's descriptor rather than explicitly unlocking shared ownership.
Abandonment is a failed backup invocation; only the next run gets a fresh UUID.
Filesystem artwork writes publish flushed replacement inodes; never truncate a
blob in place because readers and backup pins may retain that inode. Orphan
cleanup must recheck references under a write transaction and use the deletion
journal, so it cannot bypass a native checkpoint or delete a newly adopted blob.
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
original ticket and cannot certify media intake. Staged host launchers for Twitter,
Reddit, Instagram, Coomer, Kemono, Bluesky and TikTok expand lists/modes/dates into source calls. Full-history
and Reddit top mode require a separate reviewed global `skip=true` profile.
Local recording is not source completion; strict inspection returns pending until
all original tickets finish. The staged n8n adapter records stable execution
identities, checks permanent history before source admission and retains its
completion proof before submission. Pending workflow results must wait and
inspect the same token; local recording cannot reach a success branch.
Instagram downloads retain versioned `instagram_media` membership before the
pinned extractor filters/reorders files. Preserve carousel slots and separate
story identity/date from its tray or highlight container. Profile dispatch may
queue only verified same-service child collections before a source date exists;
each child still applies the original window. New capture partitioning requires
that explicit evidence so historical Instagram signatures remain replayable.
The staged Instagram timer still needs native registration, durable outbox and
dispatcher/recovery activation at cutover.
Coomer/Kemono user/post/posts-listing downloads require `original=true` and retain
`mirror_media` version 1 before upstream file selection or mutation. Revalidate
primary/attachment/inline membership in the backend; primary aliases must not
create false albums, and selected download order must not replace source order.
Unsupported audio/archive outputs retain excluded captures without downloads,
file receipts or download-archive acknowledgements. Windows require original
`published` timestamps. New shared-body partitioning requires explicit evidence;
historical signatures remain unchanged. The staged mirror profiles/launchers are
inactive; Discord, favorites and artist containers still need separate adapters.
Bluesky/TikTok downloads retain `bluesky_media`/`tiktok_media` version 1 with
backend validation against original blobs/photos/videos. Keep missing slots,
repeat positions, DID-qualified posts, quoted post identity and original source
timestamps. TikTok image keys exclude signed URL/rendition changes; its generated
titles and selected images are per-file patches. Preserve historical capture
partitions. TikTok requires audio/covers/subtitles disabled and defaults to post
collections rather than profile avatars. Explicit unsupported includes fail;
following/artwork need separate adapters. Profile and shortlink children must
apply the original run window. Never allow the pinned TikTok loop's caught
extraction errors to certify a completed source run. Host profiles are staged
only, with the intentional TikTok media choices recorded for cutover review.
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
`stash-activate-automation-discovery` separately previews one explicit retained
account listing and target selection into a private review file. Apply/status
must reuse that file, endpoint and digest after interruption. Preserve the saved
cursor/history, source record hashes, current collection/root and worker policy;
binding targets does not admit source jobs or accept a post identity. Runtime
and website-access configuration remain producer-local.
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
- **Fclones-backed physical deduplication** — The host client uses `fclones group` for duplicate discovery. `pkg/dedup` previews those exact root-relative pairs and verifies complete bytes before journaled removal; it does not replace fclones with a new library-wide duplicate finder. Both locations must have the same single scene/image owner; retain original source evidence and derived survivor matches. Keep the existing backup/download exclusion in the host caller and recover uncertain requests by their UUID. The catalog-backed host launcher has not switched. See [fclones-backed file deduplication](docs/native-file-deduplication.md).
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

The eight native entity list queries and scene/image duplicate queries accept
only their `*_filter_ast` expressions. `FindFilterType` remains the pagination,
sorting and search envelope; nonempty string `ids` selects explicit entities.
List aggregates cover all matching rows independently of pagination. See
[native queries](docs/native-queries.md).

`FilterAST` and `FilterASTNode` live in `pkg/models/filter_ast*.go`. Historical
object conversion remains at import boundaries. File/folder nested filters and
internal repository callers still use object filter types while their separate
retirement proceeds; do not delete those models
solely because the entity query arguments have been removed.

Group is the sole native API collection type. Scene inputs, filename parser
results and notifications use `groups`/`group_id`; the Movie API and duplicate
Movie hooks are removed. Provider movie-shaped input and historical filename
patterns normalize into native groups at the input boundary. Historical export
files and saved configuration are separate migration inputs.

### Saved filters — canonical AST contract

Saved filters persist criteria directly in `saved_filters.filter_ast` as of native schema 1000001. Historical conversion handles the transitional `__filter_ast` key, v2.5 excluded-value splits, and conflicting legacy edits once. Normal startup no longer reconciles database sidecars, and the old object-filter column and shadow table are removed. Pending legacy conflicts survive as review evidence in `saved_filter_import_conflicts`, with the canonical AST selected. Condition values inside the AST use the **labeled saved-criterion shape** (`{value, modifier, field?}`, e.g. `[{id, label}]` items), distinct from the GraphQL input shape used by `*_filter_ast` query arguments. See [native schema promotion](docs/native-schema.md).

Default filters persist in `default_filters` as of native schema 1000003. `internal/manager/default_filter_migration.go` imports historical `defaultFilters` and `forkDefaultFilterState` once, retains conflicting alternatives, and uses a durable database/configuration publication checkpoint so interruption cannot overwrite later native edits. The old startup reconciler and persistent compatibility shadow are removed.

`internal/manager/default_filter_update.go` updates one view transactionally through `configureDefaultFilter`. Choosing an imported alternative or keeping the current criteria requires the reviewed revision. The UI configuration response derives `defaultFilters` and `defaultFilterConflicts` from native records; these fields are not written back to YAML. Clients never replace the full UI configuration to change one default filter. See [native schema promotion](docs/native-schema.md) for import and recovery details.

The saved-filter API reads and writes only `filter_ast`. `SavedFilter.filter`,
`object_filter`, `findDefaultFilter`, `setDefaultFilter` and the obsolete
`migrateLegacySavedFilters` task are removed. Omitted/null saved criteria clear
the filter. Historical JSON exports, primary schemas and configuration are
supported only through their import boundaries; `pkg/models/filter_ast_compat.go`
retains those conversion/reconciliation helpers and the URL codec. Do not restore
a live flat projection or silently discard edits to nested filters.

v3 encodes/decodes the persisted shape via `encodeFilterASTNodeToSaved`/`decodeSavedFilterASTNode` (`filter-ast.ts`); `makeFilterAst()` folds non-builder criteria (custom_fields etc.) into the root AND group and `configureFromSavedFilter` splits them back out. The compact encoding (`{k,o,c}`/`{k,f,m,v,cf}`) survives only in URLs (`fa=` param) and its Go decoder (`DecodeCompactFilterAST`); its int tables mirror `COMPACT_OPERATORS`/`COMPACT_MODIFIERS` in `filter-ast.ts` and are append-only.

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

`stash-prepare-automation` and `stash-upload-automation` retain a frozen operating
database through the same bounded receipt model in native schema 1000046. Its
source must match an imported registry. Preserve exact raw values and one frozen
input per source; retries use the original manifest and committed chunk position.
The receipt reports all ten families pending and `imported:false`, even when all
bytes have arrived. Receiving historical rows must not create native jobs, claim
work completed, or activate providers. Domain mapping and explicit activation
follow separately. See [frozen automation input](integrations/gallery-dl/README.md#frozen-automation-input).
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
and [metadata-only extraction](integrations/gallery-dl/README.md#metadata-only-extraction-for-enrichment).

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
`post-id_media-id_...` filename convention. Filenames never establish album
identity or order. Preserve every existing attachment decision, including a
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
