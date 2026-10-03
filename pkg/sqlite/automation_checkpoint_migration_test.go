package sqlite_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestAutomationCheckpointMigrationPreservesLibraryAndRejectsCollisions(t *testing.T) {
	config.InitializeEmpty()
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema59.sqlite")
			buildLegacyDatabase(t, path, 86, true)
			db := sqlite.NewDatabase()
			defer db.Close()
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(path), &needed))
			m, err := sqlite.NewMigrator(db)
			require.NoError(t, err)
			for v := m.CurrentSchemaVersion(); v < sqlite.NativeSchemaBaseline+59; v = m.CurrentSchemaVersion() {
				require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(v)))
			}
			m.Close()
			raw := openRawDB(t, path)
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"performers", "performer_names", "scenes", "images", "archive_entities", "source_posts", "source_captures", "archive_jobs", "enrichment_job_targets", "enrichment_completions", "enrichment_targets", "enrichment_target_history", "automation_snapshots", "automation_snapshot_records", "automation_enrichment_records", "automation_enrichment_imports"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE automation_checkpoint_bodies(retained TEXT); INSERT INTO automation_checkpoint_bodies VALUES('Unrelated retained data')")
				require.NoError(t, err)
				require.Error(t, db.RunAllMigrations())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name IN ('automation_checkpoint_imports','automation_enrichment_staged_input')"))
				var retained string
				require.NoError(t, raw.QueryRow("SELECT retained FROM automation_checkpoint_bodies").Scan(&retained))
				require.Equal(t, "Unrelated retained data", retained)
			} else {
				require.NoError(t, db.RunAllMigrations())
				require.NoError(t, db.ReInitialise())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000060"))
				for _, table := range []string{"automation_checkpoint_imports", "automation_checkpoint_records", "automation_checkpoint_bodies"} {
					require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
				}
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
