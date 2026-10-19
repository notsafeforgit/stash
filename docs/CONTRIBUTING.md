# Contributing to Native Stash

Native Stash is a standalone media library. Development and pull requests target
`develop`. Read the [project policy](../PROJECT.md),
[system architecture](ARCHITECTURE.md) and
[development guide](../ui/v3/docs/development.md) before making changes.

## Issues and pull requests

Report bugs and feature requests in
[this repository](https://github.com/notsafeforgit/stash/issues). Search existing
issues first and include enough detail to reproduce a bug. Use a test library
and redact credentials and private library data from logs and screenshots.

Keep each change focused and explain the problem, resulting behavior and relevant
validation. Link an existing issue when there is one. Discuss major changes to
project scope or public contracts with the owner before implementing them;
owner-requested work already has that scope approval. Include before/after
screenshots for visual changes and migration/recovery evidence for durable-state
changes. Do not weaken tests to make a change pass.

Contributors are responsible for understanding and reviewing submitted code,
including work produced with tools or agents. Follow the [AI usage policy](AI_POLICY.md).
Contributions are licensed under the project's [AGPL-3.0 license](../LICENSE).
Preserve attribution when porting selected upstream features; the
[feature import process](../PROJECT.md#selective-feature-imports) applies.

## Design boundaries

The application organizes videos, images and galleries with reviewed metadata
and optional source evidence. Purchased or manually scanned files need no
invented source post. Source publishers remain distinct from depicted performers.
Core services own identities, associations, durable work and archive integrity.
Plugins extend the application through its versioned contract.

Use the shared React application and shadcn/Base UI components for desktop and
mobile. Keep individual operations bounded to relevant records; long operations
need durable progress and safe retries. Convert actual API clients together
when changing a contract. Preserve intentional share/media URLs, offline use,
portable archives and the StashDB/stash-box protocol.

Gallery-dl and other external producers own website access and downloads. They
submit idempotent API events and retain unsent work locally. Website credentials
stay with the producer. Personal workflows, host backups and deployment records
belong in the operations repository.

Historical upstream and pre-native databases are supported upgrade inputs.
Preserve their data and interrupted-upgrade recovery without introducing runtime
compatibility sidecars, dual writers or upstream API adapters. The frozen
`v2.5-compatible-final` release is historical, not a parallel product.

## Validation and publication

`make validate` is the full local gate. It generates bindings, validates/builds
the embedded UI, then runs Go lint/integration tests while independent CI checks
overlap. Never edit generated Go/TypeScript bindings by hand. Run affected
browser suites and `make pre-clients test-clients` when changing external-client
contracts. Test durable changes with real SQLite fixtures, including interruption
and retry cases where relevant.

Publish incremental commits without rewriting published history. Pull requests
use the same validation workflow as the `develop` publisher. Production uses
GitHub Actions-built GHCR images by verified digest; follow the
[deployment runbook](v3-deployment.md). Report source push, publication and
running deployment separately. Routine releases do not repeat a completed
installation cutover, catalog import or full-library reconciliation.
