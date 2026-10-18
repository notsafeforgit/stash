# Creating a migration

Native migrations use the primary sequence beginning at 1000000. Historical
primary migrations 1–86 and the old fork migration functions are one-time import
code. Do not add new fork migrations or runtime reconcilers. Read
[native schema promotion](../../../docs/native-schema.md) and
[FORK.md](../../../FORK.md) before changing this boundary.

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
[legacy upgrade policy](../../../FORK.md#legacy-upgrade-code).

Fresh database creation still starts at the historical SQL sequence and runs
the fork preparation before native promotion. The old readers therefore also
see empty input during bootstrap. Removing them requires a native bootstrap
baseline as well as retiring the corresponding upgrade inputs. Preserve
unfinished configuration publication checkpoints for supported upgrades. Do not
delete numbered migrations just because the production instance has passed them.

## Follow-up: direct native database bootstrap

Planned after the approved 2026-10-18 rewrite merge: create fresh databases
directly from a versioned native schema baseline instead of replaying the
historical primary/fork migration chain. This is maintenance work, not another
production data migration or a prerequisite for using the native application.

Before switching the fresh-install path:

- Prove that its tables, constraints, indexes, triggers, defaults and lineage
  match a database created by the current path, and that subsequent native
  migrations apply normally.
- Cover interrupted initialization and reopen/retry behavior without accepting
  partial, foreign or newer schemas.
- Keep supported upstream and frozen-fork upgrade readers isolated and tested,
  including unfinished database/configuration publication. They remain needed
  for users importing existing libraries even after fresh installs stop using them.

Retiring an old input format is a separate support decision. Preserve its
upgrade path until that decision is made; do not delete readers as part of
this bootstrap follow-up.

## New native migrations

1. Create a migration with the format `NN_description.up.sql`, where `NN` is the
   next native version. The migration driver runs its SQL in one transaction.

2. Update `pkg/sqlite/database.go` to update the `appSchemaVersion` value to the new migration number.

Test complete data/constraint outcomes and dirty, interrupted, unknown, and
unsupported inputs. Generated databases cannot be relabelled as old fixtures:
construct the real historical schema before applying the migration under test.
Config changes need a separate durable checkpoint; SQLite commit is not atomic
with replacing a YAML file. A custom migration must report commit failures.
