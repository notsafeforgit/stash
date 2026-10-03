package sqlite_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentJobsMigrationPreservesEveryPriorJobKind(t *testing.T) {
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			config.InitializeEmpty()
			path := filepath.Join(t.TempDir(), "schema50.sqlite")
			buildLegacyDatabase(t, path, 86, true)
			db := sqlite.NewDatabase()
			defer db.Close()
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(path), &needed))
			m, err := sqlite.NewMigrator(db)
			require.NoError(t, err)
			for v := m.CurrentSchemaVersion(); v < sqlite.NativeSchemaBaseline+50; v = m.CurrentSchemaVersion() {
				require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(v)))
			}
			m.Close()
			raw := openRawDB(t, path)
			defer raw.Close()
			for _, kind := range []string{models.ArchiveJobVerifyMedia, models.ArchiveJobBackfillAlbum, models.ArchiveJobTranslateText} {
				for _, state := range []string{"queued", "running", "succeeded", "failed", "cancelled"} {
					id, owner := uuid.NewString(), uuid.NewString()
					input := models.ArchiveJobSubmission{Kind: kind, WorkKey: ingest.Digest([]byte(id)), ResourceKey: ingest.Digest([]byte(owner)),
						Arguments: []byte(`{"retained":true}`), RequestUUID: uuid.NewString(), MaxAttempts: 3}
					var targetID string
					if kind == models.ArchiveJobTranslateText {
						postID := uuid.NewString()
						_, err = raw.Exec("INSERT INTO source_posts(uuid) VALUES(?)", postID)
						require.NoError(t, err)
						_, err = raw.Exec("INSERT INTO source_post_identifiers(namespace,value,post_uuid) VALUES('native:reddit',?,?)", state, postID)
						require.NoError(t, err)
						request, err := archive.PrepareTranslationRequest(models.TranslationRequestInput{OriginalText: state, TargetLanguage: "en", Policy: models.TranslationBingTextV1})
						require.NoError(t, err)
						_, err = raw.Exec("INSERT INTO translation_requests(uuid,original_text,original_sha256,target_language,policy) VALUES(?,?,?,?,?)",
							request.UUID, request.OriginalText, request.OriginalSHA256, request.TargetLanguage, request.Policy)
						require.NoError(t, err)
						targetID, err = archive.TranslationTargetIdentity(models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: postID, Field: "caption", Origin: "capture"})
						require.NoError(t, err)
						stamp := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
						_, err = raw.Exec(`INSERT INTO translation_targets(uuid,request_uuid,post_uuid,field,origin,state,priority,not_before,created_at,updated_at)
 VALUES(?,?,?,'caption','capture','pending',25,?,?,?)`, targetID, request.UUID, postID, stamp, stamp, stamp)
						require.NoError(t, err)
						input, err = archive.PrepareTranslationJob(models.TranslationJobArguments{Version: 1, RequestUUID: request.UUID,
							Targets: []models.TranslationTargetRef{{TargetUUID: targetID, Revision: 1}}})
						require.NoError(t, err)
					}
					_, err = raw.Exec(`INSERT INTO archive_jobs(uuid,kind,work_key,resource_key,arguments,priority,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,?,?,?,?,25,?,1,1,1)`, id, kind, input.WorkKey, input.ResourceKey, string(input.Arguments), input.MaxAttempts)
					require.NoError(t, err)
					_, err = raw.Exec("INSERT INTO archive_job_submissions VALUES(?,?,?,1)", input.RequestUUID, strings.Repeat("a", 64), id)
					require.NoError(t, err)
					if targetID != "" {
						_, err = raw.Exec("INSERT INTO translation_job_targets VALUES(?,?,1)", id, targetID)
						require.NoError(t, err)
					}
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
			for _, table := range []string{"archive_jobs", "archive_job_submissions", "archive_job_attempts", "translation_requests", "translation_targets", "translation_target_history", "translation_job_targets"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE enrichment_job_targets(retained TEXT); INSERT INTO enrichment_job_targets VALUES('Original unrelated rows')")
				require.NoError(t, err)
				require.Error(t, db.RunAllMigrations())
				var definition, retained string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='archive_jobs'").Scan(&definition))
				require.NotContains(t, definition, "post.enrich")
				require.NoError(t, raw.QueryRow("SELECT retained FROM enrichment_job_targets").Scan(&retained))
				require.Equal(t, "Original unrelated rows", retained)
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
			} else {
				require.NoError(t, db.RunAllMigrations())
				require.NoError(t, db.ReInitialise())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000051"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_job_targets"))
				require.Zero(t, queryUint(t, raw, "SELECT byte_size FROM enrichment_checkpoint_usage"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
