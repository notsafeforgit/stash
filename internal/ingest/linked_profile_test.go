package ingest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func linkedProfileFixture(t *testing.T, target, prefix string) (captureFixture, *models.MediaRoot) {
	f := newCaptureFixture(t)
	var root *models.MediaRoot
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = f.service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{MediaRootDefinition: models.MediaRootDefinition{Label: "Media", State: "active"}, Origin: "review"})
		require.NoError(t, err)
		definition := f.collection.SourceCollectionDefinition
		definition.TargetURL, definition.RootUUID, definition.PathPrefix = target, &root.UUID, prefix
		f.collection, err = f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
		return err
	}))
	var err error
	f.credential, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	return f, root
}

func linkedProfileEvent(t *testing.T, f captureFixture, root *models.MediaRoot, directory, post string) ingest.CaptureEvent {
	event := f.event(t)
	event.RootUUID, event.Post.Value = &root.UUID, post
	raw, err := json.Marshal(map[string]any{"category": "redgifs", "id": "ExampleClip", "userName": "DifferentUploader",
		"native_redgifs_directory": directory, "urls": map[string]string{"hd": "https://media.redgifs.com/ExampleClip.mp4"},
		"_reddit": map[string]any{"category": "reddit", "id": post, "author": "Example", "author_fullname": "t2_123", "url": "https://www.redgifs.com/watch/exampleclip"}})
	require.NoError(t, err)
	event.Source, err = archive.RetainSourcePayload(raw)
	require.NoError(t, err)
	event.Metadata, err = archive.CapturedMetadata(event.Source)
	require.NoError(t, err)
	return event
}

func linkedProfileSubscriptions(t *testing.T, f captureFixture, root *models.MediaRoot) []models.SourceSubscription {
	var page *models.SourceSubscriptionPage
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		page, err = f.service.Repo.SourceSubscription.List(ctx, models.SourceSubscriptionFilter{RootUUID: root.UUID, Namespace: "native:redgifs", Limit: 100})
		return err
	}))
	return page.Items
}

func TestLinkedRedgifsProfileCaptureAtomicityOwnershipAndOptOut(t *testing.T) {
	f, root := linkedProfileFixture(t, "https://www.reddit.com/user/Example/submitted/?sort=new", ".")
	event := linkedProfileEvent(t, f, root, "Custom/Example, reddit", "post1")
	store := f.service.Repo.Ingest
	f.service.Repo.Ingest = failingReceiptStore{store}
	_, err := f.submit(t, event)
	require.ErrorContains(t, err, "receipt storage failed")
	require.Empty(t, linkedProfileSubscriptions(t, f, root), "subscription must roll back with capture receipt")
	f.service.Repo.Ingest = store
	first, err := f.submit(t, event)
	require.NoError(t, err)
	items := linkedProfileSubscriptions(t, f, root)
	require.Len(t, items, 1)
	subscription := items[0]
	require.True(t, subscription.Enabled)
	require.Equal(t, "https://www.redgifs.com/users/differentuploader", subscription.Collection.TargetURL)
	require.Equal(t, "Custom/Example, reddit", subscription.Collection.PathPrefix)
	require.Nil(t, subscription.Collection.AccountUUID, "a related URL is not another account's ownership")

	var performerID int
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		repo := f.service.Repo
		publisher, err := repo.CapturePublisher.Current(ctx, first.CaptureUUID)
		require.NoError(t, err)
		account, err := repo.SourceAccount.Find(ctx, *publisher.AccountUUID)
		require.NoError(t, err)
		p := models.NewPerformer()
		p.Name = "Profile owner"
		require.NoError(t, repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &p}))
		performerID = p.ID
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, p.ID)
		require.NoError(t, err)
		input := models.AccountOwnershipReviewInput{AccountUUID: account.UUID, AccountRevision: account.Revision,
			State: models.AccountOwnershipLinked, PerformerUUID: entity.UUID, PerformerRevision: entity.Revision}
		preview, err := repo.SourceAccount.PreviewOwnership(ctx, input)
		require.NoError(t, err)
		_, _, err = repo.SourceAccount.ApplyOwnershipReview(ctx, models.AccountOwnershipReviewApplyInput{AccountOwnershipReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
		return err
	}))
	urls := func() []string {
		var ret []string
		require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			ret, err = f.service.Repo.Performer.GetURLs(ctx, performerID)
			return err
		}))
		return ret
	}
	require.Equal(t, []string{subscription.Collection.TargetURL}, urls(), "late ownership receives the discovered profile URL")
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.service.Repo.SourceSubscription.Put(ctx, subscription.Collection.UUID, models.SourceSubscriptionInput{ExpectedRevision: subscription.Revision, Enabled: false})
		require.NoError(t, err)
		update := models.NewPerformerPartial()
		update.URLs = &models.UpdateStrings{Mode: models.RelationshipUpdateModeSet, Values: []string{}}
		_, err = f.service.Repo.Performer.UpdatePartial(ctx, performerID, update)
		return err
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	replay, err := f.submit(t, event)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	_, err = f.submit(t, linkedProfileEvent(t, f, root, "Changed folder", "post2"))
	require.NoError(t, err)
	items = linkedProfileSubscriptions(t, f, root)
	require.Len(t, items, 1)
	require.False(t, items[0].Enabled)
	require.Equal(t, subscription.Collection.PathPrefix, items[0].Collection.PathPrefix, "fresh posts must not rewrite a saved destination")
	require.Empty(t, urls(), "removed profile URLs remain suppressed")
}

