package sqlite_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type threadAlbumMedia struct {
	kind models.ArchiveEntityKind
	id   int
}

func newThreadAlbumImage(t *testing.T, repo models.Repository) int {
	t.Helper()
	image := models.NewImage()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Image.Create(ctx, &models.CreateImageInput{Image: &image})
	}))
	return image.ID
}

// separateAlbumPost builds a native post whose own album was created before
// any thread evidence linked it to other posts.
func separateAlbumPost(t *testing.T, repo models.Repository, id string, media ...threadAlbumMedia) (string, *models.SourceGallerySyncResult) {
	t.Helper()
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: id}, "")
	entries := make([]models.SourceAttachmentEntry, len(media))
	for i, m := range media {
		kind := "image"
		if m.kind == models.ArchiveScene {
			kind = "video"
		}
		entries[i] = albumEntry(i, "native:twitter", fmt.Sprintf("%s-%d", id, i), kind)
	}
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, Entries: entries})
	for i, m := range media {
		chooseAlbumMedia(t, repo, selection.Entries[i].Attachment.UUID, m.kind, m.id)
	}
	album := syncSourceGallery(t, repo, post.UUID)
	require.True(t, album.Created)
	return post.UUID, album
}

func observeThread(t *testing.T, repo models.Repository, post, id, root, parent, author, replyAuthor string) {
	t.Helper()
	raw := fmt.Sprintf(`{"category":"twitter","tweet_id":%q,"conversation_id":%q,"reply_id":%q,"reply_user_id":%q,"author":{"id":%q}}`, id, root, parent, replyAuthor, author)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", []byte(raw))
	require.NoError(t, err)
	capture := recordSourceTestCapture(t, repo, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post, Origin: "gallery-dl", Platform: "twitter",
		CapturedAt: time.Now(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		status, err := repo.SourceThread.ObserveCapture(ctx, capture)
		require.Equal(t, "recorded", status)
		return err
	}))
}

func previewThreadAlbums(t *testing.T, repo models.Repository, post string) (*models.GalleryConsolidationPreview, error) {
	t.Helper()
	var ret *models.GalleryConsolidationPreview
	err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.PreviewThreadConsolidation(ctx, post)
		return err
	})
	return ret, err
}

func consolidateThreadAlbums(t *testing.T, repo models.Repository, post, signature string) (*models.ThreadGalleryConsolidationResult, error) {
	t.Helper()
	var ret *models.ThreadGalleryConsolidationResult
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.ConsolidateThread(ctx, post, signature)
		return err
	})
	return ret, err
}

