package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func reviewAccounts(t *testing.T, repo models.Repository, filter models.AccountReviewFilter) []models.AccountReviewState {
	t.Helper()
	var rows []models.AccountReviewState
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		rows, err = repo.SourceAccount.ReviewAccounts(ctx, filter)
		return err
	}))
	return rows
}

func TestAccountReviewScopeKeepsIncidentalPublishersOutOfQueues(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:reddit", "reddit", `{"category":"reddit","id":"example","author":"Incidental","author_fullname":"t2_incidental"}`)
	publisher, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, ""), "automatic"))
	require.NoError(t, err)
	account := findSourceAccount(t, repo, *publisher.AccountUUID)
	feed := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Community", Kind: "subreddit", Namespace: account.Namespace, State: "active", TargetURL: "https://www.reddit.com/r/example/"}})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CaptureUUID: capture.UUID, CollectionUUID: feed.UUID, CollectionRevision: feed.Revision})
	}))
	media := archiveFind(t, repo, models.ArchiveImage, 41)
	_, err = applyPostMedia(repo, postMediaInput(t, repo, capture.PostUUID, media.UUID, "linked"))
	require.NoError(t, err)
	require.Empty(t, reviewAccounts(t, repo, models.AccountReviewFilter{}))
	require.Empty(t, archiveQueue(t, repo, "accounts", "", 25).Items)
	all := reviewAccounts(t, repo, models.AccountReviewFilter{Scope: "all"})
	require.Len(t, all, 1)
	require.False(t, all[0].Tracked, "a post with downloaded media is still not an account subscription")
	require.False(t, accountReviewState(t, repo, account.UUID).Tracked, "direct post-author links remain readable")

	source := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Subscribed profile", Kind: "account", Namespace: account.Namespace, State: "active", AccountUUID: &account.UUID,
		TargetURL: "https://www.reddit.com/user/incidental/"}})
	tracked := reviewAccounts(t, repo, models.AccountReviewFilter{Scope: "tracked"})
	require.Len(t, tracked, 1)
	require.Equal(t, account.UUID, tracked[0].UUID)
	require.True(t, tracked[0].Tracked)
	require.Len(t, archiveQueue(t, repo, "accounts", "", 25).Items, 1)

	definition := source.SourceCollectionDefinition
	definition.AccountUUID = nil
	putSourceCollection(t, repo, models.SourceCollectionInput{UUID: source.UUID, ExpectedRevision: source.Revision, Origin: "review", SourceCollectionDefinition: definition})
	require.Empty(t, reviewAccounts(t, repo, models.AccountReviewFilter{}), "an old source revision must not re-enrol an account")

	_, _, err = applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, 0)))
	require.NoError(t, err)
	require.True(t, accountReviewState(t, repo, account.UUID).Tracked, "explicit unlinks remain accessible under tracked accounts")
	require.Empty(t, archiveQueue(t, repo, "accounts", "", 25).Items)
	choice := accountReviewInput(t, repo, account.UUID, 0)
	choice.State = models.AccountOwnershipUndecided
	_, _, err = applyAccountReview(repo, accountReviewRequest(t, repo, choice))
	require.NoError(t, err)
	require.Len(t, archiveQueue(t, repo, "accounts", "", 25).Items, 1, "an explicit request to review is retained")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		publishers, err := repo.CapturePublisher.PostAccounts(ctx, capture.PostUUID, "", 25)
		require.NoError(t, err)
		require.Len(t, publishers, 1)
		require.Equal(t, account.UUID, publishers[0].UUID, "queue filtering preserves attribution")
		return nil
	}))
}

func TestAccountReviewScopeFollowsConsolidationAndIgnoresHistoricalSourceAliases(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	former, current := createSourceAccount(t, repo, "native:reddit"), createSourceAccount(t, repo, "native:reddit")
	putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Imported account folder", Kind: "legacy_catalog", Namespace: former.Namespace, State: "disabled", AccountUUID: &former.UUID}})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		preview, err := repo.SourceAccount.PreviewConsolidation(ctx, former.UUID, current.UUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceAccount.Consolidate(ctx, models.AccountConsolidationInput{SourceUUID: former.UUID, DestinationUUID: current.UUID,
			Signature: preview.Signature, OwnershipMode: "preserve", Origin: "review"})
		return err
	}))
	rows := reviewAccounts(t, repo, models.AccountReviewFilter{})
	require.Len(t, rows, 1)
	require.Equal(t, current.UUID, rows[0].UUID)
	require.True(t, accountReviewState(t, repo, former.UUID).Tracked)

	incidental := createSourceAccount(t, repo, "native:reddit")
	oldSource := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Historical pass", Kind: "account", Namespace: incidental.Namespace, State: "active", AccountUUID: &incidental.UUID}})
	newSource := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Current source", Kind: "account", Namespace: current.Namespace, State: "active", AccountUUID: &current.UUID}})
	attachmentSQL(t, db, "INSERT INTO source_collection_aliases(alias_uuid,source_uuid) VALUES(?,?)", oldSource.UUID, newSource.UUID)
	rows = reviewAccounts(t, repo, models.AccountReviewFilter{Limit: 1})
	require.Len(t, rows, 1)
	require.Equal(t, current.UUID, rows[0].UUID)
	require.Empty(t, reviewAccounts(t, repo, models.AccountReviewFilter{After: rows[0].UUID, Limit: 1}))
	require.False(t, accountReviewState(t, repo, incidental.UUID).Tracked)
}
