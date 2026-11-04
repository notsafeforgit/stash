package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func TestImageMergeRetainsMetadataFilesGalleriesAndSourceReferences(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	source := models.NewImage()
	source.Title, source.Details = "Later caption", "Later details"
	source.URLs = models.NewRelatedStrings([]string{"https://example.com/later"})
	file := &models.ImageFile{BaseFile: &models.BaseFile{ParentFolderID: 1, Basename: "source.jpg"}}
	keptFile := &models.ImageFile{BaseFile: &models.BaseFile{ParentFolderID: 1, Basename: "kept.jpg"}}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.File.Create(ctx, file))
		require.NoError(t, repo.File.Create(ctx, keptFile))
		require.NoError(t, repo.Image.Create(ctx, &models.CreateImageInput{Image: &source, FileIDs: []models.FileID{file.ID, keptFile.ID}}))
		require.NoError(t, repo.Image.AddFileID(ctx, 41, keptFile.ID))
		_, err := repo.Image.UpdatePartial(ctx, 41, models.ImagePartial{PrimaryFileID: &keptFile.ID, Details: models.NewOptionalString("Earliest details"),
			PerformerIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{71}},
			URLs:         &models.UpdateStrings{Mode: models.RelationshipUpdateModeSet, Values: []string{"https://example.com/first"}},
			CustomFields: models.CustomFieldsInput{Full: map[string]interface{}{"note": "Keep me"}}})
		return err
	}))
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "merge-image-post"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), sourceAttachmentEntry(1, "video")}})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, source.ID)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	album := syncSourceGallery(t, repo, post.UUID)
	manual := createArchiveGallery(t, repo, "Existing gallery")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.Gallery.AddImages(ctx, manual.ID, 41, source.ID))
		return setImageCover(ctx, repo, *album.GalleryID, source.ID)
	}))
	from := archiveFind(t, repo, models.ArchiveImage, source.ID)
	to := archiveFind(t, repo, models.ArchiveImage, 41)
	before := metadataState(t, repo, to.UUID, "details")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := image.Merge(ctx, repo, source.ID, 41, from.Revision, to.Revision)
		return err
	}))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		removed, err := repo.Image.Find(ctx, source.ID)
		require.NoError(t, err)
		require.Nil(t, removed)
		redirect, err := repo.ArchiveEntity.Find(ctx, from.UUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityRedirected, redirect.State)
		require.Equal(t, to.UUID, *redirect.RedirectTo)
		kept, err := repo.Image.Find(ctx, 41)
		require.NoError(t, err)
		require.Equal(t, "Kept image", kept.Title)
		require.Equal(t, "Earliest details", kept.Details)
		require.Equal(t, keptFile.ID, *kept.PrimaryFileID)
		fileList, err := repo.Image.GetFiles(ctx, 41)
		require.NoError(t, err)
		files := []models.FileID{}
		for _, f := range fileList {
			files = append(files, f.Base().ID)
		}
		require.ElementsMatch(t, []models.FileID{file.ID, keptFile.ID}, files)
		performers, err := repo.Image.GetPerformerIDs(ctx, 41)
		require.NoError(t, err)
		require.Equal(t, []int{71}, performers)
		urls, err := repo.Image.GetURLs(ctx, 41)
		require.NoError(t, err)
		require.Equal(t, []string{"https://example.com/first"}, urls)
		fields, err := repo.Image.GetCustomFields(ctx, 41)
		require.NoError(t, err)
		require.Equal(t, map[string]interface{}{"note": "Keep me"}, fields)
		g, err := repo.Gallery.Find(ctx, *album.GalleryID)
		require.NoError(t, err)
		require.Equal(t, to.UUID, *g.CoverMediaUUID)
		return nil
	}))
	after := metadataState(t, repo, to.UUID, "details")
	require.Equal(t, before.Value, after.Value)
	require.Equal(t, before.Decision, after.Decision)
	sourceGalleryMemberships(t, repo, manual.ID, []int{41}, nil)
	sourceGalleryMemberships(t, repo, *album.GalleryID, []int{41}, []int{31})
	syncSourceGallery(t, repo, post.UUID)
	sourceGalleryMemberships(t, repo, *album.GalleryID, []int{41}, []int{31})
	for _, event := range sourceGalleryHistory(t, repo, album.GalleryUUID) {
		require.NotEqual(t, "excluded", event.State, "merge must not create a manual removal")
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	var count int
	require.NoError(t, raw.QueryRow("SELECT count(*) FROM attachment_media_decisions WHERE media_uuid=?", from.UUID).Scan(&count))
	require.Equal(t, 1, count, "original source decision remains immutable")
}

func TestImageMergeRejectsChangedRevisionsAndRollsBack(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	source := models.NewImage()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Image.Create(ctx, &models.CreateImageInput{Image: &source})
	}))
	from := archiveFind(t, repo, models.ArchiveImage, source.ID)
	to := archiveFind(t, repo, models.ArchiveImage, 41)
	for _, test := range []struct{ source, dest, sourceRev, destRev int }{
		{source.ID, 41, from.Revision + 1, to.Revision}, {source.ID, 41, from.Revision, to.Revision + 1},
		{41, 41, to.Revision, to.Revision}, {999999, 41, 1, to.Revision}, {source.ID, 999999, from.Revision, 1},
	} {
		require.Error(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := image.Merge(ctx, repo, test.source, test.dest, test.sourceRev, test.destRev)
			return err
		}))
	}
	failure := errors.New("commit rejected")
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := image.Merge(ctx, repo, source.ID, 41, from.Revision, to.Revision)
		txn.AddPreCommitHook(ctx, func(context.Context) error { return failure })
		return err
	}), failure)
	require.Equal(t, from, archiveFind(t, repo, models.ArchiveImage, source.ID))
	require.Equal(t, to, archiveFind(t, repo, models.ArchiveImage, 41))
	_, err := image.Merge(t.Context(), repo, source.ID, 41, from.Revision, to.Revision)
	require.ErrorContains(t, err, "managed transaction")
}

func TestImageMergeAssignsSourcePrimaryToFilelessDestination(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	source := models.NewImage()
	file := &models.ImageFile{BaseFile: &models.BaseFile{ParentFolderID: 1, Basename: "source.jpg"}}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.File.Create(ctx, file))
		return repo.Image.Create(ctx, &models.CreateImageInput{Image: &source, FileIDs: []models.FileID{file.ID}})
	}))
	from := archiveFind(t, repo, models.ArchiveImage, source.ID)
	to := archiveFind(t, repo, models.ArchiveImage, 41)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		kept, err := image.Merge(ctx, repo, source.ID, 41, from.Revision, to.Revision)
		require.NoError(t, err)
		require.Equal(t, file.ID, *kept.PrimaryFileID)
		return nil
	}))
}