// A thread whose posts already hold separate albums never merges them
// automatically. The owner's reviewed consolidation combines them into the
// album of the earliest post, then the thread's own sync adds the rest.
func TestThreadAlbumConsolidationCombinesSeparateAlbumsIntoTheOpeningAlbum(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	first, second, third := newThreadAlbumImage(t, repo), newThreadAlbumImage(t, repo), newThreadAlbumImage(t, repo)
	root, opening := separateAlbumPost(t, repo, "100", threadAlbumMedia{models.ArchiveImage, 41}, threadAlbumMedia{models.ArchiveScene, 31})
	reply, separate := separateAlbumPost(t, repo, "102", threadAlbumMedia{models.ArchiveScene, 32}, threadAlbumMedia{models.ArchiveImage, first})
	observeThread(t, repo, root, "100", "100", "", "99", "")
	observeThread(t, repo, reply, "102", "100", "100", "99", "99")
	later := threadPost(t, repo, "103", "100", "102", "99", "99")
	selection := selectAlbum(t, repo, later, models.SourceAttachmentManifestInput{Complete: true, Entries: []models.SourceAttachmentEntry{
		albumEntry(0, "native:twitter", "503", "image"), albumEntry(1, "native:twitter", "504", "image"),
	}})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, second)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveImage, third)
	require.Equal(t, "review", sourceGalleryPreview(t, repo, root).Action, "thread evidence alone does not merge albums")

	preview, err := previewThreadAlbums(t, repo, later)
	require.NoError(t, err)
	require.Equal(t, opening.GalleryUUID, preview.GalleryUUID, "the opening post's album survives")
	require.Len(t, preview.Galleries, 2)
	require.Equal(t, []string{opening.GalleryUUID, separate.GalleryUUID}, []string{preview.Galleries[0].UUID, preview.Galleries[1].UUID})
	require.Equal(t, 2, preview.ImageCount)
	require.Equal(t, 2, preview.SceneCount)
	require.Empty(t, preview.Conflicts)

	_, err = consolidateThreadAlbums(t, repo, later, "stale")
	require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	sourceGalleryMemberships(t, repo, *separate.GalleryID, []int{first}, []int{32})

	result, err := consolidateThreadAlbums(t, repo, later, preview.Signature)
	require.NoError(t, err)
	require.Equal(t, opening.GalleryUUID, result.GalleryUUID)
	require.Equal(t, []string{separate.GalleryUUID}, result.MergedUUIDs)
	require.Equal(t, "sync", result.ThreadAction)
	require.Len(t, result.ThreadAdded, 2, "the remaining reply's media join the combined album")
	sourceGalleryMemberships(t, repo, *opening.GalleryID, []int{41, first, second, third}, []int{31, 32})
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, post := range []string{root, reply, later} {
			association, err := repo.SourceGallery.Association(ctx, post)
			require.NoError(t, err)
			require.Equal(t, opening.GalleryUUID, *association.GalleryUUID)
		}
		redirected, err := repo.ArchiveEntity.Resolve(ctx, separate.GalleryUUID)
		require.NoError(t, err)
		require.Equal(t, opening.GalleryUUID, redirected.UUID, "old album links follow the combined album")
		return nil
	}))
	current := sourceGalleryPreview(t, repo, root)
	require.Equal(t, "sync", current.Action)
	require.Empty(t, current.Add)
	require.Empty(t, current.Remove)

	again, err := previewThreadAlbums(t, repo, root)
	require.NoError(t, err)
	require.Len(t, again.Galleries, 1)
	repeat, err := consolidateThreadAlbums(t, repo, root, again.Signature)
	require.NoError(t, err)
	require.Empty(t, repeat.MergedUUIDs)
	require.Empty(t, repeat.ThreadAdded)
	require.Empty(t, repeat.ThreadRemoved)
}

func TestThreadAlbumConsolidationRefusesOutsideAlbumsAndThreadsNeedingReview(t *testing.T) {
	t.Run("album_used_outside_the_thread", func(t *testing.T) {
		_, repo := archiveTestDatabase(t)
		extra := newThreadAlbumImage(t, repo)
		root, _ := separateAlbumPost(t, repo, "100", threadAlbumMedia{models.ArchiveImage, 41}, threadAlbumMedia{models.ArchiveScene, 31})
		reply, separate := separateAlbumPost(t, repo, "102", threadAlbumMedia{models.ArchiveScene, 32}, threadAlbumMedia{models.ArchiveImage, extra})
		observeThread(t, repo, root, "100", "100", "", "99", "")
		observeThread(t, repo, reply, "102", "100", "100", "99", "99")
		// An unrelated post sharing media joins the reply's album by the
		// ordinary overlap rule; the thread action must not pull it in.
		other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "200"}, "")
		selection := selectAlbum(t, repo, other.UUID, models.SourceAttachmentManifestInput{Complete: true, Entries: []models.SourceAttachmentEntry{
			albumEntry(0, "native:twitter", "200-0", "video"), albumEntry(1, "native:twitter", "200-1", "image"),
		}})
		chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveScene, 32)
		chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveImage, newThreadAlbumImage(t, repo))
		syncSourceGallery(t, repo, other.UUID)
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			association, err := repo.SourceGallery.Association(ctx, other.UUID)
			require.NoError(t, err)
			require.Equal(t, separate.GalleryUUID, *association.GalleryUUID)
			return nil
		}))
		_, err := previewThreadAlbums(t, repo, root)
		require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	})
	t.Run("thread_needs_review", func(t *testing.T) {
		_, repo := archiveTestDatabase(t)
		reply := threadPost(t, repo, "102", "100", "100", "99", "99")
		threadPost(t, repo, "100", "100", "", "88", "")
		_, err := previewThreadAlbums(t, repo, reply)
		require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	})
	t.Run("single_post", func(t *testing.T) {
		_, repo := archiveTestDatabase(t)
		alone := threadPost(t, repo, "100", "100", "", "99", "")
		_, err := previewThreadAlbums(t, repo, alone)
		require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	})
	t.Run("unknown_post", func(t *testing.T) {
		_, repo := archiveTestDatabase(t)
		for _, post := range []string{"not-a-post", uuid.NewString()} {
			_, err := previewThreadAlbums(t, repo, post)
			require.ErrorIs(t, err, models.ErrNotFound)
		}
	})
}

