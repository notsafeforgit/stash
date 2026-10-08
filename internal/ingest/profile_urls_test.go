package ingest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCaptureProfileURLsUseExistingMetadataAndPreserveRemovalOnRetry(t *testing.T) {
	f := newCaptureFixture(t)
	updates := 0
	f.service.Repo.TxnManager = models.WithEntityUpdateNotifier(f.service.Repo.TxnManager, func(_ context.Context, kind models.ArchiveEntityKind, _ int, fields []string) {
		require.Equal(t, models.ArchivePerformer, kind)
		require.Equal(t, []string{"urls"}, fields)
		updates++
	})
	event := f.event(t)
	var source map[string]interface{}
	require.NoError(t, json.Unmarshal(event.Source, &source))
	withProfile := func(id, bio string) {
		source["user"] = map[string]interface{}{"id": id, "name": "Example", "subreddit": map[string]interface{}{"public_description": bio}}
		raw, err := json.Marshal(source)
		require.NoError(t, err)
		event.Source, err = archive.RetainSourcePayload(raw)
		require.NoError(t, err)
		event.EventUUID = uuid.NewString()
	}
	withProfile("123", "https://site.invalid")
	firstEvent := event
	first, err := f.submit(t, event)
	require.NoError(t, err)
	var performerID int
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		repo := f.service.Repo
		publisher, err := repo.CapturePublisher.Current(ctx, first.CaptureUUID)
		require.NoError(t, err)
		account, err := repo.SourceAccount.Find(ctx, *publisher.AccountUUID)
		require.NoError(t, err)
		p := models.NewPerformer()
		p.Name = "Linked owner"
		require.NoError(t, repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &p}))
		performerID = p.ID
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, p.ID)
		require.NoError(t, err)
		input := models.AccountOwnershipReviewInput{AccountUUID: account.UUID, AccountRevision: account.Revision,
			State: models.AccountOwnershipLinked, PerformerUUID: entity.UUID, PerformerRevision: entity.Revision}
		preview, err := repo.SourceAccount.PreviewOwnership(ctx, input)
		require.NoError(t, err)
		request := models.AccountOwnershipReviewApplyInput{AccountOwnershipReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
		_, _, err = repo.SourceAccount.ApplyOwnershipReview(ctx, request)
		require.NoError(t, err)
		_, replayed, err := repo.SourceAccount.ApplyOwnershipReview(ctx, request)
		require.True(t, replayed)
		return err
	}))
	urls := func() []string {
		var result []string
		require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			result, err = f.service.Repo.Performer.GetURLs(ctx, performerID)
			return err
		}))
		return result
	}
	require.Equal(t, []string{"https://site.invalid"}, urls())
	require.Equal(t, 1, updates, "ownership review notifies once after its transaction commits")
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		update := models.NewPerformerPartial()
		update.URLs = &models.UpdateStrings{Mode: models.RelationshipUpdateModeSet, Values: []string{}}
		_, err := f.service.Repo.Performer.UpdatePartial(ctx, performerID, update)
		return err
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	replayed, err := f.submit(t, firstEvent)
	require.NoError(t, err)
	require.Equal(t, first, replayed)
	require.Empty(t, urls())
	require.Equal(t, 1, updates, "capture replay is not another performer edit")
	withProfile("123", "https://site.invalid https://new.invalid")
	_, err = f.submit(t, event)
	require.NoError(t, err)
	require.Equal(t, []string{"https://new.invalid"}, urls())
	require.Equal(t, 2, updates, "fresh capture notifies only for the newly added URL")
	withProfile("different-feed-owner", "https://wrong.invalid")
	_, err = f.submit(t, event)
	require.NoError(t, err)
	require.Equal(t, []string{"https://new.invalid"}, urls())
	require.Equal(t, 2, updates)
}
