# Native Stash

A standalone, self-hosted media library with a Go backend and React/TypeScript
application for organizing and playing local media. Derived from
[Stash](https://github.com/stashapp/stash), this project owns its database schema,
APIs, UI, plugins and releases.

[![Validation and publication](https://github.com/notsafeforgit/stash/actions/workflows/ghcr-publish.yml/badge.svg?branch=develop)](https://github.com/notsafeforgit/stash/actions/workflows/ghcr-publish.yml)
[![Browser tests](https://github.com/notsafeforgit/stash/actions/workflows/v3-browser-tests.yml/badge.svg?branch=develop)](https://github.com/notsafeforgit/stash/actions/workflows/v3-browser-tests.yml)

## Install

Use the [Docker Compose example](docker/production/README.md), or follow the
[rootless Podman and GPU setup](docs/container.md). The published Linux amd64
image is:

```text
ghcr.io/notsafeforgit/stash:develop
```

It includes the media tools and hardware runtime. For production, pin a verified
image digest using the [deployment guide](docs/v3-deployment.md).

Mount configuration at `/config` and your media at a path such as `/data`.
Open `http://localhost:9999`, finish setup, add folders in **Settings → Library**,
and scan them. Keep a backup of the database, configuration and artwork.

Fresh installations initialize directly into the native schema. Existing native
libraries apply later migrations normally. To import a supported upstream or
pre-native library, read the [legacy upgrade instructions](docs/container.md#upgrade-from-the-compatible-release)
and preserve a separate pre-upgrade copy. Old binaries cannot open the native
schema; these are one-time imports, not shared database formats.

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

## Develop and contribute

Development and pull requests target `develop`. Start with the
[project policy](PROJECT.md), [documentation index](docs/README.md),
[system architecture](docs/ARCHITECTURE.md) and
[development guide](ui/v3/docs/development.md).

```bash
make pre-ui
make validate
```

`make validate` generates bindings, validates and builds the application, and
runs Go lint, integration tests and CI checks. Downloader packages are not a
core build dependency. Run the pinned external-client contract suite when
changing its contracts, and browser suites for affected interactions.

Selected major features may be ported from upstream with source attribution and
native regression tests. There is no routine upstream synchronization or
UI/API/plugin compatibility target. StashDB/stash-box remains a supported
external protocol.

Report issues in [this repository](https://github.com/notsafeforgit/stash/issues)
and follow the [contribution guidance](docs/CONTRIBUTING.md).

## Provenance and historical upgrades

This project derives from [stashapp/stash](https://github.com/stashapp/stash)
and retains its [AGPL-3.0 license](LICENSE) and attribution. Upstream's releases,
documentation and community describe a separate application.

The immutable [final pre-native release](docs/releases/v2.5-compatible-final.md)
is retained as historical source and an upgrade recovery boundary. It is not an
active release line. Its readers are isolated as
[legacy-upgrade-only code](pkg/sqlite/migrations/README.md).
