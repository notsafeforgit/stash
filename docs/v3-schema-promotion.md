# Retiring v2.5 Compatibility

This document describes the one-way transition from the rollback-compatible
v3 bridge to a v3-only schema. Do not start this transition while any database
must still open in v2.5. The promotion release must advance the primary SQLite
schema version so an older binary refuses the database instead of silently
writing obsolete representations.

## Current Compatibility Contract

The bridge supports switching versions only when both binaries expect the same
upstream schema version. The database and the configuration file must travel
together because default filters live in UI configuration.

| Surface | v3 canonical data | v2.5-compatible data | Roll-forward behavior |
|---|---|---|---|
| Saved filters | `fork_saved_filter_state.filter_ast` | `saved_filters.object_filter` | Flat edits import automatically; complex conflicts remain pending. |
| Default filters | `forkDefaultFilterState.<view>.filter_ast` | `defaultFilters.<view>.object_filter` | Startup imports safe edits; the v3 UI resolves complex conflicts. |
| Performer auto-tag names | `fork_performer_autotag_ignored_names` keyed by name text | No per-name equivalent | v2.5 ignores the policy; reconciliation removes entries for names no longer present. |
| Video/image probe metadata | `fork_video_file_metadata` and `fork_image_file_metadata` | No equivalent | Source fingerprints invalidate metadata after v2.5 changes a file row. |

The global performer `ignore_auto_tag` flag remains an upstream field. UI-only
changes and API compatibility shims such as performer merge behavior need no
database schema promotion.

This contract preserves data, but it cannot make a complex AST and a flat
v2.5 filter equivalent. Pending conflicts require an explicit choice; they
must not be silently discarded during promotion.

### Performer merge API bridge

`performerMerge` remains one additive GraphQL mutation for v2.5, v3, plugins,
and external clients. Its current behavior is deliberately selected by the
optional `PerformerMergeInput.require_resolved_values` field:

| Request | Contract |
|---|---|
| Field omitted or `false` | Preserve the legacy destination-biased merge contract. `values` remains optional and omitted fields are not treated as explicit decisions. This is the v2.5-compatible path. |
| Field set to `true` | Require every differing, source-owned metadata field to be represented in `values`. Validation reads the current performers inside the merge transaction and aborts before any write when a field is unresolved. The v3 UI uses this path and projects even "keep destination" choices so field presence records an explicit decision. |

Both paths enforce canonical-name retention on the server: every participant's
primary name becomes an alias unless it is the final primary name. Name matching
is case-insensitive, duplicates are removed, and the per-name auto-tag policy is
carried with the name. This is a data invariant, not a v3 UI convenience, and
must remain after the compatibility bridge is removed.

The safe path treats an explicitly supplied value, including an empty value, as
an intentional resolution. Collection choices are projected as final sets by
v3. Performer relationships are transferred by the store; an explicit tag set
is applied after that transfer so the persisted result matches the preview.
Portraits support the normalized `image_input` form as well as participant image
URLs. The deprecated `alias_list` input remains accepted, but because it cannot
express per-alias policy, existing stored policies take precedence over its
synthetic defaults.

This bridge prevents accidental loss; it cannot preserve two incompatible
values in a singular field. Selecting one value or explicitly clearing the
field is an acknowledged discard. A caller that requires a strictly lossless
merge must decline any plan containing such a choice.

#### V3-only API cleanup

Retiring the v2.5 UI alone is not enough to remove this bridge. First complete a
normal GraphQL deprecation window for plugins and external clients. Then:

1. Replace client-computed field-presence resolution with a server-generated
   merge plan (or preview query) that identifies conflicts, legal combine
   operations, and the exact projected performer.
2. Include a performer revision or opaque plan token and reject execution when
   any participant changed after preview. Keep validation and execution in one
   transaction.
3. Make loss-aware execution the only contract. Remove
   `require_resolved_values`, the v3 `projectKeepValues` opt-in, and the legacy
   destination-biased branch in a breaking API release.
4. Remove `alias_list`, legacy policy inference, and the deprecated `image`
   input after their own API deprecation windows; accept only policy-bearing
   aliases and normalized `image_input`.
5. Move merge planning and execution from the resolver into a performer domain
   service. Keep the resolver as schema translation and keep canonical-name and
   per-name-policy retention as service-level invariants.
6. Remove the v3 dialog's defensive name folding once all callers rely on the
   server invariant. Retain regression tests at the API/service boundary.

The intended final shape is therefore preview/plan followed by guarded apply,
not a silent change to the default behavior of the existing mutation.

## Promotion Gate

Complete these steps before writing the final migration:

1. Ship a last bridge release and run it once against every database and its
   matching config file.
2. Back up both files. Test restoring the pair, not only the SQLite database.
3. Resolve every default-filter conflict in the v3 UI.
4. Audit saved-filter conflicts with:

   ```sql
   SELECT saved_filter_id
   FROM fork_saved_filter_state
   WHERE pending_legacy_object_filter IS NOT NULL;
   ```

5. Add a saved-filter conflict resolver or define an explicit per-row policy.
   The promotion migration should abort while the query above returns rows.
6. Stop all v2.5 instances and disable mixed-version deployments.
7. Sync the final upstream base, then reserve the next primary schema version.

