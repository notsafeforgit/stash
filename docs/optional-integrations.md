# Optional integrations

A fresh installation works as a local media library. Configure folders in
Settings → Library, scan files, edit metadata and use StashDB/metadata providers
without setting up gallery-dl, n8n, AWS credentials or cloud storage.

## Source tracking

Source tracking is **off by default**. Enable **Settings → Services → Source
tracking → Enable source tracking**, then restart Stash. The setting is
`source_tracking_enabled: true` in `config.yml`. A deployment can instead supply
`STASH_SOURCE_TRACKING_ENABLED=true`; this overrides the saved setting.

The setting controls source posts, account associations, source collections,
review/activity/import pages and source tabs on library entities. It also controls
the producer ingestion/admin APIs and internal ingestion, source maintenance,
album and notification workers. The translation worker additionally requires its
own `translation_worker_enabled` setting. Saved settings and the running capability
are reported separately, so the UI remains consistent until restart.

Enabling source tracking does not install gallery-dl, create subscriptions or
start host/n8n schedules. Set up the
[gallery-dl quickstart](https://github.com/notsafeforgit/stash-integrations/blob/main/integrations/gallery-dl/QUICKSTART.md) with
`stash-gallery setup` to provision a folder-scoped token and local worker
configuration. `stash-gallery download` registers requested sources and imports
through the existing ingestion API. Website credentials remain with
those workers. The switch is independent of ordinary metadata scraper/provider
support, including StashDB.

When source tracking is off, its APIs return HTTP 503 with
`source_tracking_disabled`, and source pages offer a link to service settings.
No ingestion event is acknowledged. Producers retain undelivered outbox entries
and can resume delivery after re-enabling. If turning off an existing installation
for an extended period, also stop its separately configured host/n8n schedules;
the Stash switch does not administer external processes.

Disabling preserves media, selected metadata, accounts, posts, associations and
queued work. It does not delete tables or perform a migration. Existing source
data remains included in whichever backup mechanism the owner uses. Core identity,
metadata provenance, library folders and crash-safe filesystem operations remain
available; disabling a UI feature must not invalidate existing relationships.

Existing installations using source tracking must explicitly enable it before
upgrading to this default. No account count or database contents implicitly turns
it on. Each deployment opts in through its own configuration.

## S3 backup

S3 backup is an independent, **optional host integration**. Installing or starting
Stash does not install a backup timer, read AWS credentials or upload to a bucket.
A separately installed backup coordinator owns its paths, credentials, destination
and schedule. Installation-specific publishers and production records live outside
this repository and are not included in the Stash application build or CI.

Stash's authenticated local checkpoint/export APIs remain available for backups
to other destinations. They do no S3 publication by themselves. Owners can use
local backups, another remote destination or the separate S3 publisher. Source
tracking and cloud backup do not depend on one another.

Temporary deletion recovery records remain core behavior, even with both
integrations unused. They coordinate SQLite with filesystem changes and are
removed after successful deletion; see [file deletion](file-deletion.md).

## External automation

FileFlows, phone-upload workflows, n8n schedules and host backup policies belong
to the deployment that uses them. Stash neither installs nor starts those services.
The application image provides the server and its media runtime; an automation
sidecar is an independently released API client.

Workers mount their own configuration, API tokens, persistent delivery state and
the media folders they need. They call the supported HTTP APIs rather than opening
the Stash database. Keep download/publication lock directories shared between
workers when coordinating captures. A FileFlows installation can run its encoder
separately and submit the finished file through the [local intake API](native-manual-intake.md).

Reusable Python packages live in [stash-integrations](https://github.com/notsafeforgit/stash-integrations):
gallery-dl ingestion, portable archives and library helpers. They have their own
releases and tests. An operations image such as `stash-ops` installs pinned client
packages; a third container is not required. API tokens, media mounts and durable
outboxes stay with the existing workers. FileFlows remains its own service.

Core Stash keeps the APIs, schema and domain services. Ordinary Go/TypeScript
validation has no downloader dependency. `make pre-clients test-clients` runs the
explicit compatibility suite against `integration-clients.json`; changes to the
API or its persistence contract also run that suite in CI. Other installations
can run the clients on a host or in a container, or install none of them.
