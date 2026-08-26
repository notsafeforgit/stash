# Fork maintenance

This repository is a **tracking fork** of [stashapp/stash](https://github.com/stashapp/stash),
not a hard fork. Upstream changes are absorbed by rebasing the fork branch, and
fork features are designed to keep that cheap. This document is the playbook
for syncs and the rules that keep the conflict surface small. It is written for
both humans and LLM agents performing a sync.

## Strategy

- Sync by rebasing `v3-rewrite` onto `stashapp/develop` (per upstream release,
  or as needed). This keeps the fork history linear and makes the upstream base
  explicit. Because the branch is published, fetch `origin/v3-rewrite` before
  rebasing and update it afterward with `git push --force-with-lease`, never an
  unconditional force push.
- Enable `git rerere` so re-encountered conflicts auto-resolve.
- Mainline v2.5 API clients must keep working. Fork features add compatibility
  layers at the resolver level rather than changing existing API semantics
  (see "Saved filters" in CLAUDE.md for the pattern).
- Planned rollout gating: the v3 UI and supporting features will sit behind a
  launch flag (`--enable-v3-ui`), later inverted to `--enable-v2-ui` when v3
  becomes the default, then removed when v2.5 is dropped.

## Design rules that keep rebases cheap

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

1. Add the source remote once with
   `git remote add stashapp https://github.com/stashapp/stash.git`. Before each
   sync, run `git fetch stashapp` and `git fetch origin v3-rewrite`,
   then `git -c rerere.enabled=true rebase stashapp/develop`.
2. **Generated files:** never hand-merge conflicts in them. Take either side
   to let the rebase proceed, then run `make generate` and commit the
   regenerated output. During a rebase, remember that `--ours` is the new
   upstream base plus commits already replayed, while `--theirs` is the fork
   commit currently being replayed. This eliminates most apparent conflicts.
3. **Database migrations** — keep upstream fallback viable:
   - Upstream migrations remain in `pkg/sqlite/migrations/*.sql` and own only
     upstream's `schema_migrations` version. Fork DB changes are registered as
     Go migrations with `sqlite.RegisterForkMigration` and are recorded in the
     separate `fork_schema_migrations` table. Do not add new fork changes to
     upstream's numeric migration sequence.
   - Never leave fork columns in upstream-owned tables. Store fork data in
     `fork_*` sidecar tables keyed to upstream rows with cascading foreign
     keys. v3 writes an upstream-compatible base representation and richer
     sidecar state in the same transaction.
   - Register idempotent compatibility passes with
     `sqlite.RegisterForkReconciler`. They run after migrations and whenever
     v3 opens a current database, importing upstream-only edits and
     invalidating derived data whose source fingerprint changed.
   - Migration 5 is the single consolidated pre-public fork migration. It
     imports the private development columns from historical migrations 1-4,
     drops those columns, and creates all current sidecars.
4. **Expected conflict zones:** `pkg/ffmpeg` (fork's HLS/segmented-streaming
   rework overlaps upstream transcode work) and occasionally
   `internal/api/resolver_*.go`. Resolve in favour of keeping fork behaviour;
   consult CLAUDE.md sections for the intent behind fork code before choosing
   sides.
5. **Validate after every sync and before pushing fork changes:** run
   `make validate-fork`. It regenerates the backend, validates v3, runs
   integration tests, and runs the CI-pinned linter without requiring a local
   install. For lint alone, use `make lint`, which executes
   `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4 run`.
   Keep that version synchronized with `.github/workflows/golangci-lint.yml`.
   During iteration, use `make test`, `make validate-ui-v3`, and focused
   integration tests such as
   `go test -tags integration ./pkg/sqlite/... -run SavedFilter`.
   For an upstream sync that changes v2.5, additionally run
   `make generate && make validate`; fork feature work must not edit v2.5.
6. `ui/v2.5/` is read-only fork-side: upstream changes rebase in freely; fork
   changes to it are not allowed (all active UI development is `ui/v3/`).
7. Review the rewritten range, verify that `stashapp/develop` is an ancestor of
   `v3-rewrite`, then publish with
   `git push --force-with-lease origin v3-rewrite`. A lease failure means the
   remote changed after it was fetched; fetch and inspect it rather than
   overriding it.

## Fork-owned surfaces (update when extending)

| Area | What | Upstream collision risk |
|---|---|---|
| `ui/v3/` | entire v3 UI | none (new directory) |
| `ui/ui_v3.go` | v3 embedded UI selector; keep `ui/ui.go` upstream-shaped for v2.5 | low |
| `graphql/schema/` | additive v3 API fields, including filter ASTs and loss-aware performer merge opt-in | low (additive) |
| `internal/api/performer_merge_*.go` | canonical-name retention and opt-in loss-aware performer merge validation | low (new files) |
| `pkg/models/filter_ast*.go` | AST model + v2.5 compat layer | none (new files) |
| `pkg/sqlite/fork_migrate.go` + `pkg/sqlite/migrations/fork_*.go` | consolidated fork migration and roll-forward reconcilers | low |
| `fork_performer_autotag_ignored_names` | case-insensitive auto-tag opt-outs keyed by performer and name text | none (fork-owned table) |
| `fork_saved_filter_state` | canonical filter AST plus upstream compatibility shadow | none (fork-owned table) |
| `fork_video_file_metadata` / `fork_image_file_metadata` | ffprobe metadata plus source fingerprints | none (fork-owned tables) |
| `forkDefaultFilterState` UI config key | canonical default-filter AST, legacy shadow, and pending conflict | none (ignored by v2.5) |
| `pkg/ffmpeg` HLS changes | segmented streaming, PTS normalization | **high** |

Migration 5 restores all upstream-owned tables to their upstream shape. An
upstream-only server at the same upstream schema version ignores the sidecars.
Rolling forward to v3 recreates missing sidecars and imports compatible
upstream changes; fork-only data remains available when its sidecar was kept.
Saved-filter edits are imported automatically only when the stored AST is
losslessly representable by v2.5. For complex ASTs, reconciliation preserves
the v3 canonical value and stores the conflicting v2.5 edit separately rather
than silently replacing either version. `defaultFilters` remains directly
usable by v2.5, while the v2.5-ignored `forkDefaultFilterState` config key
preserves the canonical default-filter AST and compatibility shadow. v3
reconciles these values at startup. Its default-filter controls replace or
clear both namespaces atomically, and surface a choice when a v2.5 edit
conflicts with a complex v3 default.

## Retiring v2.5 compatibility

The rollback bridge is intentionally removable, but retirement is a one-way
schema transition rather than deletion of the sidecars in place. Follow
[Retiring v2.5 Compatibility](docs/v3-schema-promotion.md) for the promotion
gate, final schema, removal map, and required migration tests. In particular,
the final release must promote v3 data through the primary migration track and
advance `appSchemaVersion`; another fork migration would not prevent an old
upstream binary from reopening and writing the database.
