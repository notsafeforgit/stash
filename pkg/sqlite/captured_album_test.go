package sqlite_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedAlbumsPopulateNativeGalleriesAfterMediaAssociations(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	for _, tc := range []struct{ platform, raw string }{
		{"reddit", `{"category":"reddit","id":"reddit-album","gallery_data":{"items":[{"media_id":"image"},{"media_id":"video"}]},"media_metadata":{"image":{"e":"Image"},"video":{"e":"RedditVideo"}}}`},
		{"twitter", `{"category":"twitter","tweet_id":"twitter-album","extended_entities":{"media":[{"id_str":"image","type":"photo"},{"id_str":"video","type":"video"}]}}`},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			album, err := archive.ExtractCapturedAlbum([]byte(tc.raw))
			require.NoError(t, err)
			post := sourceTestPost(t, repo, album.Post, "")
			retained, err := archive.RetainSourcePayload([]byte(tc.raw))
			require.NoError(t, err)
			payload, err := archive.PrepareRetainedCapture("gallery-dl", tc.platform, retained)
			require.NoError(t, err)
			capture := recordSourceTestCapture(t, repo, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
				Origin: "gallery-dl", Platform: tc.platform, CapturedAt: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
			album.Manifest.CaptureUUID = capture.UUID
			recordAttachmentManifest(t, repo, album.Manifest)
			selection := applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: selectionPost(t, repo, post.UUID).Revision,
				CaptureUUID: capture.UUID, Mode: "automatic", Origin: "ingest"})
			require.True(t, selection.Complete)
			chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
			preview := sourceGalleryPreview(t, repo, post.UUID)
			require.Equal(t, "create", preview.Action)
			require.Len(t, preview.Entries, 2)
			require.Nil(t, preview.Entries[1].MediaUUID, "a source list cannot fabricate a downloaded scene")
			first := syncSourceGallery(t, repo, post.UUID)
			require.True(t, first.Created)
			sourceGalleryMemberships(t, repo, *first.GalleryID, []int{41}, nil)
			chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
			second := syncSourceGallery(t, repo, post.UUID)
			require.False(t, second.Created)
			require.Equal(t, first.GalleryUUID, second.GalleryUUID)
			sourceGalleryMemberships(t, repo, *second.GalleryID, []int{41}, []int{31})
			recordAttachmentManifest(t, repo, album.Manifest)
			replay := syncSourceGallery(t, repo, post.UUID)
			require.False(t, replay.Created)
			require.Equal(t, first.GalleryUUID, replay.GalleryUUID)
			require.Empty(t, replay.Added)
			require.Empty(t, replay.Removed)
		})
	}
}
