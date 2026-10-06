package sqlite_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourcePostReviewPagesCandidatesChoicesAndMergedMedia(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	media := archiveFind(t, repo, models.ArchiveScene, 31)
	other := archiveFind(t, repo, models.ArchiveScene, 32)
	image := archiveFind(t, repo, models.ArchiveImage, 41)
	posts := []string{}
	for i := range 5 {
		post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: fmt.Sprintf("review-%d", i)}, "")
		posts = append(posts, post.UUID)
		if i < 3 {
			for range 3 {
				recordMediaEvidence(t, repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: media.UUID, Basis: "legacy"})
			}
		}
		if i > 1 {
			_, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, media.UUID, "linked"))
			require.NoError(t, err)
		}
	}
	// A direct rejected choice remains discoverable and reviewable without file
	// evidence. A redirect cannot hide a contradictory choice after a merge.
	_, err := applyPostMedia(repo, postMediaInput(t, repo, posts[4], other.UUID, "unlinked"))
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Scene.RedirectMergedIdentities(ctx, []int{32}, 31); err != nil {
			return err
		}
		return repo.Scene.Destroy(ctx, 32)
	}))
	media = archiveFind(t, repo, models.ArchiveScene, 31)
	attachmentSQL(t, db, "UPDATE source_posts SET state='forgotten' WHERE uuid=?", posts[0])
	slices.Sort(posts)
	var found []string
	after := ""
	for {
		var page []models.SourcePostMediaReview
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			page, err = repo.SourcePostMedia.PostsForMedia(ctx, other.UUID, after, 2)
			return err
		}))
		for _, item := range page {
			found = append(found, item.Association.PostUUID)
			require.Equal(t, media.UUID, item.Association.MediaUUID)
			require.Nil(t, item.LatestCapture)
		}
		if len(page) < 2 {
			break
		}
		after = page[len(page)-1].Association.PostUUID
	}
	require.Equal(t, posts, found, "duplicates and overlapping discovery paths appear once")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := repo.SourcePostMedia.PostsForMedia(ctx, image.UUID, "", 25)
		require.NoError(t, err)
		require.Empty(t, page, "another media kind cannot inherit candidates")
		for _, after := range []string{"bad", uuid.Nil.String()} {
			_, err = repo.SourcePostMedia.PostsForMedia(ctx, media.UUID, after, 25)
			require.ErrorIs(t, err, models.ErrSourcePostMediaInvalid)
		}
		_, err = repo.SourcePostMedia.PostsForMedia(ctx, media.UUID, "", 101)
		require.ErrorIs(t, err, models.ErrSourcePostMediaInvalid)
		return nil
	}))
}

func TestSourcePostReviewSeparatesAttachmentLinksAndCompactMetadata(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	media := archiveFind(t, repo, models.ArchiveImage, 41)
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
		ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: media.UUID,
		ExpectedMediaRevision: media.Revision, Origin: "review"}))
	title := strings.Repeat("花", 600)
	input := sourceTestCapture(t, capture.PostUUID, 3, "profile")
	input.Metadata.Title = &title
	input.CapturedAt = time.Time{}
	clock := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	input.RecordedAt = &clock
	latest := recordSourceTestCapture(t, repo, input)
	for i := range 4 {
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{
				SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: capture.PostUUID,
					Origin: "review", Basis: "review", ObservedAt: clock, Details: []byte(`{}`)},
				URL: fmt.Sprintf("https://example.test/posts/%d", i)})
			return err
		}))
	}
	read := func() models.SourcePostMediaReview {
		var rows []models.SourcePostMediaReview
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			rows, err = repo.SourcePostMedia.PostsForMedia(ctx, media.UUID, "", 1)
			return err
		}))
		require.Len(t, rows, 1)
		return rows[0]
	}
	item := read()
	require.Equal(t, "undecided", item.Association.State)
	require.Equal(t, 1, item.LinkedAttachments)
	require.False(t, item.HasRetainedEvidence)
	require.Len(t, item.URLs, 3)
	require.True(t, item.MoreURLs)
	require.Equal(t, latest.UUID, item.LatestCapture.UUID)
	require.Nil(t, item.LatestCapture.CapturedAt)
	require.Equal(t, clock, *item.LatestCapture.RecordedAt)
	require.Equal(t, strings.Repeat("花", 512), *item.LatestCapture.Title)
	require.True(t, item.LatestCapture.TitleTruncated)
	_, err := applyPostMedia(repo, postMediaInput(t, repo, capture.PostUUID, media.UUID, "unlinked"))
	require.NoError(t, err)
	item = read()
	require.True(t, item.Association.Suppressed())
	require.Equal(t, 1, item.LinkedAttachments, "suppressed attachment evidence remains available")
	_, err = applyPostMedia(repo, postMediaInput(t, repo, capture.PostUUID, media.UUID, "undecided"))
	require.NoError(t, err)
	require.False(t, read().Association.Suppressed())
}
