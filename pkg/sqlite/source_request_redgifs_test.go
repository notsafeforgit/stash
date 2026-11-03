package sqlite_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func redgifsBackoffRequest(t *testing.T, d pacingDownload, run *models.SourceRun, path, operation string) string {
	t.Helper()
	id := uuid.NewString()
	permit, err := d.coordinator.RequestSource(t.Context(), d.token, run.Lease(), models.SourceRequestInput{
		RequestUUID: id, URL: "https://api.redgifs.com/v2/" + path, Class: "routine", Operation: operation})
	require.NoError(t, err)
	require.True(t, permit.Ready, "request %s should be admitted", path)
	return id
}

func TestRedgifsBackoffSurvivesAuthenticationAndMediaSuccess(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://www.redgifs.com/users/example")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	step := func() uint {
		return queryUint(t, raw, "SELECT backoff_step FROM source_request_budgets WHERE scope='service:redgifs'")
	}
	for i, seconds := range []int{60, 120, 240, 480, 900, 900} {
		r := d.claim(t)
		require.NotNil(t, r)
		if i > 0 {
			for _, path := range []string{"auth/temporary", "gifs/example"} {
				id := redgifsBackoffRequest(t, d, r, path, "media-resolution")
				result := models.SourceRequestResult{RequestUUID: id, Status: 200}
				require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), result))
				require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), result), "replayed acknowledgements remain idempotent")
				require.EqualValues(t, min(i, 5), step(), "%s does not establish listing recovery", path)
				f.now = f.now.Add(time.Second)
			}
		}
		id := redgifsBackoffRequest(t, d, r, "users/example/search", "listing")
		require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 429}))
		require.EqualValues(t, min(i+1, 5), step())
		until := f.now.Add(time.Duration(seconds) * time.Second)
		require.EqualValues(t, until.UnixMilli(), queryUint(t, raw, "SELECT blocked_until_ms FROM source_request_budgets WHERE scope='service:redgifs'"))
		queued, err := d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "rate_limited", ErrorScope: "service:redgifs"})
		require.NoError(t, err)
		require.Equal(t, "queued", queued.State)
		require.Zero(t, queued.Failures)
		f.now = until.Add(-time.Second)
		require.Nil(t, d.claim(t), "the next listing must wait for the full cooldown")
		f.now = until
	}
	r := d.claim(t)
	require.NotNil(t, r)
	id := redgifsBackoffRequest(t, d, r, "users/example/search", "listing")
	require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 200}))
	require.Zero(t, step(), "a successful listing restores normal pacing")
	f.now = f.now.Add(time.Second)
	id = redgifsBackoffRequest(t, d, r, "users/example/search", "listing")
	require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 429}))
	require.EqualValues(t, 1, step(), "a new outage starts at one minute after demonstrated listing recovery")
}

func TestRedgifsAccountErrorsDoNotEstablishListingRecovery(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://www.redgifs.com/users/example")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	r := d.claim(t)
	id := redgifsBackoffRequest(t, d, r, "users/example/search", "listing")
	require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 429, RetryAfterSeconds: 7}))
	f.now = f.now.Add(7 * time.Second)
	for _, status := range []int{301, 400, 401, 403, 404, 410} {
		id = redgifsBackoffRequest(t, d, r, "users/example/search", "listing")
		require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: status}))
		require.EqualValues(t, 1, queryUint(t, raw, "SELECT backoff_step FROM source_request_budgets WHERE scope='service:redgifs'"))
		f.now = f.now.Add(time.Second)
	}
	id = redgifsBackoffRequest(t, d, r, "users/another/search", "listing")
	require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 200}))
	require.Zero(t, queryUint(t, raw, "SELECT backoff_step FROM source_request_budgets WHERE scope='service:redgifs'"))
}

func TestRedgifsMetadataProbeCannotResetListingBackoff(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	r := f.claim(t, job.UUID, 0)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	request := func(path, operation string) string {
		id := uuid.NewString()
		permit, err := f.worker.RequestSource(t.Context(), f.tokens[0], r.Lease(), models.SourceRequestInput{
			RequestUUID: id, URL: "https://api.redgifs.com/v2/" + path, Class: "recovery", Operation: operation})
		require.NoError(t, err)
		require.True(t, permit.Ready)
		return id
	}
	id := request("users/example/search", "listing")
	require.NoError(t, f.worker.ReportSourceRequest(t.Context(), f.tokens[0], r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 429, RetryAfterSeconds: 7}))
	f.now = f.now.Add(7 * time.Second)
	id = request("gifs/example", "media-resolution")
	require.NoError(t, f.worker.ReportSourceRequest(t.Context(), f.tokens[0], r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 200}))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT backoff_step FROM source_request_budgets WHERE scope='service:redgifs'"))
	f.now = f.now.Add(time.Second)
	id = request("users/example/search", "listing")
	require.NoError(t, f.worker.ReportSourceRequest(t.Context(), f.tokens[0], r.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 200}))
	require.Zero(t, queryUint(t, raw, "SELECT backoff_step FROM source_request_budgets WHERE scope='service:redgifs'"))
}
