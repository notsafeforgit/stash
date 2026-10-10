package ingest_test

import (
	"context"
	"image"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func manualGIFEffects(ctx context.Context, _ ingest.FileWork, _ ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
	return guard(ctx)
}

type manualGIFFixture struct {
	manualFileFixture
	imageUUID   string
	imageID     int
	galleryID   int
	performerID int
	postUUID    string
}

func newManualGIFFixture(t *testing.T) manualGIFFixture {
	t.Helper()
	f := newManualFileFixture(t, models.ArchiveImage)
	require.NoError(t, os.Remove(f.path))
	f.path = filepath.Join(f.root.Binding.Path, "legacy.gif")
	f.input.RelativePath = "legacy.gif"
	output, err := os.Create(f.path)
	require.NoError(t, err)
	require.NoError(t, gif.Encode(output, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil))
	require.NoError(t, output.Close())
	request := f.request(t)
	_, err = f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	_, err = f.worker(t, manualGIFEffects).ProcessNext(t.Context())
	require.NoError(t, err)
	status := f.status(t, request.RequestUUID)
	require.Equal(t, "succeeded", status.State)
	ret := manualGIFFixture{manualFileFixture: f, imageUUID: status.Publication.MediaUUID}
	repo := f.service.Repo
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		entity, err := repo.ArchiveEntity.Find(ctx, ret.imageUUID)
		require.NoError(t, err)
		ret.imageID = *entity.LocalID
		performer := models.NewPerformer()
		performer.Name = "Retained performer"
		require.NoError(t, repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &performer}))
		ret.performerID = performer.ID
		partial := models.NewImagePartial()
		partial.Title = models.NewOptionalString("Retained title")
		partial.PerformerIDs = &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{performer.ID}}
		_, err = repo.Image.UpdatePartial(ctx, ret.imageID, partial)
		require.NoError(t, err)
		gallery := models.NewGallery()
		gallery.Title = "Retained gallery"
		require.NoError(t, repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &gallery}))
		ret.galleryID = gallery.ID
		require.NoError(t, repo.Gallery.AddImages(ctx, gallery.ID, ret.imageID))
		require.NoError(t, repo.Gallery.SetCover(ctx, gallery.ID, ret.imageUUID))
		post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "legacygif"}, uuid.NewString())
		require.NoError(t, err)
		ret.postUUID = post.UUID
		association, err := repo.SourcePostMedia.Association(ctx, post.UUID, ret.imageUUID)
		require.NoError(t, err)
		_, err = repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{
			UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: ret.imageUUID,
			ExpectedPostRevision: association.PostRevision, ExpectedMediaRevision: association.MediaRevision,
			ExpectedDecisions: []string{}, State: "linked", Origin: "review",
		})
		return err
	}))
	ffmpeg, err := exec.LookPath("ffmpeg")
	require.NoError(t, err)
	log, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-i", f.path, "-c:v", "ffv1", filepath.Join(f.root.Binding.Path, "legacy.mkv")).CombinedOutput()
	require.NoError(t, err, string(log))
	require.NoError(t, os.Remove(f.path))
	ret.input.RelativePath, ret.input.MediaKind, ret.input.OriginalGIFPath = "legacy.mkv", models.ArchiveScene, "legacy.gif"
	ret.path = filepath.Join(f.root.Binding.Path, "legacy.mkv")
	return ret
}

func (f manualGIFFixture) assertConverted(t *testing.T, status *ingest.ManualFileStatus) {
	t.Helper()
	require.Equal(t, "succeeded", status.State)
	require.True(t, status.MediaIngested)
	require.NotNil(t, status.Publication.Conversion)
	require.Equal(t, f.imageUUID, status.Publication.Conversion.ImageUUID)
	repo := f.service.Repo
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		old, err := repo.ArchiveEntity.Find(ctx, f.imageUUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityRedirected, old.State)
		entity, err := repo.ArchiveEntity.Resolve(ctx, f.imageUUID)
		require.NoError(t, err)
		require.Equal(t, status.Publication.MediaUUID, entity.UUID)
		scene, err := repo.Scene.Find(ctx, *entity.LocalID)
		require.NoError(t, err)
		require.Equal(t, "Retained title", scene.Title)
		performers, err := repo.Scene.GetPerformerIDs(ctx, scene.ID)
		require.NoError(t, err)
		require.Equal(t, []int{f.performerID}, performers)
		gallery, err := repo.Gallery.Find(ctx, f.galleryID)
		require.NoError(t, err)
		require.Equal(t, entity.UUID, *gallery.CoverMediaUUID)
		images, err := repo.Gallery.GetImageIDs(ctx, f.galleryID)
		require.NoError(t, err)
		require.Empty(t, images)
		scenes, err := repo.Gallery.GetSceneIDs(ctx, f.galleryID)
		require.NoError(t, err)
		require.Equal(t, []int{scene.ID}, scenes)
		association, err := repo.SourcePostMedia.Association(ctx, f.postUUID, entity.UUID)
		require.NoError(t, err)
		require.False(t, association.Suppressed())
		require.NotEmpty(t, association.Decisions)
		captures, err := repo.SourceCollection.Captures(ctx, f.collection.UUID, nil, 10)
		require.NoError(t, err)
		require.Empty(t, captures, "repair keeps the imported post link without inventing a scrape")
		return nil
	}))
}

