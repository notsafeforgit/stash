package sqlite_test

import (
	"database/sql"
	"errors"
	"github.com/stashapp/stash/pkg/sqlite"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func backoffRequest(t *testing.T, d pacingDownload, run *models.SourceRun, id string) *models.SourceRequestPermit {
	t.Helper()
	p, err := d.coordinator.RequestSource(t.Context(), d.token, run.Lease(), models.SourceRequestInput{
		RequestUUID: id, URL: "https://www.reddit.com/user/example/submitted/.json", Class: "routine", Operation: "listing"})
	require.NoError(t, err)
	return p
}

func TestSourceBackoffRetainsProgressAndUsesOneRecoveryProbe(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://www.reddit.com/user/example")
	other := newPacingDownload(t, f, "https://old.reddit.com/user/another")
	r := d.claim(t)
	progress := models.SourceRunProgress{ItemsSeen: 1, FilesCompleted: 1, Cursor: "saved-item", Pagination: &models.SourcePagination{
		Extractor: "reddit:user", URL: "https://www.reddit.com/user/example/submitted/?sort=new", Cursor: "t3_previous", Item: "saved-item"}}
	r, err := d.coordinator.Progress(t.Context(), d.token, r.Lease(), progress)
	require.NoError(t, err)
	id := uuid.NewString()
	require.True(t, backoffRequest(t, d, r, id).Ready)
	result := models.SourceRequestResult{RequestUUID: id, Status: 403, BlockReason: "network_security"}
	require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), result))
	queued, err := d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "rate_limited", ErrorScope: "service:reddit"})
	require.NoError(t, err)
	require.Equal(t, "queued", queued.State)
	require.Zero(t, queued.Failures)
	require.Empty(t, queued.Completed)
	require.NotEmpty(t, queued.Pending)
	require.Equal(t, progress, queued.Progress)
	require.Nil(t, d.claim(t))
	require.Nil(t, other.claim(t))
	independent := newPacingDownload(t, f, "https://x.com/independent")
	require.NotNil(t, independent.claim(t))
	f.now = f.now.Add(time.Minute)
	r = d.claim(t)
	second := other.claim(t)
	require.NotNil(t, r)
	require.NotNil(t, second)
	require.Equal(t, progress, r.Progress)
	id = uuid.NewString()
	require.True(t, backoffRequest(t, d, r, id).Ready)
	require.True(t, backoffRequest(t, d, r, id).Ready, "lost permit response reuses the same probe")
	f.now = f.now.Add(5 * time.Second)
	secondID := id // Request IDs are scoped to attempts, not globally unique.
	require.False(t, backoffRequest(t, other, second, secondID).Ready, "normal spacing alone cannot admit another probe")
	require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 200}))
	require.True(t, backoffRequest(t, other, second, secondID).Ready, "successful probe restores normal pacing immediately")
}

func TestSourceBackoffGrowsBoundsAndNeverExhaustsAccounts(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://www.reddit.com/user/example")
	for i, seconds := range []int{60, 120, 240, 480, 900, 900, 900, 900, 900, 900} {
		r := d.claim(t)
		require.NotNil(t, r)
		id := uuid.NewString()
		require.True(t, backoffRequest(t, d, r, id).Ready)
		result := models.SourceRequestResult{RequestUUID: id, Status: 403, BlockReason: "network_security"}
		require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), result))
		f.now = f.now.Add(time.Second)
		require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), result), "lost report acknowledgement is idempotent")
		queued, err := d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "rate_limited", ErrorScope: "service:reddit"})
		require.NoError(t, err)
		require.Zero(t, queued.Failures, "attempt %d", i)
		require.Equal(t, "queued", queued.State)
		require.Empty(t, queued.Completed)
		f.now = f.now.Add(time.Duration(seconds-2) * time.Second)
		require.Nil(t, d.claim(t))
		f.now = f.now.Add(time.Second)
	}
}

