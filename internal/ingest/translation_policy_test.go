package ingest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func enableCaptureTranslations(t *testing.T, f captureFixture) {
	t.Helper()
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.service.Repo.TranslationPolicy.Put(ctx, models.TranslationPolicyInput{CollectionUUID: f.collection.UUID, ExpectedCollectionRevision: f.collection.Revision,
			Origin: "review", Definition: models.TranslationPolicyDefinition{Enabled: true, ProviderPolicy: models.TranslationBingTextV1, TargetLanguage: "en", Title: true, Caption: true, Priority: 100}})
		return err
	}))
}

func TestCaptureTranslationSchedulingCommitsWithReceiptAndSurvivesReplay(t *testing.T) {
	f := newCaptureFixture(t)
	enableCaptureTranslations(t, f)
	event := f.event(t)
	event.Metadata.OriginalText = event.Metadata.Title
	first, err := f.submit(t, event)
	require.NoError(t, err)
	var result ingest.CaptureResult
	require.NoError(t, json.Unmarshal(first.Result, &result))
	require.NotNil(t, result.Translation)
	require.Equal(t, "recorded", result.Translation.Status)
	require.Len(t, result.Translation.Entries, 2)
	for _, entry := range result.Translation.Entries {
		require.Equal(t, "created", entry.Status)
	}
	event.EventUUID = uuid.NewString()
	second, err := f.submit(t, event)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(second.Result, &result))
	for _, entry := range result.Translation.Entries {
		require.Equal(t, "retained", entry.Status)
	}
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	replay, err := f.submit(t, event)
	require.NoError(t, err)
	require.Equal(t, second, replay)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		decision, err := f.service.Repo.TranslationPolicy.CaptureDecision(ctx, models.CollectionCapture{CollectionUUID: event.CollectionUUID, CollectionRevision: event.CollectionRevision, CaptureUUID: second.CaptureUUID})
		require.NoError(t, err)
		require.Equal(t, result.Translation, decision)
		jobs, err := f.service.Repo.ArchiveJob.List(ctx, models.ArchiveJobTranslateText, "queued", 0, 100)
		require.NoError(t, err)
		require.Empty(t, jobs)
		return nil
	}))
}

func TestCaptureTranslationWorkRollsBackWhenReceiptFails(t *testing.T) {
	f := newCaptureFixture(t)
	enableCaptureTranslations(t, f)
	event := f.event(t)
	store := f.service.Repo.Ingest
	f.service.Repo.Ingest = failingReceiptStore{store}
	_, err := f.submit(t, event)
	require.ErrorContains(t, err, "receipt storage failed")
	f.service.Repo.Ingest = store
	expected, err := archive.PrepareTranslationRequest(models.TranslationRequestInput{OriginalText: *event.Metadata.Title, TargetLanguage: "en", Policy: models.TranslationBingTextV1})
	require.NoError(t, err)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		request, err := f.service.Repo.TranslationWork.Request(ctx, expected.UUID)
		require.NoError(t, err)
		require.Nil(t, request)
		targets, err := f.service.Repo.TranslationWork.Targets(ctx, models.TranslationTargetQuery{RequestUUID: expected.UUID, State: "pending", Limit: 100})
		require.NoError(t, err)
		require.Empty(t, targets)
		return nil
	}))
	result, err := f.submit(t, event)
	require.NoError(t, err)
	var receipt ingest.CaptureResult
	require.NoError(t, json.Unmarshal(result.Result, &receipt))
	require.Equal(t, "created", receipt.Translation.Entries[1].Status)
}
