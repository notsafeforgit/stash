package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestArchiveActivityJobsPaginationAndRetryHistoryReadOnly(t *testing.T) {
	f := newDurableJobFixture(t)
	input := jobSubmission("first", "first")
	input.Arguments = json.RawMessage(`{"private_setting":"do-not-expose"}`)
	first := f.submit(t, input)
	active := f.claim(t, uuid.NewString())
	require.Equal(t, first.UUID, active.UUID)
	f.outcome(t, active, models.ArchiveJobOutcome{State: "retry", ErrorCode: "temporary_failure", Result: json.RawMessage(`{"private_result":"do-not-expose"}`), RetryAt: f.now.Add(time.Second)})
	f.now = f.now.Add(time.Second)
	active = f.claim(t, uuid.NewString())
	require.Equal(t, first.UUID, active.UUID)
	finished := f.outcome(t, active, models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{"private_result":"do-not-expose"}`)})
	second := f.submit(t, jobSubmission("second", "second"))
	third := f.submit(t, jobSubmission("third", "third"))
	active = f.claim(t, uuid.NewString())
	require.Equal(t, second.UUID, active.UUID)
	f.now = f.now.Add(time.Hour) // History inspection must not recover this lease.
	page := models.ArchiveActivityPage{Limit: 2}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.ArchiveActivity.Jobs(ctx, models.ArchiveJobActivityFilter{ArchiveActivityPage: page})
		require.NoError(t, err)
		require.Len(t, rows, 2)
		require.Equal(t, third.UUID, rows[0].UUID)
		require.Equal(t, second.UUID, rows[1].UUID)
		require.Equal(t, "running", rows[1].State)
		require.Equal(t, active.LeaseUntil, rows[1].LeaseUntil)
		next, err := f.repo.ArchiveActivity.Jobs(ctx, models.ArchiveJobActivityFilter{ArchiveActivityPage: models.ArchiveActivityPage{Before: rows[1].Sequence, Limit: 2}})
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.Equal(t, first.UUID, next[0].UUID)
		require.EqualValues(t, 2, next[0].AttemptCount)
		for _, filter := range []models.ArchiveJobActivityFilter{
			{ArchiveActivityPage: page, State: "succeeded"},
			{ArchiveActivityPage: page, State: "succeeded", Kind: models.ArchiveJobVerifyMedia},
		} {
			filtered, err := f.repo.ArchiveActivity.Jobs(ctx, filter)
			require.NoError(t, err)
			require.Equal(t, next, filtered)
		}
		empty, err := f.repo.ArchiveActivity.Jobs(ctx, models.ArchiveJobActivityFilter{ArchiveActivityPage: page, Kind: models.ArchiveJobBackfillAlbum})
		require.NoError(t, err)
		require.NotNil(t, empty)
		require.Empty(t, empty)
		one, err := f.repo.ArchiveActivity.Job(ctx, first.UUID)
		require.NoError(t, err)
		require.Equal(t, &next[0], one)
		missing, err := f.repo.ArchiveActivity.Job(ctx, uuid.NewString())
		require.NoError(t, err)
		require.Nil(t, missing)
		attempts, err := f.repo.ArchiveActivity.JobAttempts(ctx, first.UUID, models.ArchiveActivityPage{Limit: 1})
		require.NoError(t, err)
		require.Len(t, attempts, 1)
		require.EqualValues(t, 2, attempts[0].Number)
		require.Equal(t, "succeeded", attempts[0].Outcome)
		older, err := f.repo.ArchiveActivity.JobAttempts(ctx, first.UUID, models.ArchiveActivityPage{Before: 2, Limit: 1})
		require.NoError(t, err)
		require.Len(t, older, 1)
		require.Equal(t, "retry", older[0].Outcome)
		require.Equal(t, "temporary_failure", older[0].ErrorCode)
		body, err := json.Marshal([]any{rows, next, attempts, older})
		require.NoError(t, err)
		for _, excluded := range []string{"do-not-expose", "arguments", "result", "owner_uuid", "work_key", "resource_key"} {
			require.NotContains(t, string(body), excluded)
		}
		return nil
	}))
	require.Equal(t, finished, f.find(t, first.UUID))
	require.Equal(t, active, f.find(t, second.UUID))
	require.Equal(t, third, f.find(t, third.UUID))
}

func removeArchiveActivitySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeMetadataFileKeepsSchema(t, raw)
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM native_migration_history WHERE version=1000093)").Scan(&exists))
	if !exists {
		return
	}
	_, err := raw.Exec(`DROP INDEX archive_jobs_kind_history; DROP INDEX archive_jobs_state_history;
DROP INDEX source_runs_state_history; DROP INDEX source_runs_collection_state_history;
DELETE FROM native_migration_history WHERE version=1000093`)
	require.NoError(t, err)
}

func TestArchiveActivityMigrationPreservesQueuedWorkAndRequiresHistoryIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema92.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+92; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	id := uuid.NewString()
	_, err = raw.Exec(`INSERT INTO archive_jobs(uuid,kind,work_key,resource_key,arguments,priority,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'media.verify',?,?,'{"retained":true}',0,3,1000,1000,1000)`, id, strings.Repeat("a", 64), strings.Repeat("b", 64))
	require.NoError(t, err)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		work, err := repo.ArchiveJob.Find(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "queued", work.State)
		require.EqualValues(t, 0, work.Fence)
		require.JSONEq(t, `{"retained":true}`, string(work.Arguments))
		require.EqualValues(t, 1000, work.CreatedAt.UnixMilli())
		rows, err := repo.ArchiveActivity.Jobs(ctx, models.ArchiveJobActivityFilter{ArchiveActivityPage: models.ArchiveActivityPage{Limit: 10}})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, id, rows[0].UUID)
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000093 AND name='Indexed read-only archive activity history' AND details='{}'"))
	require.NoError(t, raw.Close())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
	raw = openRawDB(t, path)
	_, err = raw.Exec("DROP INDEX source_runs_state_history")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.ErrorContains(t, db.Open(path), "missing source_runs_state_history")
	require.NoError(t, db.Close())
}

func TestArchiveActivityRunsRetainOriginalCollectionAndUnfinishedWindows(t *testing.T) {
	f := newSourceRunFixture(t)
	input := f.request()
	first := f.submit(t, input)
	active := f.claim(t, first)
	f.now = f.now.Add(time.Second)
	wider := input
	wider.RequestUUID = uuid.NewString()
	wider.Window = models.SourceWindow{Until: f.now}
	f.submit(t, wider)
	partial := f.finish(t, active, "succeeded")
	require.Equal(t, "queued", partial.State, "one successful attempt does not finish a run with more windows")
	definition := f.collection.SourceCollectionDefinition
	definition.Label, definition.TargetURL = "Renamed", "https://www.reddit.com/user/other/submitted/"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
	second := f.submit(t, f.request())
	page := models.ArchiveActivityPage{Limit: 1}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.ArchiveActivity.Runs(ctx, models.SourceRunActivityFilter{ArchiveActivityPage: page, CollectionUUID: f.collection.UUID, State: "queued"})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, second.UUID, rows[0].UUID)
		require.Equal(t, "Renamed", rows[0].CollectionLabel)
		older, err := f.repo.ArchiveActivity.Runs(ctx, models.SourceRunActivityFilter{ArchiveActivityPage: models.ArchiveActivityPage{Before: second.Sequence, Limit: 5}})
		require.NoError(t, err)
		require.Len(t, older, 1)
		require.Equal(t, first.UUID, older[0].UUID)
		require.Equal(t, "Feed", older[0].CollectionLabel)
		require.Equal(t, first.TargetURL, older[0].TargetURL)
		require.Equal(t, first.CollectionRevision, older[0].CollectionRevision)
		require.Equal(t, len(partial.Completed), older[0].CompletedWindows)
		require.Equal(t, len(partial.Pending), older[0].PendingWindows)
		require.Positive(t, older[0].PendingWindows)
		require.Equal(t, "queued", older[0].State)
		one, err := f.repo.ArchiveActivity.Run(ctx, first.UUID)
		require.NoError(t, err)
		require.Equal(t, &older[0], one)
		attempts, err := f.repo.ArchiveActivity.RunAttempts(ctx, first.UUID, page)
		require.NoError(t, err)
		require.Len(t, attempts, 1)
		require.Equal(t, "succeeded", attempts[0].Outcome)
		return nil
	}))
	require.Equal(t, partial, f.find(t, first.UUID))
	require.Equal(t, second, f.find(t, second.UUID))
}

func TestArchiveActivityValidationAndIndexedPages(t *testing.T) {
	f := newSourceRunFixture(t)
	run := f.submit(t, f.request())
	page := models.ArchiveActivityPage{Limit: 50}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, filter := range []models.ArchiveJobActivityFilter{
			{ArchiveActivityPage: page, Kind: "unknown"}, {ArchiveActivityPage: page, State: "deferred"},
			{ArchiveActivityPage: models.ArchiveActivityPage{Limit: 101}}, {ArchiveActivityPage: models.ArchiveActivityPage{Before: -1, Limit: 1}}, {},
		} {
			_, err := f.repo.ArchiveActivity.Jobs(ctx, filter)
			require.ErrorIs(t, err, models.ErrArchiveActivityInvalid)
		}
		for _, filter := range []models.SourceRunActivityFilter{
			{ArchiveActivityPage: page, CollectionUUID: "not-an-id"}, {ArchiveActivityPage: page, State: "failed"},
			{ArchiveActivityPage: models.ArchiveActivityPage{Limit: 101}}, {},
		} {
			_, err := f.repo.ArchiveActivity.Runs(ctx, filter)
			require.ErrorIs(t, err, models.ErrArchiveActivityInvalid)
		}
		for _, id := range []string{"", "bad", strings.ToUpper(run.UUID)} {
			_, err := f.repo.ArchiveActivity.Job(ctx, id)
			require.ErrorIs(t, err, models.ErrArchiveActivityInvalid)
			_, err = f.repo.ArchiveActivity.RunAttempts(ctx, id, page)
			require.ErrorIs(t, err, models.ErrArchiveActivityInvalid)
		}
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	for query, index := range map[string]string{
		"SELECT id FROM archive_jobs WHERE kind='media.verify' AND id<100 ORDER BY id DESC LIMIT 50":                                                     "archive_jobs_kind_history",
		"SELECT id FROM archive_jobs WHERE state='failed' AND id<100 ORDER BY id DESC LIMIT 50":                                                          "archive_jobs_state_history",
		"SELECT id FROM archive_jobs WHERE kind='media.verify' AND state='failed' AND id<100 ORDER BY id DESC LIMIT 50":                                  "archive_jobs_list",
		"SELECT id FROM source_runs WHERE state='deferred' AND id<100 ORDER BY id DESC LIMIT 50":                                                         "source_runs_state_history",
		fmt.Sprintf("SELECT id FROM source_runs WHERE collection_uuid='%s' AND state='queued' AND id<100 ORDER BY id DESC LIMIT 50", run.CollectionUUID): "source_runs_collection_state_history",
	} {
		rows, err := raw.Query("EXPLAIN QUERY PLAN " + query)
		require.NoError(t, err)
		defer rows.Close()
		var plan string
		for rows.Next() {
			var a, b, c int
			var detail string
			require.NoError(t, rows.Scan(&a, &b, &c, &detail))
			plan += detail
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.Contains(t, plan, index)
		require.NotContains(t, plan, "TEMP B-TREE")
	}
}
