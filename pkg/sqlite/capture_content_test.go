package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func contentTestCapture(t *testing.T, post string, index int, text string) models.SourceCaptureInput {
	t.Helper()
	user := ""
	if index%2 == 0 {
		user = `,"user":{"id":"author1","name":"author"}`
	}
	raw := fmt.Sprintf(`{"category":"reddit","id":"p","author":"author","title":%q,"score":%d,"search_tags":"author:author sort:%d","source_extractor_url":"https://www.reddit.com/user/author/?sort=%d","subcategory":"user-submitted","filename":"image%d","num":%d,"extension":"jpg","_url":"https://i.redd.it/image%d.jpg"%s}`, text, index, index, index, index%2, index%2+1, index%2, user)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(raw))
	require.NoError(t, err)
	return models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post, Origin: "gallery-dl", Platform: "reddit", CapturedAt: time.Date(2026, 10, 1, 1, index, 0, 0, time.UTC), RetentionPolicy: "legacy-retained-v1", Metadata: models.SourcePostMetadata{Title: &text, OriginalText: &text}, Payload: *payload}
}

func TestCaptureContentCleanupPreservesIDsAndActualEdits(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	var before []*models.SourceCapture
	for i := 0; i < 8; i++ {
		before = append(before, recordSourceTestCapture(t, repo, contentTestCapture(t, post.UUID, i, "Original post")))
	}
	before = append(before, recordSourceTestCapture(t, repo, contentTestCapture(t, post.UUID, 8, "Edited post")))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	result, err := db.CompactCaptureContent(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, 9, result.Captures)
	require.Equal(t, 1, result.Posts)
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_post_revisions WHERE post_uuid='"+post.UUID+"'"))
	require.EqualValues(t, 9, queryUint(t, raw, "SELECT count(*) FROM source_captures WHERE post_uuid='"+post.UUID+"'"))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, old := range before {
			got, err := repo.SourceEvidence.FindCapture(ctx, old.UUID)
			require.NoError(t, err)
			require.Equal(t, old.Metadata, got.Metadata)
			require.Equal(t, old.Payload.Profiles, got.Payload.Profiles)
			require.Equal(t, old.CapturedAt, got.CapturedAt)
			require.Equal(t, archive.PostContentRetentionVersion, got.RetentionPolicy)
			require.NotContains(t, string(got.Payload.Shared), "score")
		}
		history, err := repo.SourceEvidence.CaptureHistory(ctx, post.UUID, "", 1)
		require.NoError(t, err)
		require.Len(t, history, 1)
		next, err := repo.SourceEvidence.CaptureHistory(ctx, post.UUID, history[0].UUID, 1)
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.Equal(t, 9, history[0].Count+next[0].Count)
		return nil
	}))
	second, err := db.CompactCaptureContent(t.Context(), nil)
	require.NoError(t, err)
	require.Zero(t, second.Captures)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestCaptureContentRepeatSharesCaptureAndCountsSightings(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	var ids []string
	for i := 0; i < 6; i += 2 {
		input := contentTestCapture(t, post.UUID, i, "Same post")
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			c, err := repo.SourceEvidence.RecordContentCapture(ctx, input)
			require.NoError(t, err)
			ids = append(ids, c.UUID)
			return nil
		}))
	}
	require.Equal(t, ids[0], ids[1])
	require.Equal(t, ids[0], ids[2])
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_captures WHERE post_uuid='"+post.UUID+"'"))
	// A failed encompassing event transaction must not inflate the count.
	require.Error(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordContentCapture(ctx, contentTestCapture(t, post.UUID, 6, "Same post"))
		require.NoError(t, err)
		return errors.New("rollback")
	}))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := repo.SourceEvidence.CaptureHistory(ctx, post.UUID, "", 25)
		require.NoError(t, err)
		require.Len(t, history, 1)
		require.Equal(t, 3, history[0].Count)
		require.Equal(t, 0, history[0].UnknownCount)
		require.Equal(t, time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC), *history[0].FirstSeen)
		require.Equal(t, time.Date(2026, 10, 1, 1, 4, 0, 0, time.UTC), *history[0].LastSeen)
		return nil
	}))
	// Different media and real edits continue to create distinct retained records.
	for _, i := range []int{1, 8} {
		input := contentTestCapture(t, post.UUID, i, "Same post")
		if i == 8 {
			title := "Edited"
			input.Metadata.Title = &title
		}
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceEvidence.RecordContentCapture(ctx, input)
			return err
		}))
	}
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM source_captures WHERE post_uuid='"+post.UUID+"'"))
}

func TestCaptureContentUnknownObservationStaysUnknown(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	input := contentTestCapture(t, post.UUID, 0, "Old post")
	now := time.Now().UTC()
	input.CapturedAt = time.Time{}
	input.RecordedAt = &now
	recordSourceTestCapture(t, repo, input)
	_, err := db.CompactCaptureContent(t.Context(), nil)
	require.NoError(t, err)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := repo.SourceEvidence.CaptureHistory(ctx, post.UUID, "", 25)
		require.NoError(t, err)
		require.Len(t, history, 1)
		require.Equal(t, 1, history[0].UnknownCount)
		require.Nil(t, history[0].FirstSeen)
		require.Nil(t, history[0].LastSeen)
		encoded, err := json.Marshal(history)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "0001-")
		return nil
	}))
}
