# Creating a migration

Native migrations use the primary sequence beginning at 1000000. Historical
primary migrations 1–86 and the old fork migration functions are one-time import
code. Do not add new fork migrations or runtime reconcilers. Read
[native schema promotion](../../../docs/native-schema.md) and
[FORK.md](../../../FORK.md) before changing this boundary.

1. Create a migration with the format `NN_description.up.sql`, where `NN` is the
   next native version. The migration driver runs its SQL in one transaction.

2. Update `pkg/sqlite/database.go` to update the `appSchemaVersion` value to the new migration number.

Test complete data/constraint outcomes and dirty, interrupted, unknown, and
unsupported inputs. Generated databases cannot be relabelled as old fixtures:
construct the real historical schema before applying the migration under test.
Config changes need a separate durable checkpoint; SQLite commit is not atomic
with replacing a YAML file. A custom migration must report commit failures.
