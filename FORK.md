# Independent fork maintenance

This repository develops an independent native media archive based on Stash.
The [native archive transition plan](docs/native-archive-transition-plan.md)
defines the full migration scope and acceptance criteria.
[Implementation progress](docs/native-archive-progress.md) records completed
work, verification, and deployment status. The native application is running in
production. The original compatible database and recovery artifacts remain
retained for final acceptance. Worker rollout and the independent cloud restore
are verified; the progress record tracks the supplementary home backup and
remaining observations before the owner's merge review.

## Branch and release policy

- Continue implementation on `v3-rewrite` in incremental, reviewable commits.
  Merge directly into `develop` only after verification and the owner's success
  review. Do not rebase published implementation commits or force-push releases.
- `v2.5-compatible-final` identifies the last supported compatible source.
  [Its release manifest](docs/releases/v2.5-compatible-final.json) pins the exact
  existing images. Never rebuild or move these final and dated release tags.
- Publish development Stash images to `native-preview` and revision tags. The
  wrapper explicitly selects a full source-image digest and publishes separate
  native preview variants. Never select the newest source image implicitly.
- Production delivery must use images built and published by the repository's
  GitHub Actions workflows, then deployed from GHCR by verified digest. The owner
  explicitly rejected continued local-image deployments on October 9. Do not use
  another local build or republish one as a substitute when Actions is blocked;
  report the publication blocker and retain the running service until the
  required registry release is available.
- Keep live production pinned during development. Exercise migrations and
  imports against copies before opening the native database for production
  writes. Preserve the original database, configuration, catalogs, and operating
  state at a common backup boundary.

## Upstream changes

Import useful upstream fixes selectively with their source revision and native
regression tests. Routine rebasing onto upstream and minimizing merge conflicts
are no longer architecture requirements. Preserve copyright/license attribution.
The frozen compatible release is a migration fallback, not an indefinitely
maintained parallel product.

StashDB/stash-box compatibility remains a network protocol requirement. It does
not require matching upstream's database schema, GraphQL server, UI, or plugin
format. Preserve endpoint-qualified remote IDs and fingerprint algorithms.

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
write projections. Existing bridge code may remain temporarily while its
consumers are converted, but must be removed before the transition is complete.

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

## API, UI, and plugin contracts

V3 is the application; v2.5 UI/API compatibility is no longer a requirement.
Version the supported contract, inventory actual callers, and regenerate them
together. Remove old adapters only after converting their consumers, including
host/n8n jobs, installed plugins, offline/share entry points, and backup tools.
Preserve intentional public media/share URLs and standalone/offline behavior.

Plugins must declare [`apiVersion: 3`](docs/plugin-manifests.md). Retain typed settings,
schema-constrained mappings, UI contributions on desktop/mobile, capability
negotiation, jq evaluation, and notifications after successful changes. Hooks
cannot veto committed changes. Native archive consistency must not depend on
catalogMetadata or any other plugin receiving a notification.

Use shared domain services for both GraphQL and native ingestion. Producers send
idempotent events with durable receipts and scoped Stash API tokens; they do not write
the library database. Keep after-success behavior consistent across API edits,
background jobs, and imported changes. Never expose internal plugin settings in
mapping data without an explicit use case.

## Validation and documentation

Never edit generated Go/TypeScript bindings by hand. `make generate` regenerates
Go and v3 bindings; `make ui` builds the sole embedded application and login
locales. Build real embedded assets before full Go validation. The retired UI
and its toolchain remain available only at the frozen compatible tag.

Test changed invariants and migration outcomes with real SQLite fixtures. Cover
restarts, retries, integrity, unknown inputs, lossless promotion, API callers,
and retained external protocols. Replace v2.5 round-trip/build checks with
native migration and contract tests as their runtime paths are removed. Keep
security and dependency checks for the remaining application.

Use the [cutover runbook](docs/native-archive-transition-plan.md#production-cutover-runbook),
full-copy migration rehearsal, semantic reconciliation, and confirmed coordinated
backup publication before deploying the new writer. The owner's October 9 rollout
decision permits the separate cloud restore drill to finish after launch. Keep its
result independent of publication, retain the recovery copies until it passes,
and require verified restore coverage before declaring the transition complete.
Update architecture, contributor guidance,
build/CI, packaging, and deployment instructions with their implementation
changes. Do not describe planned work as already deployed or declare completion
while the plan's acceptance criteria remain unmet.
