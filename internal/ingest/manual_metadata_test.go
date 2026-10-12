package ingest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestManualFileMetadataIsAtomicAndPreservesLaterEdits(t *testing.T) {
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveScene, models.ArchiveImage} {
		t.Run(string(kind), func(t *testing.T) {
			f := newManualFileFixture(t, kind)
			f.policy(t, 0, map[string]models.MetadataMapping{"title": {Value: json.RawMessage(`"Folder default"`)}})
			f.input.Metadata = &ingest.ManualFileMetadata{Title: "Phone title & \"quotes\"", Details: "First line\nSecond line 🎥", Date: "2026-10-11"}
			request := f.request(t)
			_, err := f.service.SubmitManualFile(t.Context(), request)
			require.NoError(t, err)
			// Re-encoded HTTP requests use different pointer addresses.
			raw, err := json.Marshal(request)
			require.NoError(t, err)
			var replay ingest.ManualFileRequest
			require.NoError(t, json.Unmarshal(raw, &replay))
			_, err = f.service.SubmitManualFile(t.Context(), replay)
			require.NoError(t, err)
			replay.Metadata.Title = "Changed request"
			_, err = f.service.SubmitManualFile(t.Context(), replay)
			require.ErrorIs(t, err, models.ErrArchiveJobConflict)
			worker := f.worker(t, func(ctx context.Context, _ ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
				require.NoError(t, guard(ctx))
				require.NoError(t, f.service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
					for field, expected := range map[string]string{"title": request.Metadata.Title, "details": request.Metadata.Details, "date": request.Metadata.Date} {
						state, err := f.service.Repo.MetadataField.State(ctx, result.MediaUUID, field)
						require.NoError(t, err)
						var actual string
						require.NoError(t, json.Unmarshal(state.Value, &actual))
						require.Equal(t, expected, actual)
						require.True(t, state.Protected)
					}
					return nil
				}))
				return nil
			})
			processed, err := worker.ProcessNext(t.Context())
			require.NoError(t, err)
			require.True(t, processed)
			status := f.status(t, request.RequestUUID)
			require.True(t, status.MediaIngested)
			require.ElementsMatch(t, []string{"title", "details", "date"}, status.Publication.MetadataFields)
			entity := status.Publication.MediaUUID
			require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
				state, err := f.service.Repo.MetadataField.State(ctx, entity, "details")
				if err != nil {
					return err
				}
				_, err = f.service.Repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{EntityUUID: entity,
					ExpectedEntityRevision: state.Entity.Revision, Field: "details", Mode: "clear", Origin: "review"})
				return err
			}))
			// A second upload may not restore a description the user erased.
			request2 := f.request(t)
			_, err = f.service.SubmitManualFile(t.Context(), request2)
			require.NoError(t, err)
			processed, err = f.worker(t, func(ctx context.Context, _ ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
				return guard(ctx)
			}).ProcessNext(t.Context())
			require.NoError(t, err)
			require.True(t, processed)
			second := f.status(t, request2.RequestUUID)
			require.Equal(t, entity, second.Publication.MediaUUID)
			require.Contains(t, second.Publication.Review, "metadata:details")
			require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				state, err := f.service.Repo.MetadataField.State(ctx, entity, "details")
				require.NoError(t, err)
				require.JSONEq(t, `""`, string(state.Value))
				return nil
			}))
		})
	}
}

func TestManualFileMetadataValidationAndSignature(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	before, err := f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err)
	f.input.Metadata = &ingest.ManualFileMetadata{Title: "Upload title"}
	after, err := f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err)
	require.NotEqual(t, before.Signature, after.Signature)
	require.Equal(t, before.FileSignature, after.FileSignature)
	for _, m := range []ingest.ManualFileMetadata{{Date: "2026-02-30"}, {Date: "0000-01-01"}, {Date: "2026-01-01T00:00:00Z"}, {Title: strings.Repeat("x", 4097)}, {Details: strings.Repeat("x", 16385)}, {Details: "bad\x00value"}} {
		f.input.Metadata = &m
		_, err := f.service.PreviewManualFile(t.Context(), f.input)
		require.ErrorIs(t, err, ingest.ErrInvalid)
	}
}
