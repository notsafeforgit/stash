# Native Stash

An independent fork of [Stash](https://github.com/stashapp/stash), with a Go
backend and React/TypeScript v3 application for organizing and playing a local
media library.

[![Native validation and publication](https://github.com/notsafeforgit/stash/actions/workflows/ghcr-publish.yml/badge.svg?branch=develop)](https://github.com/notsafeforgit/stash/actions/workflows/ghcr-publish.yml)
[![Browser tests](https://github.com/notsafeforgit/stash/actions/workflows/v3-browser-tests.yml/badge.svg?branch=develop)](https://github.com/notsafeforgit/stash/actions/workflows/v3-browser-tests.yml)

Development continues on `develop` following the reviewed native rewrite merge.
The native database and application APIs are independent of upstream; the
[final compatible release](docs/releases/v2.5-compatible-final.md) remains
available for installations that choose to stay behind.

## Install

Use this fork's [Docker Compose example](docker/production/README.md), or follow
the [rootless Podman and GPU setup](docs/container.md). The published Linux amd64
image is:

```text
ghcr.io/notsafeforgit/stash:develop
```

It includes the media tools and hardware runtime; no `stash-s6` wrapper is needed.
For a production installation, pin a verified image digest using the
[deployment guide](docs/v3-deployment.md).

Mount configuration at `/config` and your media at a path such as `/data`.
Open `http://localhost:9999`, finish setup, add folders in **Settings → Library**,
and scan them. Keep a backup of the database, configuration and artwork.

Existing compatible installations need an explicit database migration in addition
to any container path changes. Read [upgrading from the compatible release](docs/container.md#upgrade-from-the-compatible-release)
before using an existing database with a native image. Preserve a separate
pre-upgrade copy; upstream and compatible binaries cannot open the native schema.

## Local library features

- Scenes, images, mixed-media galleries, performers, studios, tags and markers.
- Saved filters, batch editing, TV mode, playback, offline downloads and expiring shares.
- StashDB/stash-box metadata providers and versioned v3 plugins.
- UUIDs across application interfaces and native database migrations.

[Source tracking](docs/optional-integrations.md) is off by default. A local
library and StashDB work without gallery-dl, n8n, FileFlows, AWS credentials or
cloud backups. The server does not start those external services.

For installations that enable source tracking, core Stash manages accounts,
posts, media associations, source albums and review. Reusable ingestion and
portable archive clients are separately released in
[stash-integrations](https://github.com/notsafeforgit/stash-integrations).
Personal automation belongs in the deployment's operations repository; an
operations sidecar can install those clients and communicate with Stash's API.
See the [gallery-dl quickstart](https://github.com/notsafeforgit/stash-integrations/blob/main/integrations/gallery-dl/QUICKSTART.md)
for optional setup.

## Compatibility choices

| Choice | What to use |
| --- | --- |
| Native fork | This repository's image, v3 UI, native API and `apiVersion: 3` plugins |
| Stay on the final compatible fork | The immutable tags and digests in the [compatible release manifest](docs/releases/v2.5-compatible-final.json) |
| Return to upstream | Test a compatible database/configuration copy with the appropriate upstream version **before** native promotion; see the [release guide](docs/releases/v2.5-compatible-final.md) |

StashDB compatibility is a network contract and remains supported. Upstream UI,
API and plugin compatibility is not required for native development. Useful
upstream fixes can be imported selectively under the [fork policy](FORK.md).

## Develop and contribute

Start with the [documentation index](docs/README.md),
[system architecture](docs/ARCHITECTURE.md) and
[v3 development guide](ui/v3/docs/development.md). The latter lists the required
Go, Node.js and pnpm versions and the build and validation commands.

Core development uses Go and TypeScript. Downloader packages are not required by
ordinary application validation. The explicit external-client contract suite
tests the pinned integrations when their API or persistence contracts change.

Report fork issues in [this repository](https://github.com/notsafeforgit/stash/issues).
Read the [contribution guidance](docs/CONTRIBUTING.md) and [fork policy](FORK.md)
before proposing changes.

## Upstream attribution

This project derives from [stashapp/stash](https://github.com/stashapp/stash)
and retains its [AGPL-3.0 license](LICENSE). Upstream's
[documentation](https://docs.stashapp.cc),
[releases](https://github.com/stashapp/stash/releases) and
[community](https://discourse.stashapp.cc) describe mainline Stash. Their binaries
and plugins are not substitutes for this fork's native releases.
