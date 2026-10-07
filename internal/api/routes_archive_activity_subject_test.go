package api

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
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestArchiveActivityHTTPManualContextSurvivesMissingFileAndCollectionRename(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "Purchased.mp4")
	require.NoError(t, os.WriteFile(path, []byte("awaiting media verification"), 0600))
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err := repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Files", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Purchased originally", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: ".",
		}})
		return err
	}))
	service := ingest.New(repo)
	input := ingest.ManualFileInput{CollectionUUID: collection.UUID, RelativePath: "Purchased.mp4", MediaKind: models.ArchiveScene}
	preview, err := service.PreviewManualFile(t.Context(), input)
	require.NoError(t, err)
	request := ingest.ManualFileRequest{ManualFileInput: input, RequestUUID: uuid.NewString(), Signature: preview.Signature}
	accepted, err := service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	definition := collection.SourceCollectionDefinition
	definition.Label, definition.State = "Renamed and disabled", "disabled"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	body := get("/activity/jobs/"+accepted.JobUUID, 200).Body.Bytes()
	var detail activityJobDetail
	require.NoError(t, json.Unmarshal(body, &detail))
	require.True(t, detail.ContextAvailable)
	require.Equal(t, "Purchased.mp4", detail.RelativePath)
	require.Equal(t, request.RequestUUID, detail.ManualRequestUUID)
	require.Equal(t, []activitySubject{{Kind: "collection", UUID: collection.UUID, RequestedUUID: collection.UUID, Title: "Purchased originally", State: "active", Revision: 1}}, detail.Subjects)
	for _, hidden := range []string{dir, "change_token", "arguments", "settings", "publication", "producer_uuid", "signature"} {
		require.NotContains(t, string(body), hidden)
	}
	after, err := service.ManualFileStatus(t.Context(), request.RequestUUID)
	require.NoError(t, err)
	require.Equal(t, accepted, after, "activity must neither retry work nor consult the missing file")
}

func TestArchiveActivityHTTPAlbumContextIncludesCommittedGallery(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	post := albumHTTPPost(t, repo)
	service := gallery.NewAlbumBackfill(repo)
	preview, err := service.Preview(t.Context(), post.UUID, models.SourceAlbumIdentifiersV1)
	require.NoError(t, err)
	accepted, err := service.Submit(t.Context(), gallery.AlbumBackfillRequest{RequestUUID: uuid.NewString(), PostUUID: post.UUID, Policy: preview.Policy, Signature: preview.Signature})
	require.NoError(t, err)
	var before activityJobDetail
	require.NoError(t, json.Unmarshal(get("/activity/jobs/"+accepted.JobUUID, 200).Body.Bytes(), &before))
	require.True(t, before.ContextAvailable)
	require.Len(t, before.Subjects, 1)
	require.Equal(t, "Album title", before.Subjects[0].Title)
	require.Equal(t, post.UUID, before.Subjects[0].UUID)
	worker := gallery.NewAlbumWorker(service, func(ctx context.Context, _ gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
		return guard(ctx)
	})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	var after activityJobDetail
	require.NoError(t, json.Unmarshal(get("/activity/jobs/"+accepted.JobUUID, 200).Body.Bytes(), &after))
	require.Equal(t, "succeeded", after.Summary.State)
	require.Len(t, after.Subjects, 2)
	require.Equal(t, "gallery", after.Subjects[1].Kind)
	require.NotNil(t, after.Subjects[1].LocalID)
	require.True(t, after.ContextAvailable)
}

func TestArchiveActivityHTTPMergeSubjectsResolveToOneCanonicalPost(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	post := albumHTTPPost(t, repo)
	input := models.PostConsolidationReviewInput{DestinationUUID: post.UUID, Reason: "Same original post"}
	var applied *models.PostConsolidationReview
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		legacy, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "old"}, "")
		if err != nil {
			return err
		}
		input.SourceUUID = legacy.UUID
		preview, err := repo.SourceEvidence.PreviewConsolidationReview(ctx, input)
		if err != nil {
			return err
		}
		applied, _, err = repo.SourceEvidence.ApplyConsolidationReview(ctx, models.PostConsolidationReviewApplyInput{PostConsolidationReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}, time.Now().UTC())
		return err
	}))
	var detail activityJobDetail
	require.NoError(t, json.Unmarshal(get("/activity/jobs/"+applied.Result.NotificationJobUUID, 200).Body.Bytes(), &detail))
	require.True(t, detail.ContextAvailable)
	require.Equal(t, applied.Request.RequestUUID, detail.MergeRequestUUID)
	require.Len(t, detail.Subjects, 2, "merged post aliases must share one subject alongside the published gallery")
	require.Equal(t, "post", detail.Subjects[0].Kind)
	require.Equal(t, post.UUID, detail.Subjects[0].UUID)
	require.Equal(t, input.SourceUUID, detail.Subjects[0].RequestedUUID)
	require.Equal(t, "Album title", detail.Subjects[0].Title)
	require.Equal(t, "gallery", detail.Subjects[1].Kind)
	require.Equal(t, applied.Result.Gallery.GalleryUUID, detail.Subjects[1].UUID)
	require.Equal(t, "queued", detail.Summary.State, "reading cannot deliver notifications")
}
