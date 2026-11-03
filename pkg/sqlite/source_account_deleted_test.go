package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func deletedRedgifsFixture(t *testing.T) *sourceRunFixture {
	t.Helper()
	f := newSourceRunFixture(t)
	definition := f.collection.SourceCollectionDefinition
	definition.Kind, definition.Namespace = "account", "native:redgifs"
	definition.TargetURL = "https://www.redgifs.com/users/example"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID,
		ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
	return f
}

func reportRedgifsDeletionRequest(t *testing.T, f *sourceRunFixture, run *models.SourceRun, operation string, reported bool) {
	t.Helper()
	id := uuid.NewString()
	permit, err := f.coordinator.RequestSource(t.Context(), f.token, run.Lease(), models.SourceRequestInput{
		RequestUUID: id, URL: "https://api.redgifs.com/v2/users/example/search", Class: "initial", Operation: operation})
	require.NoError(t, err)
	require.True(t, permit.Ready)
	if reported {
		require.NoError(t, f.coordinator.ReportSourceRequest(t.Context(), f.token, run.Lease(), models.SourceRequestResult{RequestUUID: id, Status: 404}))
	}
}

func TestRedgifsAccountDeletionRetiresProfileAndPreservesUnfinishedHistory(t *testing.T) {
	f := deletedRedgifsFixture(t)
	setSubscription(t, f.repo, f.collection.UUID, 0, true)
	input := f.request()
	input.SubscriptionRevision = 1
	run := f.claim(t, f.submit(t, input))
	queuedInput := input
	queuedInput.RequestUUID, queuedInput.PolicySHA256 = uuid.NewString(), strings.Repeat("b", 64)
	queued := f.submit(t, queuedInput)
	manual := f.submit(t, f.request())
	require.NotEqual(t, run.UUID, manual.UUID)
	reportRedgifsDeletionRequest(t, f, run, "listing", true)
	closed, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{
		State: "deferred", ErrorCode: "account_deleted", ErrorScope: "service:redgifs"})
	require.NoError(t, err)
	for _, id := range []string{run.UUID, queued.UUID, manual.UUID} {
		current := f.find(t, id)
		require.Equal(t, "cancelled", current.State)
		require.Equal(t, "account_deleted", current.ErrorCode)
		require.NotEmpty(t, current.Pending)
		require.Empty(t, current.Completed, "deletion never proves successful coverage")
	}
	require.Contains(t, closed.Pending, *run.Window)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		subscription, err := f.repo.SourceSubscription.Find(ctx, f.collection.UUID)
		require.NoError(t, err)
		require.False(t, subscription.Enabled)
		require.Equal(t, "retired", subscription.Collection.State)
		require.Equal(t, f.collection.TargetURL, subscription.Collection.TargetURL)
		require.Equal(t, f.collection.PathPrefix, subscription.Collection.PathPrefix)
		return nil
	}))
	attempts, err := f.coordinator.Attempts(t.Context(), f.token, run.UUID, 0)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, "deferred", attempts[0].Outcome)
	require.Equal(t, "account_deleted", attempts[0].ErrorCode)
	require.Equal(t, closed, f.submit(t, input), "original admission still replays its truthful terminal result")
	require.Nil(t, f.claim(t, closed), "retired work cannot fetch again")
	_, err = f.coordinator.Submit(t.Context(), f.token, f.request())
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
}

func TestRedgifsAccountDeletionRequiresOwnCurrentAccountAndListing(t *testing.T) {
	for _, name := range []string{"reddit parent", "media response", "unreported", "wrong outcome", "wrong scope", "retargeted", "expired"} {
		t.Run(name, func(t *testing.T) {
			f := deletedRedgifsFixture(t)
			if name == "reddit parent" {
				definition := f.collection.SourceCollectionDefinition
				definition.Namespace, definition.TargetURL = "native:reddit", "https://www.reddit.com/user/example/"
				f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID,
					ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
			}
			run := f.claim(t, f.submit(t, f.request()))
			operation := "listing"
			if name == "media response" {
				operation = "media-resolution"
			}
			reportRedgifsDeletionRequest(t, f, run, operation, name != "unreported")
			outcome := models.SourceRunOutcome{State: "deferred", ErrorCode: "account_deleted", ErrorScope: "service:redgifs"}
			if name == "wrong outcome" {
				outcome.State = "retry"
			}
			if name == "wrong scope" {
				outcome.ErrorScope = ""
			}
			if name == "retargeted" {
				definition := f.collection.SourceCollectionDefinition
				definition.TargetURL = "https://www.redgifs.com/users/newaccount"
				f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID,
					ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
			}
			if name == "expired" {
				f.now = *run.LeaseUntil
			}
			_, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), outcome)
			require.Error(t, err)
			require.Equal(t, "running", f.find(t, run.UUID).State)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				current, err := f.repo.SourceCollection.Find(ctx, f.collection.UUID)
				require.NoError(t, err)
				require.Equal(t, "active", current.State)
				return nil
			}))
		})
	}
}
