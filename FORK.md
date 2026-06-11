# Fork maintenance

This repository is a **tracking fork** of [stashapp/stash](https://github.com/stashapp/stash),
not a hard fork. Upstream changes are absorbed by merging, and fork features are
designed to keep that cheap. This document is the playbook for syncs and the
rules that keep the merge surface small. It is written for both humans and
LLM agents performing a sync.

## Strategy

- Sync via `git merge upstream/develop` (per upstream release, or as needed) —
  **not** rebase. Merging resolves each conflict once and keeps history intact.
- Enable `git rerere` so re-encountered conflicts auto-resolve.
- Mainline v2.5 API clients must keep working. Fork features add compatibility
  layers at the resolver level rather than changing existing API semantics
  (see "Saved filters" in CLAUDE.md for the pattern).
- Planned rollout gating: the v3 UI and supporting features will sit behind a
  launch flag (`--enable-v3-ui`), later inverted to `--enable-v2-ui` when v3
  becomes the default, then removed when v2.5 is dropped.

## Design rules that keep merges cheap

1. **New code goes in new files/directories.** `ui/v3/` is wholly fork-owned;
   Go-side fork logic lives in dedicated files (e.g. `pkg/models/filter_ast*.go`).
2. **Schema changes are additive only.** Never change the type, nullability, or
   semantics of an existing GraphQL field or argument.
3. **Touch shared upstream files minimally** — a one-line `case` branch or call
   into a fork-owned file, not inline logic.
4. **Never edit generated files by hand** (`internal/api/generated_*.go` is
   gitignored; `ui/*/src/core/generated-graphql.ts` is checked in). They are
   products of `make generate`.

## Sync playbook

1. `git remote add upstream https://github.com/stashapp/stash.git` (once),
   then `git fetch upstream && git merge upstream/develop`.
2. **Generated files:** never hand-merge conflicts in them. Take either side
   (`git checkout --theirs <file>`), finish the merge, then run
   `make generate` and commit the regenerated output. This eliminates most
   apparent conflicts.
3. **Database migrations** — the most fragile area, two known hazards:
   - Upstream migrations remain in `pkg/sqlite/migrations/*.sql` and own only
     upstream's `schema_migrations` version. Fork DB changes are registered as
     Go migrations with `sqlite.RegisterForkMigration` or
     `sqlite.RegisterLegacyForkMigration` and are recorded in the separate
     `fork_schema_migrations` table. Do not add new fork changes to upstream's
     numeric migration sequence.
   - Upstream rewrites tables with the SQLite rebuild pattern
     (`CREATE TABLE new` + `INSERT SELECT <columns>` + `DROP` + rename), which
     **silently drops fork-added columns**. On every sync, grep the new
     upstream migrations for tables the fork has extended.
     Fork-extended tables so far: `performer_aliases` (`ignore_auto_tag`
     column, fork migration 1), `saved_filters` (`filter_ast` column, fork
     migration 2), and `video_files`/`image_files` (`bit_depth`,
     `color_range`, `color_space`, `color_transfer`, `color_primaries`, fork
     migration 3).
4. **Expected conflict zones:** `pkg/ffmpeg` (fork's HLS/segmented-streaming
   rework overlaps upstream transcode work) and occasionally
   `internal/api/resolver_*.go`. Resolve in favour of keeping fork behaviour;
   consult CLAUDE.md sections for the intent behind fork code before choosing
   sides.
5. **Validate after every sync:** `make generate && make validate`, plus the
   saved-filter integration tests
   (`go test -tags integration ./pkg/sqlite/... -run SavedFilter`).
6. `ui/v2.5/` is read-only fork-side: upstream changes merge in freely; fork
   changes to it are not allowed (all active UI development is `ui/v3/`).

## Fork-owned surfaces (update when extending)

| Area | What | Upstream collision risk |
|---|---|---|
| `ui/v3/` | entire v3 UI | none (new directory) |
| `ui/ui_v3.go` | v3 embedded UI selector; keep `ui/ui.go` upstream-shaped for v2.5 | low |
| `graphql/schema/` | `*_filter_ast` args, `FilterASTInput`, `SavedFilter.filter_ast` | low (additive) |
| `pkg/models/filter_ast*.go` | AST model + v2.5 compat layer | none (new files) |
| `pkg/sqlite/fork_migrate.go` + `pkg/sqlite/migrations/fork_*.go` | fork migration track | table-rebuild hazards |
| `saved_filters.filter_ast` column | canonical saved-filter criteria | table-rebuild hazard |
| `performer_aliases.ignore_auto_tag` column | alias-level auto-tag opt-out | table-rebuild hazard |
| `video_files` / `image_files` color metadata columns | ffprobe bit depth and colour tags for HDR/depth display and filters | table-rebuild hazard |
| `pkg/ffmpeg` HLS changes | segmented streaming, PTS normalization | **high** |
