package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestAttachmentDownloadTransfersKeepOneStableEntryAcrossReversedReports(t *testing.T) {
	f, event, input := downloadSQLFixture(t)
	post := func(value ingest.AttachmentDownloadEvent) {
		t.Helper()
		body, err := json.Marshal(value)
		require.NoError(t, err)
		_, err = f.service.AttachmentDownload(t.Context(), f.token, body, ingest.Digest(body))
		require.NoError(t, err)
	}
	read := func(before int64, limit int, now time.Time) *models.AttachmentDownloadPage {
		t.Helper()
		var page *models.AttachmentDownloadPage
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			page, err = f.repo.SourceAttachment.DownloadTransfers(ctx, input.AttachmentUUID, before, limit, now)
			return err
		}))
		return page
	}
	next := func(sequence int64) ingest.AttachmentDownloadEvent {
		t.Helper()
		capture := ingest.CaptureEvent{Protocol: 1, ProducerUUID: event.ProducerUUID, EventUUID: uuid.NewString(), RunUUID: event.RunUUID,
			CollectionUUID: event.CollectionUUID, CollectionRevision: event.CollectionRevision, RootUUID: &event.RootUUID,
			Kind: "source.capture", ObservedAt: f.now, ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion,
			Post:   ingest.PostReference{Namespace: "native:reddit", Value: "download-post"},
			Source: []byte(`{"category":"reddit","id":"download-post","url":"https://i.redd.it/first.jpg"}`)}
		body, err := json.Marshal(capture)
		require.NoError(t, err)
		_, err = f.service.Capture(t.Context(), f.token, body, ingest.Digest(body))
		require.NoError(t, err)
		other := event
		other.EventUUID, other.CaptureEventUUID, other.TransferSequence = uuid.NewString(), capture.EventUUID, sequence
		return other
	}
	end := event
	end.EventUUID, end.State, end.ReasonCode = uuid.NewString(), "excluded", "unsupported_media"
	post(end)
	initial := read(0, 1, f.now)
	require.Len(t, initial.Transfers, 1)
	anchor := initial.Transfers[0].Sequence
	require.Nil(t, initial.Transfers[0].StartedAt)
	require.Equal(t, "excluded", initial.Transfers[0].State)
	require.Equal(t, f.now, *initial.Transfers[0].FinishedAt)

	second := next(18)
	second.State, second.ReasonCode = "failed", "download_failed"
	post(second)
	page := read(0, 1, f.now)
	require.Len(t, page.Transfers, 1)
	require.Equal(t, second.CaptureEventUUID, page.Transfers[0].CaptureEventUUID)
	require.NotNil(t, page.NextBefore)
	post(event) // First transfer's start arrives after the next transfer's outcome.
	page = read(0, 1, f.now)
	require.Equal(t, second.CaptureEventUUID, page.Transfers[0].CaptureEventUUID, "late start must not reorder transfers")
	older := read(*page.NextBefore, 1, f.now)
	require.Len(t, older.Transfers, 1)
	require.Nil(t, older.NextBefore)
	require.Equal(t, anchor, older.Transfers[0].Sequence)
	require.Equal(t, event.EventUUID, older.Transfers[0].StartEventUUID)
	require.Equal(t, end.EventUUID, older.Transfers[0].TerminalEventUUID)
	require.Equal(t, "excluded", older.Transfers[0].State)
	require.NotNil(t, older.Transfers[0].StartedAt)
	require.NotEmpty(t, older.Transfers[0].CollectionLabel)
	require.NotEmpty(t, older.Transfers[0].RootLabel)
	require.Empty(t, older.Transfers[0].VerificationJob)

	third := next(19)
	post(third)
	page = read(0, 25, f.now)
	require.Len(t, page.Transfers, 3)
	require.Equal(t, "downloading", page.Transfers[0].State)
	require.Equal(t, "started", page.Transfers[0].ReportedState)
	require.Nil(t, page.Transfers[0].FinishedAt)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := albumJobRows(t, raw, "source_runs")
	expired := read(0, 25, f.now.Add(24*time.Hour))
	require.Equal(t, "interrupted", expired.Transfers[0].State)
	require.Equal(t, "failed", expired.Transfers[1].State)
	require.Equal(t, "excluded", expired.Transfers[2].State)
	require.Equal(t, before, albumJobRows(t, raw, "source_runs"), "reading expiration must not finish or renew a source attempt")
	require.EqualValues(t, 4, queryUint(t, raw, "SELECT count(*) FROM source_attachment_downloads"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	for _, bad := range []struct {
		before int64
		limit  int
		now    time.Time
	}{{-1, 1, f.now}, {1 << 54, 1, f.now}, {0, 0, f.now}, {0, 26, f.now}, {0, 1, time.Time{}}} {
		require.ErrorIs(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.SourceAttachment.DownloadTransfers(ctx, input.AttachmentUUID, bad.before, bad.limit, bad.now)
			return err
		}), models.ErrAttachmentDownloadInvalid)
	}
}
