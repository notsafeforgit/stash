package sqlite_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removePostConsolidationReviewSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM native_migration_history WHERE version=1000092)").Scan(&exists))
	if !exists {
		return
	}
	_, err := raw.Exec(`DROP TABLE post_consolidation_review_media; DROP TABLE post_consolidation_review_attachments;
DROP TABLE post_consolidation_review_members; DROP TABLE post_consolidation_reviews;
DROP INDEX archive_jobs_post_merge_notify; DROP INDEX archive_jobs_work_history; DELETE FROM native_migration_history WHERE version=1000092`)
	require.NoError(t, err)
}

func TestPostConsolidationReviewMigrationPreservesPriorJobsAndCaptures(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	f.claim(t, job.UUID, 0)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	removePostConsolidationReviewSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000091,dirty=0")
	require.NoError(t, err)
	tables := []string{"archive_jobs", "archive_job_submissions", "archive_job_attempts", "enrichment_job_targets", "enrichment_targets", "source_posts", "source_post_identities", "source_captures"}
	before := map[string][][]any{}
	for _, table := range tables {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	for _, table := range tables {
		require.Equal(t, before[table], albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
