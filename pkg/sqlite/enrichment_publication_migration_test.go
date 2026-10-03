package sqlite_test

import (
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentPublicationMigrationPreservesStagedWorkAndRejectsUnverifiableSuccess(t *testing.T) {
	for _, mode := range []string{"upgrade", "collision", "unsupported_success"} {
		t.Run(mode, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
			require.NoError(t, err)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			// The schema-52 addition changes no existing definition: remove only
			// its new objects to restore the exact previous layout with live data.
			_, err = raw.Exec(`DROP TRIGGER enrichment_checkpoint_published_delete;
 DROP TABLE enrichment_checkpoint_releases;
 DELETE FROM native_migration_history WHERE version=1000053;
 DROP TRIGGER enrichment_job_success;
 DROP TABLE enrichment_published_records; DROP TABLE enrichment_publications;
 DELETE FROM native_migration_history WHERE version=1000052;
 UPDATE schema_migrations SET version=1000051;`)
			require.NoError(t, err)
			if mode == "unsupported_success" {
				_, err = raw.Exec(`UPDATE archive_job_attempts SET outcome='succeeded',ended_at_ms=started_at_ms;
 UPDATE archive_jobs SET state='succeeded',revision=revision+1,owner_uuid=NULL,lease_until_ms=NULL;`)
				require.NoError(t, err)
			}
			before := map[string][][]any{}
			for _, table := range []string{"archive_jobs", "archive_job_attempts", "archive_job_submissions", "enrichment_targets", "enrichment_target_history", "enrichment_job_targets", "enrichment_job_attempts", "enrichment_checkpoint_receipts", "enrichment_checkpoint_records", "enrichment_checkpoints", "enrichment_checkpoint_usage"} {
				before[table] = albumJobRows(t, raw, table)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(path), &needed))
			if mode == "collision" {
				_, err = raw.Exec("CREATE TABLE enrichment_publications(retained TEXT); INSERT INTO enrichment_publications VALUES('Unrelated original data')")
				require.NoError(t, err)
			}
			err = f.db.RunAllMigrations()
			if mode == "upgrade" {
				require.NoError(t, err)
				require.NoError(t, f.db.ReInitialise())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_publications"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_published_records"))
			} else {
				require.Error(t, err)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='enrichment_job_success'"))
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
				if mode == "collision" {
					var original string
					require.NoError(t, raw.QueryRow("SELECT retained FROM enrichment_publications").Scan(&original))
					require.Equal(t, "Unrelated original data", original)
				}
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
