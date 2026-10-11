package sqlite_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func removeSourceRequestSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TABLE IF EXISTS metadata_request_permits; DROP TABLE source_request_posts; DROP TABLE source_request_metrics; DROP TABLE source_request_permits; DROP TABLE source_request_budgets; DROP TABLE source_check_health;
 DELETE FROM native_migration_history WHERE version IN (1000111,1000112);`)
	require.NoError(t, err)
}

func TestSourceRequestBudgetSharesSitesAndPrioritizesWaitingRoutineChecks(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	initial := newPacingDownload(t, f, "https://x.com/initial")
	routine := newPacingDownload(t, f, "https://x.com/routine")
	other := newPacingDownload(t, f, "https://www.reddit.com/user/other")
	first, second, third := initial.claim(t), routine.claim(t), other.claim(t)
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.NotNil(t, third)
	input := models.SourceRequestInput{RequestUUID: uuid.NewString(), URL: "https://x.com/i/api/graphql/key/UserMedia", Class: "initial", Operation: "listing"}
	request := func(d pacingDownload, r *models.SourceRun, in models.SourceRequestInput) *models.SourceRequestPermit {
		p, err := d.coordinator.RequestSource(t.Context(), d.token, r.Lease(), in)
		require.NoError(t, err)
		return p
	}
	require.True(t, request(initial, first, input).Ready)
	require.True(t, request(initial, first, input).Ready, "lost response retries reuse one permit")
	daily := input
	daily.RequestUUID = uuid.NewString()
	daily.Class = "routine"
	require.False(t, request(routine, second, daily).Ready)
	independent := daily
	independent.RequestUUID = uuid.NewString()
	independent.URL = "https://oauth.reddit.com/user/other/.json"
	require.True(t, request(other, third, independent).Ready, "independent services do not share a throttle")
	f.now = f.now.Add(20 * time.Second)
	input.RequestUUID = uuid.NewString()
	require.False(t, request(initial, first, input).Ready, "the waiting daily request takes priority")
	require.True(t, request(routine, second, daily).Ready)
	outcome := models.SourceRequestResult{RequestUUID: daily.RequestUUID, Status: 429, RetryAfterSeconds: 60}
	require.NoError(t, routine.coordinator.ReportSourceRequest(t.Context(), routine.token, second.Lease(), outcome))
	require.NoError(t, routine.coordinator.ReportSourceRequest(t.Context(), routine.token, second.Lease(), outcome))
	f.now = f.now.Add(20 * time.Second)
	require.False(t, request(initial, first, input).Ready, "Retry-After applies across producers")
	var metrics *models.SourceRequestSummary
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		metrics, err = f.repo.SourceRun.RequestMetrics(ctx, f.now, 0)
		return err
	}))
	var attempts, limited int64
	for _, row := range metrics.Metrics {
		attempts += row.Requests
		limited += row.RateLimited
	}
	require.EqualValues(t, 3, attempts)
	require.EqualValues(t, 1, limited)
	stale := first.Lease()
	stale.Fence++
	_, err := initial.coordinator.RequestSource(t.Context(), initial.token, stale, input)
	require.ErrorIs(t, err, models.ErrSourceRunLease)
}

func TestSourcePaginationPersistsPageWithoutInventingCompletedItems(t *testing.T) {
	f := newSourceRunFixture(t)
	run := f.claim(t, f.submit(t, f.request()))
	page := &models.SourcePagination{Extractor: "reddit:user", URL: "https://www.reddit.com/user/example/submitted/?sort=new", Cursor: "t3_previous"}
	progress := models.SourceRunProgress{Pagination: page}
	run, err := f.coordinator.Progress(t.Context(), f.token, run.Lease(), progress)
	require.NoError(t, err)
	progress.ItemsSeen = 1
	progress.FilesCompleted = 1
	progress.Cursor = "item1"
	progress.Pagination = &models.SourcePagination{Extractor: page.Extractor, URL: page.URL, Cursor: page.Cursor, Item: "item1"}
	run, err = f.coordinator.Progress(t.Context(), f.token, run.Lease(), progress)
	require.NoError(t, err)
	progress.Pagination = &models.SourcePagination{Extractor: page.Extractor, URL: page.URL, Cursor: "t3_next"}
	run, err = f.coordinator.Progress(t.Context(), f.token, run.Lease(), progress)
	require.NoError(t, err)
	require.EqualValues(t, 1, run.Progress.ItemsSeen)
	deferred, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "turn_complete"})
	require.NoError(t, err)
	f.now = deferred.AvailableAt
	resumed := f.claim(t, deferred)
	require.NotNil(t, resumed)
	require.Equal(t, progress, resumed.Progress)
}
