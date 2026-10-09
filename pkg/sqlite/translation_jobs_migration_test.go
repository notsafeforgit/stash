package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestTranslationJobsMigrationPreservesWorkAndRollsBackCollision(t *testing.T) {
	config.InitializeEmpty()
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema44.sqlite")
			buildLegacyDatabase(t, path, 86, true)
			db := sqlite.NewDatabase()
			defer db.Close()
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(path), &needed))
			m, err := sqlite.NewMigrator(db)
			require.NoError(t, err)
			for v := m.CurrentSchemaVersion(); v < sqlite.NativeSchemaBaseline+44; v = m.CurrentSchemaVersion() {
				require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(v)))
			}
			m.Close()
			raw := openRawDB(t, path)
			defer raw.Close()
			for _, kind := range []string{models.ArchiveJobVerifyMedia, models.ArchiveJobBackfillAlbum} {
				for _, state := range []string{"queued", "running", "succeeded", "failed", "cancelled"} {
					id, owner := uuid.NewString(), uuid.NewString()
					_, err = raw.Exec(`INSERT INTO archive_jobs(uuid,kind,work_key,resource_key,arguments,priority,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,?,?,?, '{"retained":true}',25,3,1,1,1)`, id, kind, ingest.Digest([]byte(id)), ingest.Digest([]byte(owner)))
					require.NoError(t, err)
					_, err = raw.Exec("INSERT INTO archive_job_submissions VALUES(?,?,?,1)", uuid.NewString(), strings.Repeat("a", 64), id)
					require.NoError(t, err)
					if state != "queued" {
						_, err = raw.Exec("UPDATE archive_jobs SET state='running',revision=2,fence=1,owner_uuid=?,lease_until_ms=9999999999999,updated_at_ms=2 WHERE uuid=?", owner, id)
						require.NoError(t, err)
						_, err = raw.Exec("INSERT INTO archive_job_attempts(job_uuid,fence,owner_uuid,started_at_ms) VALUES(?,1,?,2)", id, owner)
						require.NoError(t, err)
						if state != "running" {
							_, err = raw.Exec("UPDATE archive_job_attempts SET outcome=?,ended_at_ms=3,result='{\"retained\":true}' WHERE job_uuid=?", state, id)
							require.NoError(t, err)
							_, err = raw.Exec("UPDATE archive_jobs SET state=?,revision=3,owner_uuid=NULL,lease_until_ms=NULL,updated_at_ms=3,progress='{\"committed\":true}',result='{\"retained\":true}' WHERE uuid=?", state, id)
							require.NoError(t, err)
						}
					}
				}
			}
			before := map[string][][]any{}
			for _, table := range []string{"archive_jobs", "archive_job_submissions", "archive_job_attempts", "translation_requests", "translation_cache", "translation_targets", "translation_target_history"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE translation_job_targets(retained TEXT); INSERT INTO translation_job_targets VALUES('Unrelated preserved input')")
				require.NoError(t, err)
				require.Error(t, db.RunAllMigrations())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
				var definition string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='archive_jobs'").Scan(&definition))
				require.NotContains(t, definition, "text.translate", "job-table recreation rolls back with the failed migration")
				var retained string
				require.NoError(t, raw.QueryRow("SELECT retained FROM translation_job_targets").Scan(&retained))
				require.Equal(t, "Unrelated preserved input", retained)
			} else {
				require.NoError(t, db.RunAllMigrations())
				require.NoError(t, db.ReInitialise())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000045"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_job_targets"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}

func TestTranslationJobsReadyQueriesUseTargetAndActiveRequestIndexes(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	query := `SELECT t.* FROM translation_targets t INDEXED BY translation_targets_request_ready
 WHERE t.request_uuid=? AND t.state='pending' AND t.not_before<=?
 AND NOT EXISTS(SELECT 1 FROM translation_job_targets b WHERE b.target_uuid=t.uuid AND b.target_revision=t.revision)
 AND NOT EXISTS(SELECT 1 FROM archive_jobs j INDEXED BY archive_jobs_translation_request
 WHERE j.kind='text.translate' AND j.state IN ('queued','running') AND json_extract(j.arguments,'$.request_uuid')=+t.request_uuid)
 ORDER BY t.priority DESC,t.not_before,t.uuid LIMIT 50`
	rows, err := raw.Query("EXPLAIN QUERY PLAN "+query, uuid.NewString(), time.Now().UTC())
	require.NoError(t, err)
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rows.Scan(&a, &b, &c, &detail))
		plan += detail + "\n"
	}
	require.NoError(t, rows.Err())
	require.Contains(t, plan, "translation_targets_request_ready")
	require.Contains(t, plan, "sqlite_autoindex_translation_job_targets_1")
	require.Contains(t, plan, "archive_jobs_translation_request")
	require.NotContains(t, plan, "SCAN ")
	require.NotContains(t, plan, "TEMP B-TREE")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, limit := range []int{0, 51} {
			_, err := repo.TranslationWork.ReadyTargets(ctx, "", time.Now().UTC(), limit)
			require.ErrorIs(t, err, models.ErrTranslationWorkInvalid)
		}
		return nil
	}))
}
