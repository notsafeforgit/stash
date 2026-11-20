package sqlite_test

import (
	"database/sql"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func removeBackgroundMetadataSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeImportEvidenceSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER IF EXISTS enrichment_background_attempt_current;
DROP TRIGGER IF EXISTS enrichment_background_attempt_immutable;
DROP TABLE IF EXISTS enrichment_background_attempts;
DELETE FROM native_migration_history WHERE version=1000143;`)
	require.NoError(t, err)
}

func tryBackgroundEnrichment(t *testing.T, f *enrichmentExecutionFixture, job *models.ArchiveJob) *models.ArchiveJob {
	t.Helper()
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	claimed, err := f.worker.ClaimBackground(t.Context(), f.tokens[0], job.UUID, current.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	return claimed
}

func backgroundRequest(t *testing.T, f *enrichmentExecutionFixture, job *models.ArchiveJob) *models.SourceRequestPermit {
	t.Helper()
	permit, err := f.worker.RequestSource(t.Context(), f.tokens[0], job.Lease(), models.SourceRequestInput{RequestUUID: uuid.NewString(),
		URL: "https://oauth.reddit.com/comments/abc123", Class: "recovery", Operation: "other"})
	require.NoError(t, err)
	if permit.Ready {
		require.NoError(t, f.worker.ReportSourceRequest(t.Context(), f.tokens[0], job.Lease(), models.SourceRequestResult{RequestUUID: permit.RequestUUID, Status: 200}))
	}
	return permit
}

func downloadRequest(t *testing.T, d pacingDownload, run *models.SourceRun, request string) *models.SourceRequestPermit {
	t.Helper()
	permit, err := d.coordinator.RequestSource(t.Context(), d.token, run.Lease(), models.SourceRequestInput{RequestUUID: request,
		URL: "https://oauth.reddit.com/user/other/submitted", Class: "recovery", Operation: "listing"})
	require.NoError(t, err)
	if permit.Ready {
		require.NoError(t, d.coordinator.ReportSourceRequest(t.Context(), d.token, run.Lease(), models.SourceRequestResult{RequestUUID: request, Status: 200}))
	}
	return permit
}

func TestBackgroundMetadataUsesIdleRequestsBesideDownloads(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	d := newPacingDownload(t, f, "https://reddit.com/user/other")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()

	require.Nil(t, tryBackgroundEnrichment(t, f, job), "a queued download keeps an idle service")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_enrichment_waiters"), "background claims never register turn interest")
	run := d.claim(t)
	require.NotNil(t, run)
	require.Nil(t, tryPacingEnrichment(t, f, job), "an ordinary claim still waits for its turn")
	starts := queryUint(t, raw, "SELECT download_starts FROM source_service_turns WHERE scope='service:reddit'")

	bg := tryBackgroundEnrichment(t, f, job)
	require.NotNil(t, bg, "metadata can run beside a running download")
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_background_attempts"))
	require.Equal(t, starts, queryUint(t, raw, "SELECT download_starts FROM source_service_turns WHERE scope='service:reddit'"),
		"a background attempt is not a metadata turn")
	second := newPacingDownload(t, f, "https://reddit.com/user/another")
	require.NotNil(t, second.claim(t), "downloads do not wait for background metadata")

	require.True(t, backgroundRequest(t, f, bg).Ready, "an unused request slot goes to background metadata")
	waiting := downloadRequest(t, d, run, uuid.NewString())
	require.False(t, waiting.Ready)
	require.Positive(t, waiting.WaitMS, "shared request spacing still applies to the download")
	f.now = f.now.Add(5 * time.Second)
	held := backgroundRequest(t, f, bg)
	require.False(t, held.Ready, "a waiting download keeps precedence for the next slot")
	require.EqualValues(t, 5000, held.WaitMS)
	require.True(t, downloadRequest(t, d, run, waiting.RequestUUID).Ready)
	clock := backgroundRequest(t, f, bg)
	require.False(t, clock.Ready)
	require.Positive(t, clock.WaitMS)
	f.now = f.now.Add(5 * time.Second)
	cadence := backgroundRequest(t, f, bg)
	require.False(t, cadence.Ready, "a download granted within two spacings keeps the next slot for its next request")
	require.EqualValues(t, 5000, cadence.WaitMS)
	f.now = f.now.Add(5 * time.Second)
	require.True(t, backgroundRequest(t, f, bg).Ready, "after two idle spacings the slot goes to background metadata")

	for elapsed := 15 * time.Second; elapsed < 2*time.Minute; elapsed += 50 * time.Second {
		var err error
		bg, err = f.worker.Renew(t.Context(), f.tokens[0], bg.Lease(), time.Minute)
		require.NoError(t, err)
		run, err = d.coordinator.Renew(t.Context(), d.token, run.Lease(), time.Minute)
		require.NoError(t, err)
		f.now = f.now.Add(50 * time.Second)
	}
	require.True(t, downloadRequest(t, d, run, uuid.NewString()).Ready)
	require.False(t, downloadRequest(t, d, run, uuid.NewString()).Ready)
	yield := backgroundRequest(t, f, bg)
	require.False(t, yield.Ready)
	require.Zero(t, yield.WaitMS, "after two minutes, well inside the worker's fetch deadline, waiting behind downloads yields the job")
	_, err := f.worker.Fail(t.Context(), f.tokens[0], bg.Lease(), "budget_yield")
	require.NoError(t, err)
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "queued", current.State)
	require.Zero(t, current.Failures)
}

func TestBackgroundClaimTakesAFreeTurnWithoutWaiting(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	claimed := tryBackgroundEnrichment(t, f, job)
	require.NotNil(t, claimed, "an idle service needs no download beside it")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_background_attempts"), "a free turn is an ordinary attempt")
	d := newPacingDownload(t, f, "https://reddit.com/user/other")
	require.Nil(t, d.claim(t), "the ordinary attempt keeps its metadata turn")
	_, err := raw.Exec("INSERT INTO enrichment_background_attempts(job_uuid,fence) VALUES(?,?)", job.UUID, claimed.Fence+1)
	require.ErrorContains(t, err, "background metadata requires the current running attempt")
}

func TestBackgroundMetadataKeepsCooldownsAndDownloadPreference(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	d := newPacingDownload(t, f, "https://reddit.com/user/other")
	run := d.claim(t)
	require.NotNil(t, run)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("UPDATE source_pacing SET available_at_ms=? WHERE scope='service:reddit'", f.now.Add(time.Minute).UnixMilli())
	require.NoError(t, err)
	require.Nil(t, tryBackgroundEnrichment(t, f, job), "a service cooldown also holds background metadata")
	_, err = raw.Exec("UPDATE source_pacing SET available_at_ms=0 WHERE scope='service:reddit'")
	require.NoError(t, err)
	_, err = d.coordinator.Finish(t.Context(), d.token, run.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.NoError(t, err)
	newPacingDownload(t, f, "https://reddit.com/user/queued")
	require.Nil(t, tryBackgroundEnrichment(t, f, job), "a queued download is served first when none is running")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_job_attempts WHERE job_uuid='"+job.UUID+"'"))
}

func TestBackgroundTimeoutAfterYieldDoesNotPauseTheService(t *testing.T) {
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		overrun bool
	}{
		{name: "early timeout is a site timeout", elapsed: 30 * time.Second},
		{name: "timeout after the yield is the worker's own deadline", elapsed: 150 * time.Second, overrun: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			d := newPacingDownload(t, f, "https://reddit.com/user/other")
			run := d.claim(t)
			require.NotNil(t, run)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			bg := tryBackgroundEnrichment(t, f, job)
			require.NotNil(t, bg)
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_background_attempts"))
			for elapsed := time.Duration(0); elapsed < tc.elapsed; elapsed += 50 * time.Second {
				step := min(50*time.Second, tc.elapsed-elapsed)
				f.now = f.now.Add(step)
				var err error
				bg, err = f.worker.Renew(t.Context(), f.tokens[0], bg.Lease(), time.Minute)
				require.NoError(t, err)
				run, err = d.coordinator.Renew(t.Context(), d.token, run.Lease(), time.Minute)
				require.NoError(t, err)
			}
			before := f.now
			_, err := f.worker.Fail(t.Context(), f.tokens[0], bg.Lease(), "timeout")
			require.NoError(t, err)
			current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
			require.NoError(t, err)
			require.Equal(t, "queued", current.State)
			paused := queryUint(t, raw, "SELECT count(*) FROM source_pacing WHERE scope='service:reddit' AND available_at_ms>"+
				strconv.FormatInt(before.Add(30*time.Minute).UnixMilli(), 10))
			require.Equal(t, 1, current.Failures, "a timeout keeps its failure and retry delay")
			if tc.overrun {
				require.Zero(t, paused, "a background overrun does not pause downloads on the service")
				next := newPacingDownload(t, f, "https://reddit.com/user/next")
				require.NotNil(t, next.claim(t), "the service stays available to downloads")
			} else {
				require.EqualValues(t, 1, paused, "an early timeout still pauses the service")
			}
		})
	}
}