// A post whose album was chosen by review keeps that choice's origin when its
// album joins the thread's; the thread's sync must not drop its media.
func TestThreadAlbumConsolidationKeepsReviewedAlbumChoiceMedia(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	first, second := newThreadAlbumImage(t, repo), newThreadAlbumImage(t, repo)
	root, opening := separateAlbumPost(t, repo, "100", threadAlbumMedia{models.ArchiveImage, 41}, threadAlbumMedia{models.ArchiveScene, 31})
	reply, separate := separateAlbumPost(t, repo, "102", threadAlbumMedia{models.ArchiveScene, 32}, threadAlbumMedia{models.ArchiveImage, first})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := repo.SourceEvidence.FindPost(ctx, reply)
		require.NoError(t, err)
		gallery, err := repo.ArchiveEntity.Find(ctx, separate.GalleryUUID)
		require.NoError(t, err)
		input := models.GalleryAssociationReviewInput{PostUUID: reply, PostRevision: post.Revision, State: "linked",
			GalleryUUID: separate.GalleryUUID, GalleryRevision: gallery.Revision, Reason: "Kept after a reviewed merge"}
		preview, err := repo.SourceGallery.PreviewAssociationReview(ctx, input)
		require.NoError(t, err)
		_, _, err = repo.SourceGallery.ApplyAssociationReview(ctx, models.GalleryAssociationReviewApplyInput{
			GalleryAssociationReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
		return err
	}))
	observeThread(t, repo, root, "100", "100", "", "99", "")
	observeThread(t, repo, reply, "102", "100", "100", "99", "99")
	later := threadPost(t, repo, "103", "100", "102", "99", "99")
	threadMedia(t, repo, later, "503", models.ArchiveImage, second)

	preview, err := previewThreadAlbums(t, repo, root)
	require.NoError(t, err)
	require.Equal(t, []string{opening.GalleryUUID, separate.GalleryUUID}, []string{preview.Galleries[0].UUID, preview.Galleries[1].UUID})
	result, err := consolidateThreadAlbums(t, repo, root, preview.Signature)
	require.NoError(t, err)
	require.Equal(t, []string{separate.GalleryUUID}, result.MergedUUIDs)
	require.Empty(t, result.ThreadRemoved)
	sourceGalleryMemberships(t, repo, *opening.GalleryID, []int{41, first, second}, []int{31, 32})
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		association, err := repo.SourceGallery.Association(ctx, reply)
		require.NoError(t, err)
		require.Equal(t, opening.GalleryUUID, *association.GalleryUUID)
		require.Equal(t, "review", association.Origin, "the reviewed choice keeps its origin")
		return nil
	}))
	// A later ordinary sync keeps the reviewed post's media too.
	resync := syncSourceGallery(t, repo, later)
	require.Empty(t, resync.Removed)
	sourceGalleryMemberships(t, repo, *opening.GalleryID, []int{41, first, second}, []int{31, 32})
}
