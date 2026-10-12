package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceAccessRechecksKeepProgressBeyondRetryLimit(t *testing.T) {
	for _, code := range []string{"not_found", "access_denied"} {
		t.Run(code, func(t *testing.T) {
			f := newSourceRunFixture(t)
			input := f.request()
			run := f.submit(t, input)
			progress := models.SourceRunProgress{ItemsSeen: 12, FilesCompleted: 3, Cursor: "retained-item"}
			for attempt := 0; attempt < 10; attempt++ {
				active := f.claim(t, run)
				require.NotNil(t, active)
				if attempt == 0 {
					var err error
					active, err = f.coordinator.Progress(t.Context(), f.token, active.Lease(), progress)
					require.NoError(t, err)
				}
				require.Equal(t, progress, active.Progress)
				var err error
				run, err = f.coordinator.Finish(t.Context(), f.token, active.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: code})
				require.NoError(t, err)
				require.Equal(t, "queued", run.State)
				require.Zero(t, run.Failures, "access checks do not consume the technical retry budget")
				require.Equal(t, []models.SourceWindow{input.Window}, run.Pending)
				require.Empty(t, run.Completed)
				require.Equal(t, progress, run.Progress)
				require.Equal(t, f.now.Add(24*time.Hour), run.AvailableAt)
				input.RequestUUID = uuid.NewString()
				require.Equal(t, run.AvailableAt, f.submit(t, input).AvailableAt, "scheduled submissions cannot reset the daily wait")
				f.now = run.AvailableAt.Add(-time.Millisecond)
				require.Nil(t, f.claim(t, run))
				f.now = run.AvailableAt
			}
			active := f.claim(t, run)
			require.NotNil(t, active, "a returning account remains eligible after ten days")
			run = f.finish(t, active, "succeeded")
			require.Equal(t, "succeeded", run.State)
			require.Equal(t, []models.SourceWindow{input.Window}, run.Completed)
		})
	}
}

func TestSourceAccessRecheckDoesNotBlockRedditSearch(t *testing.T) {
	f := newSourceRunFixture(t)
	definition := f.collection.SourceCollectionDefinition
	definition.Kind = "account"
	definition.TargetURL = "https://www.reddit.com/user/example/"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	input := f.request()
	input.RetrievalURL = "https://www.reddit.com/user/example/submitted/?sort=new"
	profile := f.claim(t, f.submit(t, input))
	require.NotNil(t, profile)
	profile, err := f.coordinator.Finish(t.Context(), f.token, profile.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "not_found", RetryAfterSeconds: 2 * 86400})
	require.NoError(t, err)
	require.Equal(t, f.now.Add(48*time.Hour), profile.AvailableAt, "longer source retry delays are retained")
	input.RequestUUID = uuid.NewString()
	input.RetrievalURL = "https://www.reddit.com/search?q=author%3Aexample+nsfw%3Ayes&include_over_18=on&sort=new&t=all"
	search := f.submit(t, input)
	require.Equal(t, profile.CollectionUUID, search.CollectionUUID)
	require.Nil(t, f.claim(t, search), "normal target cooldown still applies")
	f.now = f.now.Add(time.Duration(input.CooldownSeconds) * time.Second)
	search = f.claim(t, search)
	require.NotNil(t, search, "the hidden profile's wait must not block its public author search")
	search = f.finish(t, search, "succeeded")
	require.Equal(t, "succeeded", search.State)
	require.Equal(t, "queued", f.find(t, profile.UUID).State)
	require.Empty(t, f.find(t, profile.UUID).Completed, "search success cannot complete another pass's pending window")
	require.Nil(t, f.claim(t, profile))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		metrics, err := f.repo.SourceRun.RequestMetrics(ctx, f.now, 0)
		require.NoError(t, err)
		require.Len(t, metrics.Accounts, 1, "passes share one account health entry")
		require.Equal(t, input.Window.Until.UnixMilli(), metrics.Accounts[0].LastSuccessMS)
		return nil
	}))
}
