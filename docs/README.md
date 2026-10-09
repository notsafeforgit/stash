# Project documentation

This branch is the `v3-rewrite` independent fork of Stash. Active UI development
is in `ui/v3/`. The [native archive transition](native-archive-transition-plan.md)
retires v2.5 compatibility and brings catalogs into the application.
[Implementation progress](native-archive-progress.md) records verified work and
the production cutover status. The [final compatible release](releases/v2.5-compatible-final.md)
provides the frozen fallback for users who do not migrate.

## Current development guides

| Guide | Use it for |
| --- | --- |
| [System architecture](ARCHITECTURE.md) | Runtime map, browser entry points, backend services, storage, compatibility, and request flows |
| [v3 development](../ui/v3/docs/development.md) | Local setup, generation, builds, and validation order |
| [v3 dependencies](v3-dependencies.md) | Dependency versions, native security updates, and shadcn maintenance |
| [v3 frontend architecture](../ui/v3/docs/architecture.md) | Startup, module/state ownership, typed list sources, cache updates, and extension points |
| [Player architecture](../ui/v3/docs/player.md) | Media lifetime, source/seek policy, recovery, transcode sessions, and backend HLS boundaries |
| [Interaction and layout contracts](../ui/v3/docs/interactions.md) | Motion, dialogs, editing shells, mobile navigation, keyboard layout, and focus |
| [Feature development plan](../ui/v3/docs/plan.md) | Expectations for new feature work |
| [Fork maintenance](../FORK.md) | Independent development, upstream imports, native migrations, and release boundaries |
| [Native schema promotion](native-schema.md) | Implemented lineage, one-time historical imports, promoted tables, and remaining model conversions |
| [Native source identity](native-source-identity.md) | Captured account claims, service namespaces, and reviewed performer/account registry import |
| [Imported catalog association repair](catalog-association-repair.md) | Backfill historical publishers and unique performer links from retained account and author-folder evidence |
| [Native source collections](native-source-collections.md) | Collections, media-root bindings, metadata rules, draft previews and durable browser saves |
| [Native local-file intake](native-manual-intake.md) | Reviewed purchased-media admission, durable file verification, recovery and cancellation |
| [Native file deduplication](native-file-deduplication.md) | Verified redundant locations, preserved source evidence, journaled removal and host conversion boundary |
| [Metadata policy migration](native-metadata-policy-migration.md) | Retained plugin/folder settings, explicit conversions and resumable native policy imports |
| [Native source albums](native-source-albums.md) | Ordered post attachments, partial downloads, gallery membership, and source-evidence requirements |
| [Native ingestion](native-ingestion.md) | Scoped ingestion, receipts, verified files, source-run coordination, native metadata policies, and preview/apply contracts |
| [Native bulk updates](native-bulk-updates.md) | Completed versus queued edits, caller conversion, cache refresh and host helpers |
| [Native entity queries](native-queries.md) | Filter expressions, explicit IDs, full-selection totals and converted clients |
| [Portable native archives](native-archive-format.md) | Compressed database/artwork snapshots, explicit operating-state components, verified restores and offline inspection |
| [Host backup integration](../integrations/backup/README.md) | Staged native/media publication, host-owned S3 verification and restore tools; installed daily scripts remain unchanged |
| [Repository guidance](../CLAUDE.md) and [v3 contributor guidance](../ui/v3/AGENTS.md) | Coding conventions and backend/player constraints |
| [Plugin host](../ui/v3/docs/plugin-host.md) | Current v3 UI plugin API, startup lifecycle, and failure handling |
| [Versioned plugin manifests](plugin-manifests.md) | Independent v3 plugin contract, rejected packages, and API generations |
| [Backend plugin notifications](plugin-events.md) | Scene/file deletion, metadata and file edits, commit semantics, and event payloads |
| [Plugin settings and jq](plugin-settings.md) | Manifest settings definitions, validated updates, expression previews, mappings, and browser host APIs |
| [Theming](../ui/v3/docs/theming.md) | Runtime CSS, JavaScript, custom assets, and component selectors |
| [Offline downloads](../ui/v3/docs/offline.md) | Implemented download/storage/playback behavior and remaining limits |
| [Expiring shares](sharing.md) | Anonymous scoped media links, management, security boundaries and Caddy setup |
| [TV mode](../ui/v3/docs/tv-mode.md) | Scene and marker feeds, quality settings, controls, shared activity, and implementation boundaries |
| [Preview images](preview-images.md) | HDR AVIF, SDR fallbacks, generation requirements, and the v2.5 compatibility boundary |
| [Read performance](read-performance.md) | Lazy browsing dependencies, compatible SQLite indexes/search, and library-scale measurements |
| [Locales](../ui/v3/src/locales/README.md) | Translation files and message conventions |

## Operations

- [v3 deployment](v3-deployment.md): publish the Stash image, bake the matching
  `stash-s6` wrapper, and verify a local rootless Quadlet restart.
- [Video duration mismatch repair](video-duration-mismatch-repair.md): manual
  diagnosis and repair for affected media files.
- [Recoverable file deletion](file-deletion.md): staging, crash recovery,
  automatic journal cleanup and trash transfers.

## Upstream reference and contribution policies

[Building from Source](DEVELOPMENT.md) retains upstream platform setup and v2.5
build instructions. Use the current development and architecture guides above
for this branch.

[Contributing](CONTRIBUTING.md) and [AI policy](AI_POLICY.md) describe upstream
contribution requirements. The [root README](../README.md) retains upstream
installation and community links; those downloads are mainline releases.

## Future plans and historical material

- [Native archive and independent fork transition](native-archive-transition-plan.md)
  is the requested full implementation plan for retiring v2.5 compatibility,
  promoting fork storage, importing existing catalogs, and moving gallery-dl
  and n8n to native ingestion. It includes migration, backup, cutover, and
  verification requirements; the production transition has not been executed.
- The [TV implementation plan](../ui/v3/docs/tv-mode-plan.md) records the
  accepted scope and performance constraints behind the implemented feature.
- The [2026-09-08 v3 quality audit](../ui/v3/docs/quality-audit-2026-09-08.md)
  records the findings, completed foundation improvements, validation, and
  offline deployment isolation with migration and recovery.
- [Retiring v2.5 compatibility](v3-schema-promotion.md) is the earlier, narrower
  schema-promotion outline. The native archive transition plan extends it to
  all current sidecars, catalog data, producers, and operational state. Current
  runtime compatibility remains in place until the planned cutover.
- The [original rewrite plan](../ui/v3/docs/archive/rewrite-plan.md) and
  [early evaluation](../ui/v3/docs/archive/rewrite-plan-evaluation.md) are
  historical snapshots, not current checklists.

When a feature changes a documented contract, update its current guide in the
same change. Keep implementation details in the owning guide, link to them from
the index and contributor guidance, and label proposals and archives explicitly.
