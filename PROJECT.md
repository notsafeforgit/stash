# Project policy

Native Stash is a standalone media library derived from Stash. This project owns
its architecture, schema, APIs, UI, plugins and release lifecycle. Its
[architecture and upgrade boundaries](docs/native-archive-transition-plan.md)
cover shared application behavior. Personal automation and production records
live in the separate `notsafeforgit/stash-ops` repository; they are optional API
clients and do not participate in the Stash application build. Reusable Python
clients live in `notsafeforgit/stash-integrations` with independent releases. Core
retains its API/data contracts and tests them against pinned external clients;
ordinary application validation must not install gallery-dl.

## Branch and release policy

- Continue development on `develop` in incremental, reviewable commits. The
  owner approved the native rewrite merge after verification on 2026-10-18.
  Do not recreate the retired `v3-rewrite` branch, rebase published implementation
  commits or force-push releases.
- `v2.5-compatible-final` identifies the last supported compatible source.
  [Its release manifest](docs/releases/v2.5-compatible-final.json) pins the exact
  existing images. Never rebuild or move these final and dated release tags.
- Publish development Stash images to `develop` and `develop-<commit>` tags. The
  image includes the native runtime, Jellyfin FFmpeg, GPU drivers and `uv`.
  Verify the published digest and container checks before deployment; no second
  `stash-s6` build is required. Frozen compatible images remain unchanged.
- `v3.0.0-dev` anchors native development version names. Subsequent builds use
  `git describe` from this tag rather than the frozen compatible release. It is
  a development checkpoint, not a stable release; preserve its tagged revision.
- Production delivery must use images built and published by the repository's
  GitHub Actions workflows, then deployed from GHCR by verified digest. The owner
  explicitly rejected continued local-image deployments on October 9. Do not use
  another local build or republish one as a substitute when Actions is blocked;
  report the publication blocker and retain the running service until the
  required registry release is available.
- Keep production pinned to a verified digest. Routine releases update the
  existing native installation; they do not repeat its completed cutover,
  catalog imports, reconciliation or backup/restore exercises. Test changed
  upgrade behavior with isolated fixtures before deployment.

## Selective feature imports

Treat `stashapp/stash` as a source of selected major features. There is no routine
sync, merge, rebase, feature-parity or compatibility target. A remote named
`upstream`, if configured locally, is only a source lookup; it does not define
this project's architecture or release schedule.

For an explicitly selected feature:

1. Record the source commit SHA(s), source repository and the intended behavior
   in the commit or PR description. Preserve copyright and license attribution.
2. Review its dependencies and adapt it to this project's models, shared domain
   services, UUID API and shared UI components. Cherry-pick with `-x` only when
   the commit fits; otherwise port the behavior with the same source attribution.
3. Add focused regression coverage for the imported behavior and its native
   contracts. Run `make validate` and any affected client/browser suites.
4. Publish through the normal `develop` workflow. Do not import upstream release
   jobs, migration numbering, old API shims or contribution policy incidentally.

Security fixes are evaluated on their own merits; they do not require adopting
an upstream release. StashDB/stash-box remains a supported network protocol.
Preserve endpoint-qualified remote IDs and fingerprint algorithms.

The Go module path `github.com/stashapp/stash` is a retained package identity,
not an update source or compatibility promise. The `ui/v3` directory and
versioned API routes identify the current implementation and contracts. Moving
those paths is not a prerequisite for independent development.

## Database and domain ownership

New data belongs in normal SQLite migrations, models, repositories, and domain
services. Use foreign keys, unique constraints, bounded indexed lookups, and
transactional updates. One-to-one metadata tables may remain normalized; being
first class does not require flattening them into entity rows.

The native schema must have an explicit lineage and independent version range.
Validate it before writes, reject dirty/newer/foreign databases, and verify that
the frozen binary refuses the promoted schema. Production uses a distinct native
database path. An upstream schema number must never be mistaken for a native
schema merely because its integer matches.

Historical primary/fork schemas are supported input formats for one-time
promotion. Import all completed migration history and retained values before
removing the sidecars. Enumerate unknown fork objects and fail with a useful
report. Do not create new compatibility sidecars, live reconcilers, or parallel
write projections. Convert actual consumers when replacing a contract; do not
add runtime fallback readers for retired upstream formats.

Canonical performer UUIDs, accounts, source posts, shared revisions/captures,
field decisions, media associations, and durable jobs belong to core services.
Source publishers and depicted performers are separate relationships, including
for aggregator accounts. Preserve names, aliases, explicit unlinks, merge
redirects, UUIDs, local integer IDs, and provenance during import.

