package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPostTextCleanupRemovesStoredVersionsAndReplays(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	var before []*models.SourceCapture
	for i, text := range []string{"Old caption", "Another caption", "Saved caption"} {
		before = append(before, recordSourceTestCapture(t, repo, contentTestCapture(t, post.UUID, i*2, text)))
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	result, err := db.CompactPostText(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, 3, result.Captures)
	require.Equal(t, 1, result.Posts)
	require.Equal(t, 3, result.RevisionsRemoved)
	require.Greater(t, result.StoredBytesRemoved, int64(0))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_post_revisions"))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, old := range before {
			capture, err := repo.SourceEvidence.FindCapture(ctx, old.UUID)
			require.NoError(t, err)
			require.Equal(t, old.CapturedAt, capture.CapturedAt)
			require.Equal(t, old.Payload.Refs, capture.Payload.Refs)
			require.Equal(t, old.Payload.Profiles, capture.Payload.Profiles)
			require.Equal(t, "Saved caption", *capture.Metadata.Title)
			require.Equal(t, "Saved caption", *capture.Metadata.OriginalText)
			require.Contains(t, string(capture.Payload.Shared), "Saved caption")
			require.NotContains(t, string(capture.Payload.Shared), "Old caption")
			require.NotContains(t, string(capture.Payload.Shared), "Another caption")
		}
		return nil
	}))
	// The next scrape keeps the saved text and reuses the compacted storage.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		capture, err := repo.SourceEvidence.RecordContentCapture(ctx, contentTestCapture(t, post.UUID, 8, "A subsequent edit"))
		require.NoError(t, err)
		require.Equal(t, before[2].UUID, capture.UUID)
		return nil
	}))
	second, err := db.CompactPostText(t.Context(), nil)
	require.NoError(t, err)
	require.Zero(t, second.Captures)
	require.Zero(t, second.RevisionsRemoved)
	require.NoError(t, db.AuditForTesting(db.DatabasePath()))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestPostTextCleanupPreservesOriginalAndAttachmentEvidence(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	// The source text is separate from a translated NFO display value.
	first := contentTestCapture(t, post.UUID, 0, "Texto original")
	first.Origin = "gallery-dl-enrichment"
	original := recordSourceTestCapture(t, repo, first)
	second := contentTestCapture(t, post.UUID, 1, "Texto original")
	attachment := recordSourceTestCapture(t, repo, second)
	legacy := contentTestCapture(t, post.UUID, 6, "Translated NFO caption")
	legacy.Origin, legacy.RetentionPolicy = "legacy-nfo", "imported-metadata-v1"
	payload, err := archive.PrepareRetainedCapture(legacy.Origin, legacy.Platform, []byte(`{}`))
	require.NoError(t, err)
	legacy.Payload = *payload
	old := recordSourceTestCapture(t, repo, legacy)
	result, err := db.CompactPostText(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Posts)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		content, err := repo.SourceEvidence.PostContent(ctx, post.UUID)
		require.NoError(t, err)
		require.Equal(t, "Texto original", *content.OriginalText)
		for _, previous := range []*models.SourceCapture{original, attachment, old} {
			capture, err := repo.SourceEvidence.FindCapture(ctx, previous.UUID)
			require.NoError(t, err)
			require.Equal(t, "Texto original", *capture.Metadata.OriginalText)
			before, err := archive.RestoreCapture(previous.Payload)
			require.NoError(t, err)
			before, err = archive.RetainPostContent(before)
			require.NoError(t, err)
			retained, err := archive.PrepareRetainedCapture(previous.Origin, previous.Platform, before)
			require.NoError(t, err)
			require.JSONEq(t, string(retained.Patch), string(capture.Payload.Patch))
		}
		return nil
	}))
	require.NoError(t, db.AuditForTesting(db.DatabasePath()))
}

func TestPostTextCleanupRetainsSparseContentAndSeparatePosts(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	input := contentTestCapture(t, post.UUID, 0, "Saved caption")
	recordSourceTestCapture(t, repo, input)
	input.UUID, input.Origin = uuid.NewString(), "derived-twitter-id"
	input.CapturedAt = input.CapturedAt.Add(time.Hour)
	date, basis := "2026-10-01T00:00:00Z", "source"
	input.Metadata = models.SourcePostMetadata{PublishedAt: &date, DateBasis: &basis}
	payload, err := archive.PrepareRetainedCapture(input.Origin, input.Platform, []byte(`{}`))
	require.NoError(t, err)
	input.Payload = *payload
	recordSourceTestCapture(t, repo, input)
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "another-post"}, "")
	otherCapture := recordSourceTestCapture(t, repo, contentTestCapture(t, other.UUID, 0, "A separate post"))
	_, err = db.CompactPostText(t.Context(), nil)
	require.NoError(t, err)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		content, err := repo.SourceEvidence.PostContent(ctx, post.UUID)
		require.NoError(t, err)
		require.Equal(t, "Saved caption", *content.Title)
		require.Equal(t, date, *content.PublishedAt)
		capture, err := repo.SourceEvidence.FindCapture(ctx, otherCapture.UUID)
		require.NoError(t, err)
		require.Equal(t, otherCapture, capture)
		return nil
	}))
}

func TestPostTextCleanupRollsBackOnCancellation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	old := recordSourceTestCapture(t, repo, contentTestCapture(t, post.UUID, 0, "Old"))
	recordSourceTestCapture(t, repo, contentTestCapture(t, post.UUID, 2, "Saved"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := db.CompactPostText(ctx, func(string, int) { cancel() })
	require.Error(t, err)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		capture, err := repo.SourceEvidence.FindCapture(ctx, old.UUID)
		require.NoError(t, err)
		require.Equal(t, old, capture)
		return nil
	}))
	// The rolled-back transaction must restore its guard and temporary state.
	_, err = db.CompactPostText(t.Context(), nil)
	require.NoError(t, err)
}

func TestPostTextCleanupPreservesInputsStillUsedByTranslations(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	collection := translationPolicyCollection(t, repo)
	putTranslationPolicy(t, repo, collection, 0, translationPolicyDefinition())
	old := recordSourceTestCapture(t, repo, contentTestCapture(t, post.UUID, 0, "Original translation input"))
	scope := models.CollectionCapture{CaptureUUID: old.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.SourceCollection.RecordCapture(ctx, scope) }))
	scheduleTranslationCapture(t, repo, scope, time.Now().UTC())
	recordSourceTestCapture(t, repo, contentTestCapture(t, post.UUID, 2, "Saved caption"))
	result, err := db.CompactPostText(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.ProtectedCaptures)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		capture, err := repo.SourceEvidence.FindCapture(ctx, old.UUID)
		require.NoError(t, err)
		require.Equal(t, old, capture)
		return nil
	}))
	require.NoError(t, db.AuditForTesting(db.DatabasePath()))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}
