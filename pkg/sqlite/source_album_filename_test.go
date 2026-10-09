package sqlite_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func filenameSelectionGet(t *testing.T, repo models.Repository, post string) *models.AttachmentSelection {
	t.Helper()
	var selected *models.AttachmentSelection
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		selected, err = repo.SourceAttachment.Selection(ctx, post)
		return err
	}))
	return selected
}

func twitterFilenameFixture(t *testing.T) (sourceFileFixture, string) {
	t.Helper()
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "1575550205214134278"}, "")
	input := sourceTestCapture(t, post.UUID, 1, "profile")
	title, text := "1575550205214134278_1", "Original post description"
	input.Metadata.Title, input.Metadata.OriginalText = &title, &text
	recordSourceTestCapture(t, f.repo, input)
	return f, post.UUID
}

func TestTwitterFilenameAlbumRecoversOriginalPathsWithGaps(t *testing.T) {
	f, post := twitterFilenameFixture(t)
	// All actual library files have unrelated names. Two observations of the
	// first slot resolve to the same surviving file, not two album entries.
	albumLegacyEvidence(t, f, post, "original/1575550205214134278_1.jpg", nil, models.ArchiveImage, 41, 22)
	albumLegacyEvidence(t, f, post, "copy/1575550205214134278_1.jpg", nil, models.ArchiveImage, 41, 22)
	albumLegacyEvidence(t, f, post, "original/1575550205214134278_3.gif", nil, models.ArchiveScene, 31, 21)
	strict := albumBackfillPreview(t, f.repo, post, models.SourceAlbumIdentifiersV1)
	require.Equal(t, "ineligible", strict.Gallery.Action)
	preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumTwitterFilenameV1)
	require.Equal(t, preview, albumBackfillPreview(t, f.repo, post, preview.Policy))
	require.NotNil(t, preview.Recovery)
	require.False(t, preview.Recovery.Complete)
	require.Nil(t, preview.Recovery.ExpectedCount)
	require.Equal(t, "create", preview.Gallery.Action)
	require.Equal(t, "Original post description", preview.Gallery.Title)
	require.Len(t, preview.Gallery.Entries, 2)
	require.Equal(t, 0, preview.Gallery.Entries[0].Position)
	require.Equal(t, 2, preview.Gallery.Entries[1].Position)
	require.Len(t, preview.Gallery.Add, 2)
	require.Len(t, preview.Matches[0].Candidates[0].Proofs, 2)
	result := applyAlbumBackfill(t, f.repo, preview)
	require.True(t, result.Gallery.Created)
	require.Equal(t, 2, result.Selected)
	sourceGalleryMemberships(t, f.repo, *result.Gallery.GalleryID, []int{41}, []int{31})
	raw := openRawDB(t, f.db.DatabasePath())
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_captures"), "recovery never fabricates remote captures")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_capture_attachment_manifests"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM media_contents"), "path evidence does not claim byte verification")
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	selection := filenameSelectionGet(t, f.repo, post)
	require.False(t, selection.Complete)
	require.Nil(t, selection.ExpectedCount)
	require.Equal(t, 2, selection.Entries[1].Position)
	replay := albumBackfillPreview(t, f.repo, post, preview.Policy)
	require.Nil(t, replay.Recovery)
	require.Empty(t, replay.Gallery.Add)
	replayed := applyAlbumBackfill(t, f.repo, replay)
	require.Equal(t, result.Gallery.GalleryUUID, replayed.Gallery.GalleryUUID)
	require.Zero(t, replayed.Selected)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := f.repo.SourceAttachment.ReviewSelectionManifests(ctx, post, "", 25)
		require.NoError(t, err)
		require.Empty(t, page, "capture picker does not pretend recovered order came from a capture")
		album, err := f.repo.SourceGallery.ReadAlbum(ctx, post, -1, 25)
		require.NoError(t, err)
		require.NotNil(t, album)
		return nil
	}))
}

