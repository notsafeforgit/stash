package sqlite_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourcePostBrowserPreservesAmbiguousURLsAndQualifiedIdentifiers(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	posts := make([]string, 5)
	clock := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range posts {
		key := models.SourcePostIdentifier{Namespace: "native:twitter", Value: fmt.Sprint(i)}
		if i == 4 {
			key.Namespace, key.Value = "native:reddit", "0"
		}
		post := sourceTestPost(t, repo, key, "")
		posts[i] = post.UUID
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{
				SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post.UUID,
					Origin: "migration", Basis: "retained", ObservedAt: clock, Details: []byte(`{}`)},
				URL: "https://example.test/shared-post"})
			return err
		}))
	}
	read := func(filter models.SourcePostFilter) []models.SourcePostSummary {
		var rows []models.SourcePostSummary
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			rows, err = repo.SourceEvidence.BrowsePosts(ctx, filter)
			return err
		}))
		return rows
	}
	expected := slices.Clone(posts)
	slices.Sort(expected)
	for _, url := range []string{"", "https://example.test/shared-post"} {
		var actual []string
		after := ""
		for {
			page := read(models.SourcePostFilter{URL: url, After: after, Limit: 2})
			for _, row := range page {
				actual = append(actual, row.UUID)
				require.Nil(t, row.LatestCapture)
			}
			if len(page) < 2 {
				break
			}
			after = page[len(page)-1].UUID
		}
		require.Equal(t, expected, actual, "one shared URL does not collapse independent native posts")
	}
	for i, namespace := range []string{"native:twitter", "native:reddit"} {
		page := read(models.SourcePostFilter{Identifier: &models.SourcePostIdentifier{Namespace: namespace, Value: "0"}, Limit: 25})
		require.Len(t, page, 1)
		require.Equal(t, posts[i*4], page[0].UUID)
	}
	page := read(models.SourcePostFilter{PostUUID: posts[0], Limit: 1})
	require.Len(t, page, 1)
	require.Equal(t, posts[0], page[0].UUID)
	require.Empty(t, read(models.SourcePostFilter{PostUUID: posts[0], After: posts[0], Limit: 1}))
	require.Empty(t, read(models.SourcePostFilter{URL: "https://example.test/unknown", Limit: 25}))
	require.Empty(t, read(models.SourcePostFilter{PostUUID: uuid.NewString(), Limit: 25}))
}

func TestSourcePostBrowserUsesCompactSharedMetadataAndRetainsUnknownObservationTimes(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "compact-browser"}, "")
	input := sourceTestCapture(t, post.UUID, 1, "profile body that must not be returned")
	title := strings.Repeat("界", 700)
	input.Metadata.Title = &title
	input.CapturedAt = time.Time{}
	clock := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	input.RecordedAt = &clock
	capture := recordSourceTestCapture(t, repo, input)
	for i := range 4 {
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			current, err := repo.SourceEvidence.FindPost(ctx, post.UUID)
			if err != nil {
				return err
			}
			if err := repo.SourceEvidence.AddPostIdentifier(ctx, post.UUID,
				models.SourcePostIdentifier{Namespace: "native:reddit", Value: fmt.Sprint(i)}, current.Revision); err != nil {
				return err
			}
			_, err = repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{
				SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post.UUID,
					Origin: "migration", Basis: "retained", ObservedAt: clock, Details: []byte(`{}`)},
				URL: fmt.Sprintf("https://example.test/retained/%d", i)})
			return err
		}))
	}
	attachmentSQL(t, db, "UPDATE source_posts SET state='forgotten' WHERE uuid=?", post.UUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		summary, err := repo.SourceEvidence.PostSummary(ctx, post.UUID)
		require.NoError(t, err)
		require.Equal(t, "forgotten", summary.State)
		require.Len(t, summary.Identifiers, 3)
		require.True(t, summary.MoreIdentifiers)
		require.Len(t, summary.URLs, 3)
		require.True(t, summary.MoreURLs)
		require.Equal(t, capture.UUID, summary.LatestCapture.UUID)
		require.Nil(t, summary.LatestCapture.CapturedAt)
		require.Equal(t, clock, *summary.LatestCapture.RecordedAt)
		require.Equal(t, strings.Repeat("界", 512), *summary.LatestCapture.Title)
		require.True(t, summary.LatestCapture.TitleTruncated)
		encoded, err := json.Marshal(summary)
		require.NoError(t, err)
		for _, excluded := range []string{"payload", "profile body", "settings", "Shared post text"} {
			require.NotContains(t, string(encoded), excluded)
		}
		missing, err := repo.SourceEvidence.PostSummary(ctx, uuid.NewString())
		require.NoError(t, err)
		require.Nil(t, missing)
		return nil
	}))
}

