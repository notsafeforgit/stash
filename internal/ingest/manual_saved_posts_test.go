package ingest_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func retainedFilePost(t *testing.T, f manualFileFixture) string {
	t.Helper()
	var post *models.SourcePost
	info, err := os.Stat(filepath.Join(filepath.Dir(f.path), f.input.RelativePath))
	require.NoError(t, err)
	size := info.Size()
	title, details, date := "Saved post title", "Saved post description", "2021-03-04T05:06:07Z"
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = f.service.Repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "savedpost"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(`{"category":"reddit","id":"savedpost","title":"Saved post title"}`))
		if err != nil {
			return err
		}
		_, err = f.service.Repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "gallery-dl", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload,
			Metadata: models.SourcePostMetadata{Title: &title, OriginalText: &details, PublishedAt: &date}})
		if err != nil {
			return err
		}
		observation, err := f.service.Repo.SourceFile.RecordObservation(ctx, models.SourceFileObservation{UUID: uuid.NewString(),
			CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: f.root.UUID, RootRevision: f.root.Revision,
			RelativePath: f.input.RelativePath, State: "present", Role: "local", Size: &size, Origin: "migration", ObservedAt: time.Now()})
		if err != nil {
			return err
		}
		_, err = f.service.Repo.SourceFile.RecordPostEvidence(ctx, models.SourcePostFileEvidence{
			SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "migration", Basis: "saved-path", ObservedAt: time.Now()},
			ObservationUUID:    observation.UUID})
		if err != nil {
			return err
		}
		_, err = f.service.Repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{
			SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "migration", Basis: "saved-post", ObservedAt: time.Now()},
			URL:                "https://www.reddit.com/comments/savedpost"})
		return err
	}))
	return post.UUID
}

func TestManualDuplicateIntakeSavedMetadataAndRemovalGuards(t *testing.T) {
	for _, scenario := range []string{"new_media", "existing_clear", "changed_after_preview", "changed_after_admission", "unlinked"} {
		t.Run(scenario, func(t *testing.T) {
			f := newManualFileFixture(t, models.ArchiveImage)
			worker := f.worker(t, func(ctx context.Context, _ ingest.FileWork, _ ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
				return guard(ctx)
			})
			var owner string
			if scenario == "existing_clear" || scenario == "unlinked" {
				f.input.Metadata = &ingest.ManualFileMetadata{Title: "Earliest title"}
				first := f.request(t)
				_, err := f.service.SubmitManualFile(t.Context(), first)
				require.NoError(t, err)
				_, err = worker.ProcessNext(t.Context())
				require.NoError(t, err)
				owner = f.status(t, first.RequestUUID).Publication.MediaUUID
				require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
					state, err := f.service.Repo.MetadataField.State(ctx, owner, "details")
					if err != nil {
						return err
					}
					_, err = f.service.Repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{EntityUUID: owner,
						ExpectedEntityRevision: state.Entity.Revision, Field: "details", Mode: "clear", Origin: "review"})
					return err
				}))
				body, err := os.ReadFile(f.path)
				require.NoError(t, err)
				f.input.RelativePath = "Duplicate.png"
				require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(f.path), f.input.RelativePath), body, 0600))
				f.input.Metadata = nil
			}
			f.input.DeduplicationPolicy = models.FileDeduplicationEarliest
			post := retainedFilePost(t, f)
			if scenario == "unlinked" {
				require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
					choice, err := f.service.Repo.SourcePostMedia.Association(ctx, post, owner)
					if err != nil {
						return err
					}
					_, err = f.service.Repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post,
						MediaUUID: owner, ExpectedPostRevision: choice.PostRevision, ExpectedMediaRevision: choice.MediaRevision, State: "unlinked", Origin: "review"})
					return err
				}))
			}
			preview, err := f.service.PreviewManualFile(t.Context(), f.input)
			require.NoError(t, err)
			require.Equal(t, 1, preview.SavedPostCount)
			request := ingest.ManualFileRequest{ManualFileInput: f.input, RequestUUID: uuid.NewString(), Signature: preview.Signature}
			change := func() {
				require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := f.service.Repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{
						SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post, Origin: "review", Basis: "new evidence", ObservedAt: time.Now()},
						URL:                "https://www.reddit.com/r/example/comments/savedpost"})
					return err
				}))
			}
			if scenario == "changed_after_preview" {
				change()
			}
			_, err = f.service.SubmitManualFile(t.Context(), request)
			if scenario == "changed_after_preview" {
				require.ErrorIs(t, err, ingest.ErrManualFileChanged)
				return
			}
			require.NoError(t, err)
			if scenario == "changed_after_admission" {
				change()
			}
			_, err = worker.ProcessNext(t.Context())
			require.NoError(t, err)
			status := f.status(t, request.RequestUUID)
			if scenario == "changed_after_admission" || scenario == "unlinked" {
				require.Equal(t, "failed", status.State)
				require.False(t, status.RegistrationCommitted)
				return
			}
			require.Equal(t, "succeeded", status.State, status.ErrorCode)
			if owner != "" {
				require.Equal(t, owner, status.Publication.MediaUUID)
			}
			require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				choice, err := f.service.Repo.SourcePostMedia.Association(ctx, post, status.Publication.MediaUUID)
				require.NoError(t, err)
				require.Equal(t, "linked", choice.State)
				values := map[string]string{"title": "Saved post title", "details": "Saved post description", "date": "2021-03-04"}
				if scenario == "existing_clear" {
					values["title"], values["details"] = "Earliest title", ""
				}
				for field, expected := range values {
					state, err := f.service.Repo.MetadataField.State(ctx, status.Publication.MediaUUID, field)
					require.NoError(t, err)
					var actual string
					require.NoError(t, json.Unmarshal(state.Value, &actual))
					require.Equal(t, expected, actual, field)
				}
				urls, err := f.service.Repo.MetadataField.State(ctx, status.Publication.MediaUUID, "urls")
				require.NoError(t, err)
				require.JSONEq(t, `["https://www.reddit.com/comments/savedpost"]`, string(urls.Value))
				return nil
			}))
		})
	}
}