func TestSourceBackoffHonorsRetryAfterAndIgnoresEarlierSuccess(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://www.reddit.com/user/example")
	other := newPacingDownload(t, f, "https://old.reddit.com/user/another")
	r, second := d.claim(t), other.claim(t)
	id, otherID := uuid.NewString(), uuid.NewString()
	require.True(t, backoffRequest(t, d, r, id).Ready)
	f.now = f.now.Add(5 * time.Second)
	require.True(t, backoffRequest(t, other, second, otherID).Ready)
	require.NoError(t, other.coordinator.ReportSourceRequest(t.Context(), other.token, second.Lease(), models.SourceRequestResult{RequestUUID: otherID, Status: 429, RetryAfterSeconds: 7}))
	f.now = f.now.Add(time.Second)
	require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 200}))
	third := newPacingDownload(t, f, "https://www.reddit.com/user/third")
	require.Nil(t, third.claim(t), "a success from before the block cannot release it")
	f.now = f.now.Add(6 * time.Second)
	probe := third.claim(t)
	require.NotNil(t, probe, "an explicit short Retry-After avoids an unnecessary one-hour delay")
	require.True(t, backoffRequest(t, third, probe, uuid.NewString()).Ready)
	f.now = f.now.Add(2 * time.Minute)
	fourth := newPacingDownload(t, f, "https://www.reddit.com/user/fourth")
	resumed := fourth.claim(t)
	require.NotNil(t, resumed)
	require.True(t, backoffRequest(t, fourth, resumed, uuid.NewString()).Ready, "an interrupted probe has a bounded lease")
}

func TestMetadataBackoffRetainsJobBeyondFailureLimit(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	for i := 0; i < 10; i++ {
		r := tryPacingEnrichment(t, f, job)
		require.NotNil(t, r)
		id := uuid.NewString()
		permit, err := f.worker.RequestSource(t.Context(), f.tokens[0], r.Lease(), models.SourceRequestInput{RequestUUID: id, URL: "https://www.reddit.com/comments/example/.json", Class: "recovery", Operation: "other"})
		require.NoError(t, err)
		require.True(t, permit.Ready)
		require.NoError(t, f.worker.ReportSourceRequest(t.Context(), f.tokens[0], r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 403, BlockReason: "network_security", RetryAfterSeconds: 10}))
		_, err = f.worker.Fail(t.Context(), f.tokens[0], r.Lease(), "rate_limited")
		require.NoError(t, err)
		job, err = f.worker.Find(t.Context(), f.tokens[0], job.UUID)
		require.NoError(t, err)
		require.Equal(t, "queued", job.State)
		require.Zero(t, job.Failures)
		f.now = f.now.Add(10 * time.Second)
	}
}

func removeSourceBackoffSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`ALTER TABLE source_request_budgets DROP COLUMN backoff_step;
 ALTER TABLE source_request_budgets DROP COLUMN backoff_at_ms;
 ALTER TABLE source_request_budgets DROP COLUMN blocked_until_ms;
 ALTER TABLE source_request_budgets DROP COLUMN probe_uuid;
 ALTER TABLE source_request_budgets DROP COLUMN probe_until_ms;
 ALTER TABLE source_request_budgets DROP COLUMN probe_owner;
 DROP TRIGGER archive_job_transition;
 ALTER TABLE source_request_permits DROP COLUMN backoff;
 ALTER TABLE metadata_request_permits DROP COLUMN backoff;
 DELETE FROM native_migration_history WHERE version=1000127`)
	require.NoError(t, err)
	old, err := os.ReadFile("migrations/1000112_metadata_request_budget.up.sql")
	require.NoError(t, err)
	_, trigger, _ := strings.Cut(string(old), "DROP TRIGGER archive_job_transition;")
	trigger, _, _ = strings.Cut(trigger, "INSERT INTO native_migration_history")
	_, err = raw.Exec(trigger)
	require.NoError(t, err)
}

func TestSourceBackoffMigrationPreservesRequestHistoryAndReopens(t *testing.T) {
	db, _ := archiveTestDatabase(t)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeSourceBackoffSchema(t, raw)
	_, err := raw.Exec(`UPDATE schema_migrations SET version=1000126,dirty=0;
 INSERT INTO source_pacing(scope) VALUES('service:reddit');
 INSERT INTO source_request_budgets(scope,next_at_ms,maintained_hour) VALUES('service:reddit',12345,100);
 INSERT INTO source_request_metrics(hour,scope,class,operation,requests,responses,rate_limited,errors) VALUES(100,'service:reddit','routine','listing',20,20,1,1)`)
	require.NoError(t, err)
	before := albumJobRows(t, raw, "source_request_metrics")
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.Equal(t, before, albumJobRows(t, raw, "source_request_metrics"))
	require.EqualValues(t, 12345, queryUint(t, raw, "SELECT next_at_ms FROM source_request_budgets"))
	require.Zero(t, queryUint(t, raw, "SELECT backoff_step FROM source_request_budgets"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
}
