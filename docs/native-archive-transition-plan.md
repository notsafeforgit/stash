# Native archive architecture and upgrades

Stash is an independent fork with a native SQLite schema, the v3 UI and UUID-based
application interfaces. The complete original installation cutover plan and its
production verification records are archived in the separate operations repository.
They are historical deployment evidence, not a checklist for every Stash user.

## Shared application responsibilities

Ordinary library folders, scanning, scenes, images, mixed galleries, performers,
metadata providers, playback, plugins and recoverable file operations are core.
[Source tracking](optional-integrations.md) is off by default; enabling it adds
source posts, accounts, associations, ingestion and review without installing a
scraper or cloud backup. StashDB remains a supported network protocol.

The [native schema guide](native-schema.md), [library identity contract](library-identities.md)
and [ingestion API](native-ingestion.md) describe current storage and interfaces.
Website interpretation belongs in source adapters; transaction and relationship
integrity belong in Stash. Producers retain durable outboxes and communicate over
HTTP. They never write the library database.

## Source-post albums and galleries

A post can associate ordered images and videos with a mixed gallery. Preserve
missing positions and duplicate-media associations without inventing unseen
attachments. Evidenced self-replies can share a thread gallery. See
[native source albums](native-source-albums.md) for ordering, title and membership
behavior. Folder and manual galleries remain useful independently of source tracking.

## Supported upgrades

The frozen [final compatible release](releases/v2.5-compatible-final.md) identifies
supported historical inputs. Upgrade code must be clearly marked and isolated;
it does not impose v2.5 runtime compatibility. Preserve UUIDs, metadata, relationships,
explicit decisions and interrupted-upgrade recovery. See
[migration maintenance](../pkg/sqlite/migrations/README.md) and [fork policy](../FORK.md).

Before a production schema upgrade, rehearse the changed migration on a copy and
retain a consistent recoverable backup. Publication, full restore drills and
schema migration are separate operations. A normal native application update does
not repeat a completed catalog import or initial cutover.

## External deployment responsibilities

Gallery-dl, FileFlows, n8n, host ZFS orchestration, S3 and Google Drive publication
are separately installed automation. The reusable gallery-dl and portable archive
clients remain in this repository; personal policies and workflows have independent
releases in `stash-ops`. See [optional integrations](optional-integrations.md),
[container setup](container.md) and the [portable archive contract](native-archive-format.md).
