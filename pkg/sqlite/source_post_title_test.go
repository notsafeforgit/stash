package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourcePostCaptionTitlesReachAlbumsAndEveryPostSummary(t *testing.T) {
	for _, tc := range []struct{ platform, title, text, want string }{
		{"twitter", "", "A short first line\nThe full description remains here.", "A short first line"},
		{"twitter", "1575550205214134278_1", "A sentence. More text follows.", "A sentence."},
		{"twitter", "", "", "Twitter post 1575550205214134278"},
		{"fansly", "", "A caption\nMore details", "A caption"},
	} {
		t.Run(tc.platform+tc.title+tc.want, func(t *testing.T) {
			_, repo := archiveTestDatabase(t)
			post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:" + tc.platform, Value: "1575550205214134278"}, "")
			input := sourceTestCapture(t, post.UUID, 1, "profile")
			input.Platform, input.Metadata.Title, input.Metadata.OriginalText = tc.platform, nil, &tc.text
			if tc.title != "" {
				input.Metadata.Title = &tc.title
			}
			capture := recordSourceTestCapture(t, repo, input)
			recordAttachmentManifest(t, repo, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, DeclaredAlbum: true, Complete: true,
				Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "image"), sourceAttachmentEntry(1, "second")}})
			selection := applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: selectionPost(t, repo, post.UUID).Revision,
				CaptureUUID: capture.UUID, Mode: "automatic", Origin: "ingest"})
			chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
			album := syncSourceGallery(t, repo, post.UUID)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				gallery, err := repo.Gallery.Find(ctx, *album.GalleryID)
				require.NoError(t, err)
				require.Equal(t, tc.want, gallery.Title)
				require.Equal(t, tc.text, gallery.Details)
				summary, err := repo.SourceEvidence.PostSummary(ctx, post.UUID)
				require.NoError(t, err)
				require.Equal(t, tc.want, *summary.LatestCapture.Title)
				posts, err := repo.SourceEvidence.BrowsePosts(ctx, models.SourcePostFilter{Limit: 25})
				require.NoError(t, err)
				require.Equal(t, tc.want, *posts[0].LatestCapture.Title)
				saved, err := repo.SourceEvidence.FindCapture(ctx, capture.UUID)
				require.NoError(t, err)
				require.Equal(t, input.Metadata.Title, saved.Metadata.Title)
				require.Equal(t, tc.text, *saved.Metadata.OriginalText)
				return nil
			}))
			if tc.text != "" {
				state := metadataState(t, repo, album.GalleryUUID, "title")
				require.Equal(t, "source", state.Origin)
				require.Equal(t, capture.UUID, *state.Decision.CaptureUUID)
			}
			// Sync never overwrites a title the user intentionally replaces.
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				partial := models.NewGalleryPartial()
				partial.Title = models.NewOptionalString("My own album title")
				_, err := repo.Gallery.UpdatePartial(ctx, *album.GalleryID, partial)
				return err
			}))
			syncSourceGallery(t, repo, post.UUID)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				gallery, err := repo.Gallery.Find(ctx, *album.GalleryID)
				require.NoError(t, err)
				require.Equal(t, "My own album title", gallery.Title)
				return nil
			}))
		})
	}
}

func TestSourcePostCaptionSummaryStaysBounded(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "123"}, "")
	input := sourceTestCapture(t, post.UUID, 1, "profile")
	text := strings.Repeat("界", 10000)
	input.Metadata.Title, input.Metadata.OriginalText = nil, &text
	recordSourceTestCapture(t, repo, input)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		summary, err := repo.SourceEvidence.PostSummary(ctx, post.UUID)
		require.NoError(t, err)
		require.Equal(t, strings.Repeat("界", 200), *summary.LatestCapture.Title)
		require.True(t, summary.LatestCapture.TitleTruncated)
		return nil
	}))
}