func TestTwitterFilenameAlbumConservativeEvidence(t *testing.T) {
	for name, filename := range map[string]string{
		"single":       "1575550205214134278_1.jpg",
		"wrong post":   "1575550205214134279_2.jpg",
		"zero":         "1575550205214134278_0.jpg",
		"leading zero": "1575550205214134278_02.jpg",
		"not final":    "1575550205214134278_2.jpg.part",
		"title suffix": "1575550205214134278_2_title.jpg",
		"unqualified":  "anything_2.jpg",
	} {
		t.Run(name, func(t *testing.T) {
			f, post := twitterFilenameFixture(t)
			albumLegacyEvidence(t, f, post, "original/"+filename, nil, models.ArchiveImage, 41, 22)
			preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumTwitterFilenameV1)
			require.Nil(t, preview.Recovery)
			require.Equal(t, "ineligible", preview.Gallery.Action)
		})
	}
	t.Run("later slot does not declare a total", func(t *testing.T) {
		f, post := twitterFilenameFixture(t)
		albumLegacyEvidence(t, f, post, "original/1575550205214134278_4.mp4", nil, models.ArchiveScene, 31, 21)
		preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumTwitterFilenameV1)
		require.Equal(t, "create", preview.Gallery.Action)
		require.Nil(t, preview.Recovery.ExpectedCount)
		require.False(t, preview.Recovery.Complete)
		require.Equal(t, 3, preview.Gallery.Entries[0].Position)
	})
	t.Run("explicit conflicting source ID", func(t *testing.T) {
		f, post := twitterFilenameFixture(t)
		albumLegacyEvidence(t, f, post, "original/1575550205214134278_2.jpg", "reddit:media:unrelated", models.ArchiveImage, 41, 22)
		preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumTwitterFilenameV1)
		require.Nil(t, preview.Recovery)
		require.Equal(t, "review", preview.Gallery.Action)
	})
	t.Run("saved disabled source list", func(t *testing.T) {
		f, post := twitterFilenameFixture(t)
		albumLegacyEvidence(t, f, post, "original/1575550205214134278_2.jpg", nil, models.ArchiveImage, 41, 22)
		applySelection(t, f.repo, models.AttachmentSelectionInput{PostUUID: post, ExpectedPostRevision: selectionPost(t, f.repo, post).Revision, Mode: "disabled", Origin: "review"})
		preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumTwitterFilenameV1)
		require.Nil(t, preview.Recovery)
		require.Equal(t, "disabled", preview.Gallery.Action)
	})
	t.Run("new evidence invalidates preview", func(t *testing.T) {
		f, post := twitterFilenameFixture(t)
		albumLegacyEvidence(t, f, post, "original/1575550205214134278_2.jpg", nil, models.ArchiveImage, 41, 22)
		preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumTwitterFilenameV1)
		albumLegacyEvidence(t, f, post, "original/1575550205214134278_3.mp4", nil, models.ArchiveScene, 31, 21)
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.SourceGallery.Backfill(ctx, post, preview.Policy, preview.Signature)
			return err
		})
		require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
		require.Nil(t, filenameSelectionGet(t, f.repo, post))
	})
	t.Run("ambiguous files are not selected", func(t *testing.T) {
		f, post := twitterFilenameFixture(t)
		albumLegacyEvidence(t, f, post, "one/1575550205214134278_2.jpg", nil, models.ArchiveImage, 41, 22)
		albumLegacyEvidence(t, f, post, "two/1575550205214134278_2.mp4", nil, models.ArchiveScene, 31, 21)
		preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumTwitterFilenameV1)
		require.Equal(t, "ambiguous", preview.Matches[0].Status)
		require.Empty(t, preview.Gallery.Add)
		require.Equal(t, "review", preview.Gallery.Action)
	})
	t.Run("separate original slots share a deduplicated survivor", func(t *testing.T) {
		f, post := twitterFilenameFixture(t)
		albumLegacyEvidence(t, f, post, "one/1575550205214134278_1.jpg", nil, models.ArchiveImage, 41, 22)
		albumLegacyEvidence(t, f, post, "one/1575550205214134278_2.jpg", nil, models.ArchiveImage, 41, 22)
		preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumTwitterFilenameV1)
		require.Len(t, preview.Gallery.Entries, 2)
		require.Len(t, preview.Gallery.Add, 1)
		result := applyAlbumBackfill(t, f.repo, preview)
		require.Equal(t, 2, result.Selected)
		sourceGalleryMemberships(t, f.repo, *result.Gallery.GalleryID, []int{41}, nil)
	})
	t.Run("discovery includes missing source lists", func(t *testing.T) {
		f, post := twitterFilenameFixture(t)
		albumLegacyEvidence(t, f, post, "one/1575550205214134278_2.jpg", nil, models.ArchiveImage, 41, 22)
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			rows, err := f.repo.SourceGallery.FilenameBackfillPosts(ctx, "", 1)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, post, rows[0].PostUUID)
			require.Equal(t, "unselected", rows[0].Mode)
			rows, err = f.repo.SourceGallery.FilenameBackfillPosts(ctx, post, 1)
			require.NoError(t, err)
			require.Empty(t, rows)
			return nil
		}))
	})
}

func TestTwitterFilenameAlbumDurablePublication(t *testing.T) {
	f, post := twitterFilenameFixture(t)
	albumLegacyEvidence(t, f, post, "original/1575550205214134278_1.jpg", nil, models.ArchiveImage, 41, 22)
	albumLegacyEvidence(t, f, post, "original/1575550205214134278_2.mp4", nil, models.ArchiveScene, 31, 21)
	service := gallery.NewAlbumBackfill(f.repo)
	preview, err := service.Preview(t.Context(), post, models.SourceAlbumTwitterFilenameV1)
	require.NoError(t, err)
	input := gallery.AlbumBackfillRequest{RequestUUID: uuid.NewString(), PostUUID: post, Policy: preview.Policy, Signature: preview.Signature}
	accepted, err := service.Submit(t.Context(), input)
	require.NoError(t, err)
	calls := 0
	worker := gallery.NewAlbumWorker(service, func(ctx context.Context, publication gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
		calls++
		require.True(t, publication.Created)
		require.Equal(t, 2, publication.Selected)
		require.Equal(t, 2, publication.Added)
		require.NotNil(t, filenameSelectionGet(t, f.repo, post))
		return guard(ctx)
	})
	processAlbumJob(t, worker)
	final := albumJobStatus(t, service, accepted.JobUUID)
	require.Equal(t, "succeeded", final.State)
	require.True(t, final.PublicationCommitted)
	require.True(t, final.HooksFinished)
	require.Equal(t, 1, calls)
	replay, err := service.Submit(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, final, replay)
}
