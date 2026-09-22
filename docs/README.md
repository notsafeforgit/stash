# Project documentation

This branch is the `v3-rewrite` tracking fork of Stash. Active UI development is
in `ui/v3/`; v2.5 remains the mainline client and storage compatibility baseline.
v3 route paths may differ, but existing v2.5 clients must keep working.

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
| [Fork maintenance](../FORK.md) | Upstream syncs, additive API changes, migrations, and rollback compatibility |
| [Repository guidance](../CLAUDE.md) and [v3 contributor guidance](../ui/v3/AGENTS.md) | Coding conventions and backend/player constraints |
| [Plugin host](../ui/v3/docs/plugin-host.md) | Current v3 UI plugin API, startup lifecycle, and failure handling |
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

## Upstream reference and contribution policies

[Building from Source](DEVELOPMENT.md) retains upstream platform setup and v2.5
build instructions. Use the current development and architecture guides above
for this branch.

[Contributing](CONTRIBUTING.md) and [AI policy](AI_POLICY.md) describe upstream
contribution requirements. The [root README](../README.md) retains upstream
installation and community links; those downloads are mainline releases.

## Future plans and historical material

- The [TV implementation plan](../ui/v3/docs/tv-mode-plan.md) records the
  accepted scope and performance constraints behind the implemented feature.
- The [2026-09-08 v3 quality audit](../ui/v3/docs/quality-audit-2026-09-08.md)
  records the findings, completed foundation improvements, validation, and
  offline deployment isolation with migration and recovery.
- [Retiring v2.5 compatibility](v3-schema-promotion.md) is a conditional,
  one-way transition plan. It is **not active migration policy** and does not
  authorize dropping compatibility during the rewrite.
- The [original rewrite plan](../ui/v3/docs/archive/rewrite-plan.md) and
  [early evaluation](../ui/v3/docs/archive/rewrite-plan-evaluation.md) are
  historical snapshots, not current checklists.

When a feature changes a documented contract, update its current guide in the
same change. Keep implementation details in the owning guide, link to them from
the index and contributor guidance, and label proposals and archives explicitly.
