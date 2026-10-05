package sqlite_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeMetadataNameSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP INDEX metadata_studio_names; DROP INDEX metadata_studio_aliases;
 DROP INDEX metadata_tag_names; DROP INDEX metadata_tag_aliases; DROP INDEX metadata_group_names;
 DELETE FROM native_migration_history WHERE version=1000079;`)
	require.NoError(t, err)
}

func TestMetadataNameMigrationPreservesNativePoliciesAndUnknownIndexes(t *testing.T) {
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			f := newMetadataPolicyFixture(t)
			f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, Mappings: map[string]models.MetadataMapping{
				"performers": {Value: json.RawMessage(`["Shared name"]`), PerformerNames: true},
			}})
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removeMetadataNameSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000078,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"performers", "performer_names", "archive_entities", "metadata_policies", "metadata_policy_revisions", "native_migration_history"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE INDEX metadata_tag_aliases ON tags(name)")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			err = f.db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				var definition string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='metadata_tag_aliases'").Scan(&definition))
				require.Equal(t, "CREATE INDEX metadata_tag_aliases ON tags(name)", definition)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='metadata_studio_names'"), "preceding index creation must roll back")
			} else {
				require.NoError(t, err)
				require.NoError(t, f.db.ReInitialise())
				require.Equal(t, f.db.AppSchemaVersion(), f.db.Version())
				require.EqualValues(t, len(before["native_migration_history"])+int(f.db.AppSchemaVersion()-1000078), queryUint(t, raw, "SELECT count(*) FROM native_migration_history"))
				delete(before, "native_migration_history")
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}

func TestMetadataNameStartupRequiresTheCaseInsensitiveIndex(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("DROP INDEX metadata_tag_aliases; CREATE INDEX metadata_tag_aliases ON tag_aliases(alias,tag_id)")
	require.NoError(t, err)
	require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "invalid name index metadata_tag_aliases")
}