func TestManualGIFConversionWithoutSourceRetainsMetadataAndGalleryAfterRestart(t *testing.T) {
	f := newManualGIFFixture(t)
	preview, err := f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err)
	require.Equal(t, f.imageUUID, preview.OriginalImageUUID)
	require.Empty(t, preview.ExistingSceneUUID)
	request := ingest.ManualFileRequest{ManualFileInput: f.input, Signature: preview.Signature, RequestUUID: uuid.NewString()}
	_, err = f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	database := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(database))
	f.service = ingest.New(f.db.Repository())
	processed, err := f.worker(t, func(ctx context.Context, work ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		require.Nil(t, work.Publication.Source, "local conversion must not invent a source capture")
		require.NotNil(t, result.Conversion)
		return guard(ctx)
	}).ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	f.assertConverted(t, f.status(t, request.RequestUUID))
	replayed, err := f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	f.assertConverted(t, replayed)
	_, err = f.worker(t, manualGIFEffects).ProcessNext(t.Context())
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(database))
}

func TestManualGIFConversionReusesCompatibleSceneButRejectsConflicts(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "compatible", true: "conflicting title"}[conflict], func(t *testing.T) {
			f := newManualGIFFixture(t)
			plain := f.manualFileFixture
			plain.input.OriginalGIFPath = ""
			first := plain.request(t)
			_, err := plain.service.SubmitManualFile(t.Context(), first)
			require.NoError(t, err)
			_, err = plain.worker(t, manualGIFEffects).ProcessNext(t.Context())
			require.NoError(t, err)
			destination := plain.status(t, first.RequestUUID).Publication.MediaUUID
			repo := f.service.Repo
			title := "Retained title"
			if conflict {
				title = "A different edited title"
			}
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				entity, err := repo.ArchiveEntity.Find(ctx, destination)
				require.NoError(t, err)
				partial := models.NewScenePartial()
				partial.Title = models.NewOptionalString(title)
				_, err = repo.Scene.UpdatePartial(ctx, *entity.LocalID, partial)
				return err
			}))
			preview, err := f.service.PreviewManualFile(t.Context(), f.input)
			require.NoError(t, err)
			require.Equal(t, destination, preview.ExistingSceneUUID)
			request := ingest.ManualFileRequest{ManualFileInput: f.input, Signature: preview.Signature, RequestUUID: uuid.NewString()}
			_, err = f.service.SubmitManualFile(t.Context(), request)
			require.NoError(t, err)
			_, err = f.worker(t, manualGIFEffects).ProcessNext(t.Context())
			require.NoError(t, err)
			status := f.status(t, request.RequestUUID)
			if !conflict {
				f.assertConverted(t, status)
				require.Equal(t, destination, status.Publication.MediaUUID)
				require.False(t, status.Publication.MediaCreated)
				return
			}
			require.False(t, status.RegistrationCommitted)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				original, err := repo.Image.Find(ctx, f.imageID)
				require.NoError(t, err)
				require.NotNil(t, original, "failed conversion rolls back without deleting the image")
				entity, err := repo.ArchiveEntity.Find(ctx, destination)
				require.NoError(t, err)
				scene, err := repo.Scene.Find(ctx, *entity.LocalID)
				require.NoError(t, err)
				require.Equal(t, title, scene.Title)
				return nil
			}))
		})
	}
}

func TestManualGIFConversionRejectsChangedReviewAndInvalidClaims(t *testing.T) {
	f := newManualGIFFixture(t)
	for _, change := range []func(*ingest.ManualFileInput){
		func(i *ingest.ManualFileInput) { i.OriginalGIFPath = "../legacy.gif" },
		func(i *ingest.ManualFileInput) { i.OriginalGIFPath = "different.gif" },
		func(i *ingest.ManualFileInput) { i.MediaKind = models.ArchiveImage },
		func(i *ingest.ManualFileInput) { i.ScanCollectionUUID = i.CollectionUUID },
	} {
		input := f.input
		change(&input)
		_, err := f.service.PreviewManualFile(t.Context(), input)
		require.ErrorIs(t, err, ingest.ErrInvalid)
	}
	request := f.request(t)
	repo := f.service.Repo
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		partial := models.NewImagePartial()
		partial.Title = models.NewOptionalString("Changed after preview")
		_, err := repo.Image.UpdatePartial(ctx, f.imageID, partial)
		return err
	}))
	_, err := f.service.SubmitManualFile(t.Context(), request)
	require.ErrorIs(t, err, ingest.ErrManualFileChanged)
	request = f.request(t)
	_, err = f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	// A downloader recreating the original path must prevent retirement.
	require.NoError(t, os.WriteFile(filepath.Join(f.root.Binding.Path, "legacy.gif"), []byte("replacement"), 0600))
	_, err = f.worker(t, manualGIFEffects).ProcessNext(t.Context())
	require.NoError(t, err)
	require.False(t, f.status(t, request.RequestUUID).RegistrationCommitted)
}
