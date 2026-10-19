# Creating a migration

Native migrations use the primary sequence beginning at 1000000. Historical
primary migrations 1–86 and the old fork migration functions are one-time import
code. Do not add new fork migrations or runtime reconcilers. Read
[native schema promotion](../../../docs/native-schema.md) and
[PROJECT.md](../../../PROJECT.md) before changing this boundary.

## Historical code maintenance

`LEGACY-UPGRADE-ONLY` marks code that interprets historical inputs, including
`../fork_migrate.go`, `fork_compatibility.go`, and
`1000001_saved_filter_ast.go`. The filter codecs live in
[`pkg/filtermigration`](../../filtermigration/doc.go); their boundary test
rejects new runtime importers. Default-filter YAML conversion and its durable
publication checkpoint live in `internal/manager/default_filter_migration.go`
and `internal/manager/config/ui_import.go`.

These readers support upstream databases and frozen pre-native fork formats.
Their historical semantics must not follow new native UI/API behavior. Keep
input-specific fixes and recovery fixes covered by historical fixtures; implement
new behavior in native code and new numbered migrations. Follow the
[legacy upgrade policy](../../../PROJECT.md#legacy-upgrade-code).

Fresh database creation uses the native baseline described below. Historical
readers remain necessary for supported upstream/frozen-fork upgrades and recovery
of an old migration driver's empty ledger. Preserve unfinished configuration
publication checkpoints for supported upgrades. Do not delete numbered migrations
just because the production instance has passed them.

## Completed follow-up: direct native database bootstrap

Completed on develop after the approved 2026-10-18 rewrite merge. New databases
install [`../bootstrap/1000121.sql`](../bootstrap/1000121.sql), then apply 1000122
and later native migrations normally. The bootstrap version is pinned separately
from `appSchemaVersion`; adding a native migration does not require regenerating
the baseline or changing existing databases. This is initialization maintenance,
not another production data migration.

The direct path requires no SQLite schema objects and zero schema, user and
application versions. Missing files, zero-length files and untouched SQLite files
qualify. Existing, dirty, incomplete, foreign and newer schemas retain pre-write
validation. An old driver's empty migration ledger resumes its historical chain;
it is never overwritten by the native baseline.

Initialization rechecks freshness under SQLite's immediate writer lock and commits
all tables, indexes, constraints, triggers, initial rows, lineage and the clean
version together. A failed or interrupted transaction leaves an empty database
that can retry. If interruption occurs after the baseline commits, reopening
reports the normal migration-needed state until the remaining native migrations
finish. Dirty later migrations retain their existing recovery requirements.

The baseline is generated from an empty historical schema using the actual SQL
and Go migration readers. `TestNativeBootstrapBaselineMatchesMigrations` checks
the exact stored DDL and every initial row, reconstructs the snapshot, and checks
integrity and foreign keys. Only installation-specific migration timestamps are
regenerated. Index and trigger creation order is preserved and checked separately,
including implicit indexes, because it can affect SQLite's query-plan choices and
trigger execution. Historical migration-history rows are retained for equivalence;
they do not indicate that the new database imported a library or configuration.
`TestNativeBootstrapCurrentSchemaMatchesMigrations` compares both paths again at
the current schema, including subsequent migrations. These tests run in the
existing develop SQLite CI shards and gate image publication.

To regenerate after an intentional baseline change, run:

```sh
make generate-native-bootstrap
```

Review the generated SQL diff; never edit it by hand. Advancing the pinned version
also requires updating the embedded filename/version and the equivalence fixture,
while retaining a real subsequent migration test. Tests cover fresh/repeated opens,
transaction failures, abrupt process exit and retry, unsafe inputs, historical
upgrades and unfinished database/configuration publication.

Retiring an old input format is a separate support decision. Preserve its
upgrade path until that decision is made; do not delete readers as part of
this bootstrap follow-up. Promotion preparation is isolated in
`../legacy_native_promotion.go`; its historical reconciliation semantics are
unchanged.

## New native migrations

1. Create a migration with the format `NN_description.up.sql`, where `NN` is the
   next native version. The migration driver runs its SQL in one transaction.

2. Update `pkg/sqlite/database.go` to update the `appSchemaVersion` value to the new migration number.

Test complete data/constraint outcomes and dirty, interrupted, unknown, and
unsupported inputs. Generated databases cannot be relabelled as old fixtures:
construct the real historical schema before applying the migration under test.
Config changes need a separate durable checkpoint; SQLite commit is not atomic
with replacing a YAML file. A custom migration must report commit failures.