func TestLinkedRedgifsProfileRequiresOwnProfileAndBoundDirectory(t *testing.T) {
	for _, tc := range []struct{ name, target, prefix, directory string }{
		{"subreddit", "https://www.reddit.com/r/example/", ".", "Example, reddit"},
		{"another author", "https://www.reddit.com/user/somebodyelse/", ".", "Example, reddit"},
		{"saved", "https://www.reddit.com/user/example/saved/", ".", "Example, reddit"},
		{"outside prefix", "https://www.reddit.com/user/example/", "Allowed", "Other/Example, reddit"},
		{"no hint", "https://www.reddit.com/user/example/", ".", ""},
		{"absolute", "https://www.reddit.com/user/example/", ".", "/outside"},
		{"parent escape", "https://www.reddit.com/user/example/", ".", "../outside"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, root := linkedProfileFixture(t, tc.target, tc.prefix)
			_, err := f.submit(t, linkedProfileEvent(t, f, root, tc.directory, "post1"))
			require.NoError(t, err)
			require.Empty(t, linkedProfileSubscriptions(t, f, root))
		})
	}
}

func TestLinkedRedgifsProfileDiscoverySurvivesPublisherReview(t *testing.T) {
	f, root := linkedProfileFixture(t, "https://www.reddit.com/user/example/", ".")
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		account, err := f.service.Repo.SourceAccount.Create(ctx, "native:reddit", "Existing handle-only account")
		require.NoError(t, err)
		now := time.Now()
		_, err = f.service.Repo.SourceAccount.ObserveIdentifier(ctx, account.UUID,
			models.AccountReference{Namespace: "native:reddit", Kind: "handle", Value: "example"},
			models.AccountIdentifierEvidence{Key: "fixture-profile", Basis: "profile-url", Origin: "review", FirstObserved: now, LastObserved: now})
		return err
	}))
	receipt, err := f.submit(t, linkedProfileEvent(t, f, root, "Example, reddit", "post1"))
	require.NoError(t, err)
	var result ingest.CaptureResult
	require.NoError(t, json.Unmarshal(receipt.Result, &result))
	require.Equal(t, "review", result.Publisher, "discovery must not bypass publisher identity review")
	require.Len(t, linkedProfileSubscriptions(t, f, root), 1, "independent Redgifs work need not wait for Reddit account review")
}

func TestLinkedRedgifsProfilePausedParentAndNewCollectionScope(t *testing.T) {
	f, root := linkedProfileFixture(t, "https://www.reddit.com/r/example/", ".")
	receipt, err := f.submit(t, linkedProfileEvent(t, f, root, "Example, reddit", "post1"))
	require.NoError(t, err)
	require.Empty(t, linkedProfileSubscriptions(t, f, root))
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := f.collection.SourceCollectionDefinition
		definition.TargetURL = "https://www.reddit.com/user/example/"
		var err error
		f.collection, err = f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
		require.NoError(t, err)
		sub, err := f.service.Repo.SourceSubscription.Put(ctx, f.collection.UUID, models.SourceSubscriptionInput{Enabled: true})
		require.NoError(t, err)
		_, err = f.service.Repo.SourceSubscription.Put(ctx, f.collection.UUID, models.SourceSubscriptionInput{ExpectedRevision: sub.Revision, Enabled: false})
		require.NoError(t, err)
		return f.service.Repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CaptureUUID: receipt.CaptureUUID, CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision})
	}))
	require.Empty(t, linkedProfileSubscriptions(t, f, root), "paused parent must not start new related subscriptions")
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		sub, err := f.service.Repo.SourceSubscription.Find(ctx, f.collection.UUID)
		require.NoError(t, err)
		_, err = f.service.Repo.SourceSubscription.Put(ctx, f.collection.UUID, models.SourceSubscriptionInput{ExpectedRevision: sub.Revision, Enabled: true})
		require.NoError(t, err)
		return f.service.Repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CaptureUUID: receipt.CaptureUUID, CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision})
	}))
	require.Len(t, linkedProfileSubscriptions(t, f, root), 1, "new profile scope must be examined even when publisher was already linked")
}
