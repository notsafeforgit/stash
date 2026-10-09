package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeSourceThreadSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TRIGGER post_gallery_thread_share_insert;
DROP TRIGGER post_gallery_thread_share_update;
DROP TABLE source_post_threads;
DROP INDEX post_gallery_links_gallery;
CREATE UNIQUE INDEX post_gallery_links_gallery ON post_gallery_links(gallery_uuid) WHERE gallery_uuid IS NOT NULL;
DELETE FROM native_migration_history WHERE version=1000105`)
	require.NoError(t, err)
}

func threadPost(t *testing.T, repo models.Repository, id, root, parent, author, replyAuthor string) string {
	t.Helper()
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: id}, "")
	raw := fmt.Sprintf(`{"category":"twitter","tweet_id":%q,"conversation_id":%q,"reply_id":%q,"reply_user_id":%q,"author":{"id":%q}}`, id, root, parent, replyAuthor, author)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", []byte(raw))
	require.NoError(t, err)
	input := models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "gallery-dl", Platform: "twitter", CapturedAt: time.Now(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload}
	capture := recordSourceTestCapture(t, repo, input)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		status, err := repo.SourceThread.ObserveCapture(ctx, capture)
		require.NoError(t, err)
		require.Equal(t, "recorded", status)
		return err
	}))
	return post.UUID
}

func threadMedia(t *testing.T, repo models.Repository, post, ref string, kind models.ArchiveEntityKind, local int) *models.AttachmentSelection {
	t.Helper()
	mediaKind := "image"
	if kind == models.ArchiveScene {
		mediaKind = "video"
	}
	selection := selectAlbum(t, repo, post, models.SourceAttachmentManifestInput{Complete: true, Entries: []models.SourceAttachmentEntry{albumEntry(0, "native:twitter", ref, mediaKind)}})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, kind, local)
	return selection
}

func TestThreadGalleryOutOfOrderRepliesAndMissingRoot(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	third := threadPost(t, repo, "103", "100", "102", "99", "99")
	threadMedia(t, repo, third, "503", models.ArchiveImage, 41)
	require.Equal(t, "ineligible", syncSourceGallery(t, repo, third).Action)
	second := threadPost(t, repo, "102", "100", "100", "99", "99")
	threadMedia(t, repo, second, "502", models.ArchiveScene, 31)
	preview := sourceGalleryPreview(t, repo, second)
	require.Equal(t, "create", preview.Action)
	require.Len(t, preview.ThreadPlans, 2)
	require.Equal(t, second, preview.ThreadPlans[0].PostUUID)
	album := syncSourceGallery(t, repo, second)
	require.True(t, album.Created)
	require.Len(t, album.Added, 2)
	sourceGalleryMemberships(t, repo, *album.GalleryID, []int{41}, []int{31})
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, id := range []string{second, third} {
			a, err := repo.SourceGallery.Association(ctx, id)
			require.NoError(t, err)
			require.Equal(t, album.GalleryUUID, *a.GalleryUUID)
		}
		view, err := repo.SourceThread.Read(ctx, third, "", 1)
		require.NoError(t, err)
		require.Nil(t, view.Root.Post)
		require.Equal(t, "https://x.com/i/status/100", view.Root.URL)
		require.Equal(t, second, view.Parent.Post.UUID)
		require.Equal(t, "102", view.Next)
		page, err := repo.SourceThread.Read(ctx, third, view.Next, 1)
		require.NoError(t, err)
		require.Equal(t, "103", page.Posts[0].SourceID)
		require.Empty(t, page.Next)
		return nil
	}))
	root := threadPost(t, repo, "100", "100", "", "99", "")
	threadMedia(t, repo, root, "500", models.ArchiveScene, 32)
	late := syncSourceGallery(t, repo, root)
	require.False(t, late.Created)
	require.Equal(t, album.GalleryUUID, late.GalleryUUID)
	sourceGalleryMemberships(t, repo, *album.GalleryID, []int{41}, []int{31, 32})
	require.Empty(t, syncSourceGallery(t, repo, third).Added)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM source_post_threads"))
}

func TestThreadGalleryKeepsAuthorsAndManualExclusionsSeparate(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	root := threadPost(t, repo, "100", "100", "", "99", "")
	threadMedia(t, repo, root, "500", models.ArchiveImage, 41)
	other := threadPost(t, repo, "101", "100", "100", "88", "99")
	threadMedia(t, repo, other, "501", models.ArchiveScene, 32)
	require.Equal(t, "ineligible", syncSourceGallery(t, repo, other).Action)
	self := threadPost(t, repo, "102", "100", "100", "99", "99")
	selection := threadMedia(t, repo, self, "502", models.ArchiveImage, 41)
	album := syncSourceGallery(t, repo, self)
	require.Len(t, album.Added, 1, "the same media in two replies is one gallery member")
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: selection.Entries[0].Attachment.UUID,
		ExpectedAttachmentRevision: findAttachment(t, repo, selection.Entries[0].Attachment.UUID).Revision, State: "unlinked", Origin: "review"}))
	require.Empty(t, syncSourceGallery(t, repo, self).Removed, "root still selects the shared image")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Gallery.RemoveImages(ctx, *album.GalleryID, 41) }))
	require.Empty(t, syncSourceGallery(t, repo, root).Added, "do not re-add a manual exclusion")
	sourceGalleryMemberships(t, repo, *album.GalleryID, nil, nil)
}

func TestThreadGalleryRejectsStalePreviewAndPreservesDeletedGallery(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	root := threadPost(t, repo, "100", "100", "", "99", "")
	threadMedia(t, repo, root, "500", models.ArchiveImage, 41)
	self := threadPost(t, repo, "102", "100", "100", "99", "99")
	threadMedia(t, repo, self, "502", models.ArchiveScene, 31)
	preview := sourceGalleryPreview(t, repo, self)
	third := threadPost(t, repo, "103", "100", "102", "99", "99")
	threadMedia(t, repo, third, "503", models.ArchiveScene, 32)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.Sync(ctx, self, preview.Signature)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	album := syncSourceGallery(t, repo, self)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Gallery.Destroy(ctx, *album.GalleryID) }))
	later := threadPost(t, repo, "104", "100", "103", "99", "99")
	threadMedia(t, repo, later, "504", models.ArchiveScene, 32)
	require.Equal(t, "disabled", syncSourceGallery(t, repo, later).Action)
}

func TestThreadMigrationPreservesExistingAlbumsWithoutScanningCaptures(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := threadPost(t, repo, "100", "100", "", "99", "")
	selectAlbum(t, repo, post, models.SourceAttachmentManifestInput{Complete: true, Entries: []models.SourceAttachmentEntry{
		albumEntry(0, "native:twitter", "500", "image"), albumEntry(1, "native:twitter", "501", "image"),
	}})
	album := syncSourceGallery(t, repo, post)
	require.True(t, album.Created)
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	removeSourceThreadSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000104")
	require.NoError(t, err)
	tables := []string{"galleries", "source_posts", "source_captures", "source_attachment_entries", "post_gallery_links", "post_gallery_decisions"}
	before := map[string][][]any{}
	for _, table := range tables {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	for _, table := range tables {
		require.Equal(t, before[table], albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_threads"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestThreadGalleryLaterRootFromDifferentAuthorRequiresReview(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	first := threadPost(t, repo, "102", "100", "101", "88", "88")
	threadMedia(t, repo, first, "502", models.ArchiveImage, 41)
	second := threadPost(t, repo, "103", "100", "102", "88", "88")
	threadMedia(t, repo, second, "503", models.ArchiveScene, 31)
	album := syncSourceGallery(t, repo, second)
	threadPost(t, repo, "100", "100", "", "99", "")
	require.Equal(t, "review", sourceGalleryPreview(t, repo, second).Action)
	sourceGalleryMemberships(t, repo, *album.GalleryID, []int{41}, []int{31})
}
