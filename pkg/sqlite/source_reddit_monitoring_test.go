package sqlite_test

import (
	"encoding/json"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func redditMonitoringFixture(t *testing.T, code, scope string, complete bool) (*sourceRunFixture, *models.SourceRun, models.SourceRunRequest) {
	t.Helper()
	f := newSourceRunFixture(t)
	d := f.collection.SourceCollectionDefinition
	d.Kind, d.TargetURL = "account", "https://www.reddit.com/user/example/"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: d})
	if complete {
		for _, part := range []string{"reddit-new", "reddit-top"} {
			importBackfill(t, f, backfillLegacy(t, f.root.UUID, uuid.NewString(), "example", part))
		}
	}
	in := f.request()
	in.RetrievalURL = "https://www.reddit.com/user/example/submitted/?sort=new"
	profile := f.claim(t, f.submit(t, in))
	reserveURL := "https://www.reddit.com/user/example/about.json"
	if scope == "service:redgifs" {
		reserveURL = "https://api.redgifs.com/v2/gifs/example"
	}
	_, err := f.coordinator.ReserveSource(t.Context(), f.token, profile.Lease(), reserveURL)
	require.NoError(t, err)
	profile, err = f.coordinator.Finish(t.Context(), f.token, profile.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: code, ErrorScope: scope})
	require.NoError(t, err)
	f.now = f.now.Add(time.Hour * 48)
	in.RequestUUID = uuid.NewString()
	in.RetrievalURL = "https://www.reddit.com/search?q=author%3Aexample+nsfw%3Ayes&include_over_18=on&sort=new&t=all"
	return f, profile, in
}

func monitoringPermit(t *testing.T, f *sourceRunFixture, run *models.SourceRun, class string) *models.SourceRequestPermit {
	t.Helper()
	p, err := f.coordinator.RequestSource(t.Context(), f.token, run.Lease(), models.SourceRequestInput{
		RequestUUID: uuid.NewString(), URL: "https://www.reddit.com/search.json?q=author%3Aexample", Class: class, Operation: "listing", AccountMonitoringProtocol: 1})
	require.NoError(t, err)
	return p
}

func TestRedditMonitoringPauseUsesExistingEvidenceAndRetainsCoverage(t *testing.T) {
	for _, code := range []string{"not_found", "account_not_found", "account_suspended"} {
		t.Run(code, func(t *testing.T) {
			f, profile, in := redditMonitoringFixture(t, code, "service:reddit", true)
			run := f.claim(t, f.submit(t, in))
			require.NotNil(t, run)
			progress := models.SourceRunProgress{ItemsSeen: 12, FilesCompleted: 3, Cursor: "retained"}
			run, err := f.coordinator.Progress(t.Context(), f.token, run.Lease(), progress)
			require.NoError(t, err)
			p := monitoringPermit(t, f, run, "routine")
			require.False(t, p.Ready)
			require.Equal(t, "account_unavailable", p.PauseReason)
			require.Zero(t, p.WaitMS, "the worker releases its lease instead of sleeping")
			queued, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "account_check_pending"})
			require.NoError(t, err)
			require.Zero(t, queued.Failures)
			require.Equal(t, []models.SourceWindow{in.Window}, queued.Pending)
			require.Empty(t, queued.Completed)
			require.Equal(t, progress, queued.Progress)
			require.Equal(t, f.now.Add(30*24*time.Hour), queued.AvailableAt)
			in.RequestUUID = uuid.NewString()
			require.Equal(t, queued.AvailableAt, f.submit(t, in).AvailableAt, "daily admissions coalesce without resetting the pause")
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_request_metrics"), "no website request was granted or recorded")
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_run_attempt_failures WHERE run_uuid='"+run.UUID+"'"), "the local pause is not a new source failure")
			f.now = profile.AvailableAt
			recovered := f.claim(t, profile)
			require.NotNil(t, recovered, "search pause cannot postpone the monthly profile check")
			f.finish(t, recovered, "succeeded")
			require.Equal(t, f.now, f.find(t, queued.UUID).AvailableAt, "profile recovery releases the retained search immediately")
			f.now = f.now.Add(5 * time.Second)
			require.True(t, monitoringPermit(t, f, f.claim(t, queued), "routine").Ready)
		})
	}
}

func TestRedditMonitoringPauseExcludesHistoryRecoveryAndUnrelatedFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code, scope, class string
		complete                 bool
	}{
		{"unfinished history", "account_suspended", "service:reddit", "routine", false},
		{"initial", "account_not_found", "service:reddit", "initial", true},
		{"recovery", "account_not_found", "service:reddit", "recovery", true},
		{"access denied", "access_denied", "service:reddit", "routine", true},
		{"rate limit", "rate_limited", "service:reddit", "routine", true},
		{"linked host", "not_found", "service:redgifs", "routine", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, in := redditMonitoringFixture(t, tc.code, tc.scope, tc.complete)
			run := f.claim(t, f.submit(t, in))
			require.NotNil(t, run)
			p := monitoringPermit(t, f, run, tc.class)
			require.True(t, p.Ready)
			require.Empty(t, p.PauseReason)
		})
	}
}

func TestRedditMonitoringSearchSuccessDoesNotRecoverProfile(t *testing.T) {
	f, _, in := redditMonitoringFixture(t, "account_not_found", "service:reddit", true)
	search := f.claim(t, f.submit(t, in))
	require.True(t, monitoringPermit(t, f, search, "initial").Ready)
	f.finish(t, search, "succeeded")
	f.now = f.now.Add(time.Hour)
	in.RequestUUID = uuid.NewString()
	in.Window.Until = f.now
	search = f.claim(t, f.submit(t, in))
	require.Equal(t, "account_unavailable", monitoringPermit(t, f, search, "routine").PauseReason)
}

func TestRedditMonitoringUsesHealthyProfileEvenWhenLaterDownloadFails(t *testing.T) {
	f, profile, in := redditMonitoringFixture(t, "account_not_found", "service:reddit", true)
	run := f.claim(t, f.submit(t, in))
	require.Equal(t, "account_unavailable", monitoringPermit(t, f, run, "routine").PauseReason)
	queued, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "account_check_pending"})
	require.NoError(t, err)
	f.now = profile.AvailableAt
	profile = f.claim(t, profile)
	require.NotNil(t, profile)
	event := ingest.AccountProfileEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: profile.UUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: &f.root.UUID, OwnerUUID: profile.OwnerUUID, Fence: profile.Fence,
		Kind: "account.profile", ObservedAt: f.now.Add(-60 * 24 * time.Hour), ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion,
		Source: []byte(`{"category":"reddit","user":{"id":"abc123","name":"example"}}`)}
	body, err := json.Marshal(event)
	require.NoError(t, err)
	_, err = f.service.AccountProfile(t.Context(), f.token, body, ingest.Digest(body))
	require.NoError(t, err)
	require.Equal(t, queued.AvailableAt, f.find(t, queued.UUID).AvailableAt, "late delivery of an old observation cannot recover the account")
	event.EventUUID = uuid.NewString()
	event.ObservedAt = f.now
	body, err = json.Marshal(event)
	require.NoError(t, err)
	_, err = f.service.AccountProfile(t.Context(), f.token, body, ingest.Digest(body))
	require.NoError(t, err)
	require.Equal(t, f.now, f.find(t, queued.UUID).AvailableAt, "healthy about response releases the search before listing completion")
	f.finish(t, profile, "retry")
	f.now = f.now.Add(6 * time.Minute)
	require.True(t, monitoringPermit(t, f, f.claim(t, queued), "routine").Ready)
}