If v3 is accepted upstream, use upstream's next migration number. If this
remains an independent fork, the fork owns the primary schema lineage from
this point. Do not reuse an upstream migration number accidentally.

## Recommended Final Schema

**Saved filters:** make `saved_filters.filter_ast` the canonical column. Copy
the sidecar AST into it, validate every AST, then remove `object_filter` after
the legacy GraphQL API deprecation window. `legacy_object_filter` and
`pending_legacy_object_filter` are bridge state and must not enter the final
schema.

**Default filters:** retain `defaultFilters.<view>` as UI configuration, but
keep only `filter_ast` as criteria. Copy the canonical value from
`forkDefaultFilterState`, then remove `object_filter` and the entire fork key.
No database table is required merely to make this data canonical.

**Performer names:** the low-risk promotion is to rename the current policy
table to `performer_autotag_ignored_names`. The preferred full-v3 model is a
`performer_names` table containing `performer_id`, `name`, primary status,
display order, and `ignore_auto_tag`. Backfill `performers.name` and
`performer_aliases`, enforce one primary name per performer, and expose the
existing GraphQL name/alias fields as views over that model during API
deprecation. This matches the rule that the primary name is a distinguished
name, not a different kind of value. Preserve policy by joining on the
case-insensitive name text during this migration; do not attach the setting to
the old primary/alias role.

**File metadata:** rename the sidecars to `video_file_metadata` and
`image_file_metadata` and keep them as one-to-one tables. They are derived,
source-fingerprinted subtype data, so a join is appropriate and is not merely
a compatibility workaround. Folding these columns into `video_files` and
`image_files` would increase migration conflicts without improving ownership.

A table is part of the primary v3 schema when a normal schema migration owns
it and the repositories treat it as canonical. It does not need to be folded
into an older upstream table or lose normalization.

## Promotion Migration

Implement the cutover as one primary migration plus any required Go pre/post
migration hooks:

1. Accept both inputs: a pure v2.5 database with no fork tables and a fully
   reconciled bridge database. Reuse the bridge conversion helpers rather than
   assuming fork migration 5 has already run.
2. Create the final semantic tables and columns, then copy canonical data.
3. Validate row counts, foreign keys, AST decoding, primary-name uniqueness,
   and the absence of pending conflicts before dropping anything.
4. Convert UI configuration with an idempotent, schema-versioned config hook.
   Write the config atomically and fail without deleting the bridge key if the
   write does not complete.
5. Update stores and filters to read only the final schema. Stop dual-writing
   legacy projections and stop registering fork reconcilers.
6. Drop the four `fork_*` data tables and `fork_schema_migrations` only after
   successful copy and validation.
7. Increment `appSchemaVersion` and add the normal migration file. This is the
   rollback boundary: v2.5 must report a schema mismatch after promotion.

Do not register the promotion only as another fork migration. An upstream-only
binary ignores `fork_schema_migrations`; without a primary schema bump it could
still open the database and create divergence after reconciliation is gone.

## Compatibility Removal Map

| Bridge code | V3-only action |
|---|---|
| `pkg/sqlite/migrations/fork_compatibility.go` | Move reusable import logic into the primary promotion migration; remove reconcilers and fork registration. |
| `pkg/sqlite/fork_migrate.go` | Remove after the minimum supported database version is the promoted schema. |
| `pkg/sqlite/saved_filter.go` sidecar join | Read/write `saved_filters.filter_ast` directly. |
| `pkg/models/filter_ast_compat.go` | Keep only explicit legacy import tooling, or remove after legacy imports are unsupported. |
| Saved-filter resolver compatibility paths | Deprecate `object_filter`, then remove its flattening and legacy write path in a breaking API release. |
| `internal/manager/default_filter_compat.go` | Replace with the one-time config promotion, then remove. |
| `ui/v3/src/hooks/default-filter.ts` bridge fields | Save only canonical AST; remove projection and version-conflict controls. |
| Performer and metadata `fork_*` constants/joins | Point to the promoted semantic tables; remove cleanup reconcilers. |
| `performerMerge` compatibility path and `internal/api/performer_merge_*.go` | Replace the field-presence shim with guarded server-side planning; retain canonical-name/policy preservation in the domain service. |
| v3 performer merge `projectKeepValues` and defensive name folding | Remove after the guarded API is the only supported merge contract. |
| v2.5 UI embedding and launch flags | Remove `ui/v2.5`, the fallback selector, and `--enable-v2-ui` after the rollback window closes. |

Retiring the v2.5 UI and retiring the legacy GraphQL API may be separate
releases. Keeping the API shim briefly is useful for plugins and external
clients, but after schema promotion it must derive compatibility output from
the v3 source of truth and must never become canonical again.

## Required Verification

Migration tests must cover a pristine v2.5 fixture, a fork-migration-5 fixture,
and a reconciled database containing all sidecar data. Compare semantic records
before and after promotion, run `PRAGMA foreign_key_check`, and verify that no
`fork_*` objects remain. Also test config promotion, old-binary schema refusal,
backup restoration, `make validate-fork`, and the full v3 UI build.

Keep the promotion migration for every supported upgrade path. It may be
squashed only before any public build can create the promoted schema.
