package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestCaptureRecordingTimePreservesUnknownObservationAndIndexedPaging(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post-one"}, "")
	known := sourceTestCapture(t, post.UUID, 1, "Profile")
	first := recordSourceTestCapture(t, repo, known)
	stamp := known.CapturedAt.Add(time.Hour)
	unknown := sourceTestCapture(t, post.UUID, 2, "Profile")
	unknown.CapturedAt, unknown.RecordedAt = time.Time{}, &stamp
	second := recordSourceTestCapture(t, repo, unknown)
	require.True(t, second.CapturedAt.IsZero())
	require.True(t, second.RecordedAt.Equal(stamp))
	require.Equal(t, second, recordSourceTestCapture(t, repo, unknown))
	encoded, err := json.Marshal(second)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(encoded, &view))
	require.Nil(t, view["CapturedAt"])
	require.Equal(t, stamp.UTC().Format(time.RFC3339Nano), view["RecordedAt"])
	require.NotContains(t, string(encoded), "0001-01-01")
	last := sourceTestCapture(t, post.UUID, 3, "Profile")
	last.CapturedAt = stamp.Add(time.Hour)
	third := recordSourceTestCapture(t, repo, last)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var cursor *models.SourceCaptureCursor
		for _, expected := range []*models.SourceCapture{first, second, third} {
			page, err := repo.SourceEvidence.Captures(ctx, post.UUID, cursor, 1)
			require.NoError(t, err)
			require.Len(t, page, 1)
			require.Equal(t, expected.UUID, page[0].UUID)
			require.Equal(t, expected.RecordedAt, page[0].RecordedAt)
			require.Equal(t, expected.CapturedAt, page[0].CapturedAt)
			require.Nil(t, page[0].Payload)
			cursor = &models.SourceCaptureCursor{CapturedAt: page[0].CapturedAt, RecordedAt: page[0].RecordedAt, UUID: page[0].UUID}
		}
		page, err := repo.SourceEvidence.Captures(ctx, post.UUID, cursor, 1)
		require.NoError(t, err)
		require.Empty(t, page)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_captures WHERE captured_at IS NULL AND recorded_at IS NOT NULL"))
	rows, err := raw.Query(`EXPLAIN QUERY PLAN SELECT uuid FROM source_captures
 WHERE post_uuid=? AND (coalesce(captured_at,recorded_at),uuid)>(?,?)
 ORDER BY coalesce(captured_at,recorded_at),uuid LIMIT 1`, post.UUID, "", "")
	require.NoError(t, err)
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plans = append(plans, detail)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Contains(t, strings.Join(plans, "\n"), "source_captures_order")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	backup := filepath.Join(t.TempDir(), "recording-times.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		got, err := repo.SourceEvidence.FindCapture(ctx, second.UUID)
		require.Equal(t, second, got)
		return err
	}))
	anonymous, err := sqlite.NewAnonymiser(db, filepath.Join(t.TempDir(), "anonymous.sqlite"))
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
}

func TestCaptureRecordingTimeRejectsAmbiguityAndChangedReplay(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post-one"}, "")
	input := sourceTestCapture(t, post.UUID, 1, "Profile")
	stamp := input.CapturedAt
	for _, test := range []struct {
		observed time.Time
		recorded *time.Time
	}{
		{time.Time{}, nil}, {stamp, &stamp}, {time.Time{}, new(time.Time)},
	} {
		bad := input
		bad.CapturedAt, bad.RecordedAt = test.observed, test.recorded
		require.Error(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceEvidence.RecordCapture(ctx, bad)
			return err
		}))
	}
	input.CapturedAt, input.RecordedAt = time.Time{}, &stamp
	recordSourceTestCapture(t, repo, input)
	for _, change := range []func(*models.SourceCaptureInput){
		func(c *models.SourceCaptureInput) { later := stamp.Add(time.Nanosecond); c.RecordedAt = &later },
		func(c *models.SourceCaptureInput) { c.CapturedAt = stamp; c.RecordedAt = nil },
	} {
		bad := input
		change(&bad)
		err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceEvidence.RecordCapture(ctx, bad)
			return err
		})
		require.ErrorIs(t, err, models.ErrSourceCaptureReplay)
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	for _, observed := range []string{"NULL", "recorded_at"} {
		recorded := "NULL"
		if observed != "NULL" {
			recorded = "recorded_at"
		}
		_, err := raw.Exec(`INSERT INTO source_captures(uuid,post_uuid,revision_uuid,origin,platform,captured_at,extractor_version,retention_policy,patch_digest,signature,recorded_at)
 SELECT ?,post_uuid,revision_uuid,origin,platform,`+observed+`,extractor_version,retention_policy,patch_digest,signature,`+recorded+` FROM source_captures LIMIT 1`, uuid.NewString())
		require.ErrorContains(t, err, "CHECK constraint")
	}
}

func TestCaptureRecordingTimePublisherReviewDoesNotInventIdentifierObservation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post-one"}, "")
	input := sourceTestCapture(t, post.UUID, 1, "Profile")
	stamp := input.CapturedAt
	input.CapturedAt, input.RecordedAt = time.Time{}, &stamp
	capture := recordSourceTestCapture(t, repo, input)
	preview := publisherPreview(t, repo, capture.UUID, "")
	require.Equal(t, "review", preview.Action)
	require.Contains(t, preview.Conflicts, "observation_time_unrecorded")
	_, err := applyPublisher(repo, publisherInput(preview, "automatic"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	account := createSourceAccount(t, repo, "native:twitter")
	preview = publisherPreview(t, repo, capture.UUID, account.UUID)
	decision, err := applyPublisher(repo, publisherInput(preview, "link"))
	require.NoError(t, err)
	require.Equal(t, account.UUID, *decision.AccountUUID)
	require.Equal(t, "preserve", publisherPreview(t, repo, capture.UUID, "").Action)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_account_identifiers"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM capture_publisher_claims"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestCaptureRecordingTimeStartupRejectsAlteredRecordingWithoutWriting(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: uuid.NewString()}, "")
	input := sourceTestCapture(t, post.UUID, 1, "Profile")
	stamp := input.CapturedAt
	input.CapturedAt, input.RecordedAt = time.Time{}, &stamp
	recordSourceTestCapture(t, repo, input)
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	_, err := raw.Exec(`DROP TRIGGER source_capture_immutable; UPDATE source_captures SET recorded_at='2026-10-03T00:00:00.000000001Z'`)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TRIGGER source_capture_immutable BEFORE UPDATE ON source_captures BEGIN SELECT RAISE(ABORT,'immutable'); END`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Error(t, db.Open(db.DatabasePath()))
	after, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