func TestSourcePostBrowserRejectsInvalidScopesAndCursors(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	for _, filter := range []models.SourcePostFilter{
		{}, {Limit: 101}, {Limit: 25, After: "bad"}, {Limit: 25, After: uuid.Nil.String()},
		{Limit: 25, PostUUID: "bad"}, {Limit: 25, URL: "javascript:alert(1)"},
		{Limit: 25, URL: "https://user:secret@example.test/post"}, {Limit: 25, URL: " https://example.test/post"},
		{Limit: 25, URL: "https://example.test/post\n"}, {Limit: 25, URL: "https://"},
		{Limit: 25, Identifier: &models.SourcePostIdentifier{Value: "123"}},
		{Limit: 25, Identifier: &models.SourcePostIdentifier{Namespace: "native:twitter"}},
		{Limit: 25, PostUUID: uuid.NewString(), URL: "https://example.test/post"},
	} {
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceEvidence.BrowsePosts(ctx, filter)
			require.ErrorIs(t, err, models.ErrSourcePostBrowseInvalid)
			return nil
		}))
	}
}

func TestSourcePostBrowserMediaKeepsMergedChoicesDeletedIdentitiesAndCandidates(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	post := capture.PostUUID
	image := archiveFind(t, repo, models.ArchiveImage, 41)
	first := archiveFind(t, repo, models.ArchiveScene, 31)
	second := archiveFind(t, repo, models.ArchiveScene, 32)
	for _, media := range []*models.ArchiveEntity{first, second} {
		for range 3 {
			recordMediaEvidence(t, repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post, MediaUUID: media.UUID, Basis: "legacy"})
		}
	}
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
		ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: image.UUID,
		ExpectedMediaRevision: image.Revision, Origin: "review"}))
	for _, choice := range []struct{ id, state string }{{first.UUID, "linked"}, {second.UUID, "unlinked"}, {image.UUID, "unlinked"}} {
		_, err := applyPostMedia(repo, postMediaInput(t, repo, post, choice.id, choice.state))
		require.NoError(t, err)
	}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Scene.RedirectMergedIdentities(ctx, []int{32}, 31); err != nil {
			return err
		}
		return repo.Scene.Destroy(ctx, 32)
	}))
	attachmentSQL(t, db, "UPDATE scenes SET title=? WHERE id=31", strings.Repeat("花", 600))
	read := func(after string, limit int) []models.SourcePostMediaItem {
		var rows []models.SourcePostMediaItem
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			rows, err = repo.SourcePostMedia.MediaForPost(ctx, post, after, limit)
			return err
		}))
		return rows
	}
	rows := read("", 25)
	require.Len(t, rows, 2, "merged media and repeated evidence appear once")
	for _, row := range rows {
		if row.Media.Kind == models.ArchiveScene {
			require.Equal(t, first.UUID, row.Media.UUID)
			require.Equal(t, "conflict", row.Association.State)
			require.True(t, row.HasRetainedEvidence)
			require.Len(t, row.Association.Decisions, 2)
			require.True(t, row.Media.TitleTruncated)
			require.Equal(t, strings.Repeat("花", 512), row.Media.Title)
		} else {
			require.Equal(t, image.UUID, row.Media.UUID)
			require.Equal(t, "unlinked", row.Association.State)
			require.Equal(t, 1, row.LinkedAttachments, "explicit unlink suppresses, but does not destroy attachment evidence")
		}
	}
	page := read("", 1)
	require.Equal(t, rows[:1], page)
	require.Equal(t, rows[1:], read(page[0].Media.UUID, 1))
	require.Empty(t, read(rows[1].Media.UUID, 1))
	encoded, err := json.Marshal(rows)
	require.NoError(t, err)
	for _, excluded := range []string{"latest_capture", "payload", "settings", "urls"} {
		require.NotContains(t, string(encoded), excluded, "post metadata is fetched once, separately")
	}
	// Deletion retains evidence with no local ID to accidentally reopen or reuse.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Scene.Destroy(ctx, 31) }))
	for _, row := range read("", 25) {
		if row.Media.UUID == first.UUID {
			require.Equal(t, models.ArchiveEntityDeleted, row.Media.State)
			require.Nil(t, row.Media.LocalID)
			require.Empty(t, row.Media.Title)
		}
	}
	for _, args := range []struct {
		post, after string
		limit       int
	}{{"bad", "", 25}, {post, "bad", 25}, {post, "", 0}, {post, "", 101}} {
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourcePostMedia.MediaForPost(ctx, args.post, args.after, args.limit)
			require.ErrorIs(t, err, models.ErrSourcePostMediaInvalid)
			return nil
		}))
	}
}