Filesystem effects are not made atomic by a SQLite transaction. Preserve the
deletion journal and commit markers, root validation, completion receipts,
postprocessor path changes, and producer outboxes. Required post-commit work
must survive restart. Never reclassify failed or pending work as completed.

Config and database publication need durable checkpoints. A failed or interrupted
migration must leave enough state for deterministic retry. Resolve or preserve
conflicting saved/default filters before dropping either representation. Do not
use an old binary to roll back a native database after native writes begin.

## Legacy upgrade code

Code that interprets supported upstream/official database or configuration
formats, and the frozen pre-native fork formats, is **legacy upgrade only**.
Mark dedicated files with `LEGACY-UPGRADE-ONLY` and identify their historical
input, upgrade entry point, and retirement dependencies. Keep native runtime
helpers separate from these readers. Versioned native-to-native migrations are
a separate responsibility; do not label the entire migration system disposable.

Historical input semantics are frozen. Do not adjust old codecs, ordinal tables,
field mappings, or reconciliation rules to follow current UI/API features.
Changes need a specific historical-input, interrupted-upgrade, or security fix,
with historical fixtures proving that existing supported inputs still convert
correctly. New native behavior belongs in native code and, when needed, a new
native migration. Do not turn upgrade helpers into runtime fallback paths.

`pkg/filtermigration` documents the filter conversion boundary. Its import guard
permits only the database promotion and checkpointed default-filter promotion;
normal APIs, native imports/exports and query execution use `models.FilterAST`.
Configuration promotion can resume at startup after a database upgrade; its
durable checkpoint must prevent reimporting over later native edits.

Removal requires retiring the corresponding supported upgrade inputs (or
providing a separate importer), preserving promised interrupted-upgrade recovery,
and removing all bootstrap dependencies. Fresh databases now install a tested,
versioned native baseline and apply later native migrations. Historical readers
remain required for supported upstream/frozen-fork upgrades and interrupted
historical initialization, independently of this fresh-install optimization. See the
[migration maintenance notes](pkg/sqlite/migrations/README.md).

## API, UI, and plugin contracts

Source tracking is an installation opt-in (`source_tracking_enabled`, default
false). Preserve its API, worker and UI boundaries; ordinary library folders,
provider metadata, playback and deletion recovery remain core. Disabling must
preserve existing source data and undelivered work. S3 publication remains an
independent opt-in host integration, never an application startup dependency.
See [optional integrations](docs/optional-integrations.md).

The native application owns its UI/API contract. Upstream UI, GraphQL and
plugin compatibility are not design requirements. Version the supported
contract, inventory actual callers, and regenerate them
together. Remove old adapters only after converting their consumers, including
host/n8n jobs, installed plugins, offline/share entry points, and backup tools.
Preserve intentional public media/share URLs and standalone/offline behavior.

Plugins must declare [`apiVersion: 3`](docs/plugin-manifests.md). Retain typed settings,
schema-constrained mappings, UI contributions on desktop/mobile, capability
negotiation, jq evaluation, and notifications after successful changes. Hooks
cannot veto committed changes. Native archive consistency must not depend on
catalogMetadata or any other plugin receiving a notification.

Keep captured website-format interpretation in the offline `pkg/sourceadapter`
package. Core archive services validate its claims and retain ownership of hashes,
scope checks, explicit decisions and transactional associations. Adapters must not
gain repository, filesystem, worker or network access; see the
[source interpretation boundary](docs/native-ingestion.md#source-interpretation-and-archive-validation).

Use shared domain services for both GraphQL and native ingestion. Producers send
idempotent events with durable receipts and scoped Stash API tokens; they do not write
the library database. Keep after-success behavior consistent across API edits,
background jobs, and imported changes. Never expose internal plugin settings in
mapping data without an explicit use case.

## Validation and documentation

Never edit generated Go/TypeScript bindings by hand. `make generate` regenerates
Go and UI bindings; `make ui` builds the sole embedded application and login
locales. `make validate` is the full local gate; it generates and validates the
assets before Go checks. `make validate-ui`, `make fmt-ui`, `make ui-start` and
`make test-ui-browser` address that same application. The retired UI
and its toolchain remain available only at the frozen compatible tag.

Test changed invariants and migration outcomes with real SQLite fixtures. Cover
restarts, retries, integrity, unknown inputs, lossless promotion, API callers,
and retained external protocols. Historical round-trip fixtures belong only to
legacy upgrade tests; they are not runtime compatibility gates. Keep security
and dependency checks for the application.

Rehearse supported database upgrades on copies, verify semantic preservation,
and keep a recoverable backup before changing a production writer. Routine native
releases do not repeat an installation's completed one-time cutover. Update shared
architecture, contributor, build and packaging guidance with implementation changes.
Keep installation-specific cutover evidence and schedules in the operations repo.
Do not describe planned work as deployed.
