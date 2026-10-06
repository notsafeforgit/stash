package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type downloadFixture struct {
	intakePublicationFixture
	coordinator *ingest.RunCoordinator
	run         *models.SourceRun
	event       ingest.AttachmentDownloadEvent
	now         time.Time
}

func newDownloadFixture(t *testing.T) downloadFixture {
	t.Helper()
	f := downloadFixture{intakePublicationFixture: newIntakePublicationFixture(t, true), now: time.Now().UTC().Truncate(time.Millisecond)}
	repo := f.service.Repo
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := f.collection.SourceCollectionDefinition
		definition.TargetURL = "https://www.reddit.com/user/example/submitted/"
		var err error
		f.collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	f.coordinator = ingest.NewRunCoordinator(f.service)
	f.coordinator.Now = func() time.Time { return f.now }
	policy := strings.Repeat("a", 64)
	var err error
	f.run, err = f.coordinator.Submit(t.Context(), f.token, models.SourceRunRequest{RequestUUID: uuid.NewString(), CollectionUUID: f.collection.UUID,
		CollectionRevision: f.collection.Revision, Operation: "download", PolicySHA256: policy, Window: models.SourceWindow{Until: f.now}})
	require.NoError(t, err)
	f.run, err = f.coordinator.Claim(t.Context(), f.token, f.run.UUID, uuid.NewString(), policy, time.Minute)
	require.NoError(t, err)
	capture := f.captureFixture.event(t)
	capture.RootUUID, capture.RunUUID = &f.root.UUID, f.run.UUID
	f.receipt, err = f.submit(t, capture)
	require.NoError(t, err)
	attachment := f.attachment(t, 0)
	f.event = ingest.AttachmentDownloadEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: f.run.UUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: f.root.UUID, Kind: "attachment.download", ObservedAt: f.now,
		OwnerUUID: f.run.OwnerUUID, Fence: f.run.Fence, TransferSequence: 11, CaptureEventUUID: capture.EventUUID,
		Attachment: ingest.PostReference{Namespace: attachment.Reference.Namespace, Value: attachment.Reference.Value}, State: "started"}
	return f
}

func (f downloadFixture) report(t *testing.T, event ingest.AttachmentDownloadEvent) (*models.IngestReceipt, error) {
	t.Helper()
	body, err := json.Marshal(event)
	require.NoError(t, err)
	return f.service.AttachmentDownload(t.Context(), f.token, body, ingest.Digest(body))
}

func (f downloadFixture) history(t *testing.T, after int64, limit int, now time.Time) []models.AttachmentDownloadReport {
	t.Helper()
	var ret []models.AttachmentDownloadReport
	attachment := f.attachment(t, 0)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.service.Repo.SourceAttachment.DownloadHistory(ctx, attachment.UUID, after, limit, now)
		return err
	}))
	return ret
}

func TestAttachmentDownloadServiceReportsQueuedVerificationWithoutInventingMedia(t *testing.T) {
	f := newDownloadFixture(t)
	start, err := f.report(t, f.event)
	require.NoError(t, err)
	require.Empty(t, start.JobUUID)
	rows := f.history(t, 0, 100, f.now)
	require.Len(t, rows, 1)
	require.Equal(t, "downloading", rows[0].TransferState)
	require.Equal(t, f.now, rows[0].ObservedAt)
	require.False(t, rows[0].RecordedAt.IsZero())
	file := f.fileEvent(t)
	file.RunUUID = f.run.UUID
	fileReceipt, err := f.submitFile(t, file)
	require.NoError(t, err)
	end := f.event
	end.EventUUID, end.State, end.FileEventUUID = uuid.NewString(), "downloaded", file.EventUUID
	accepted, err := f.report(t, end)
	require.NoError(t, err)
	require.Contains(t, string(accepted.Result), `"media_ingested":false`)
	rows = f.history(t, 0, 100, f.now)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "downloaded", row.TransferState)
		require.Equal(t, "queued", row.VerificationState)
		require.Equal(t, fileReceipt.JobUUID, row.VerificationJob)
	}
	attachment := f.attachment(t, 0)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		file, err := f.service.Repo.File.FindByPath(ctx, f.path, true)
		require.NoError(t, err)
		require.Nil(t, file)
		decision, err := f.service.Repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
		require.NoError(t, err)
		require.Nil(t, decision)
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	replay, err := f.report(t, end)
	require.NoError(t, err)
	require.Equal(t, accepted, replay)
	require.Len(t, f.history(t, 0, 100, f.now), 2)
}

func TestAttachmentDownloadServiceLateReportsKeepTerminalStateAndOriginalReceipt(t *testing.T) {
	f := newDownloadFixture(t)
	_, err := f.coordinator.Finish(t.Context(), f.token, f.run.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.NoError(t, err)
	end := f.event
	end.EventUUID, end.State, end.ReasonCode = uuid.NewString(), "failed", "download_failed"
	first, err := f.report(t, end)
	require.NoError(t, err)
	_, err = f.report(t, f.event)
	require.NoError(t, err, "offline delivery can reverse start/finish receipt order")
	rows := f.history(t, 0, 100, f.now)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "failed", row.TransferState)
	}
	page := f.history(t, rows[0].Sequence, 1, f.now)
	require.Equal(t, rows[1:], page)
	replay, err := f.report(t, end)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	end.ReasonCode = "postprocess_failed"
	_, err = f.report(t, end)
	require.ErrorIs(t, err, models.ErrIngestReplay)
	end.EventUUID = uuid.NewString()
	_, err = f.report(t, end)
	require.ErrorIs(t, err, models.ErrAttachmentDownloadConflict)
	require.Len(t, f.history(t, 0, 100, f.now), 2)
}

func TestAttachmentDownloadServiceExpiresWithoutInventingTerminalReports(t *testing.T) {
	f := newDownloadFixture(t)
	_, err := f.report(t, f.event)
	require.NoError(t, err)
	rows := f.history(t, 0, 100, *f.run.LeaseUntil)
	require.Len(t, rows, 1)
	require.Equal(t, "started", rows[0].State)
	require.Equal(t, "interrupted", rows[0].TransferState)
	// Inspecting expiry neither recovers the source run nor inserts a fake
	// producer failure. Late delivery can still supply the true terminal state.
	run, err := f.coordinator.Find(t.Context(), f.token, f.run.UUID)
	require.NoError(t, err)
	require.Equal(t, "running", run.State)
	end := f.event
	end.EventUUID, end.State, end.ReasonCode = uuid.NewString(), "skipped", "archive_entry_without_file"
	_, err = f.report(t, end)
	require.NoError(t, err)
	rows = f.history(t, 0, 100, f.run.LeaseUntil.Add(time.Hour))
	require.Len(t, rows, 2)
	require.Equal(t, "skipped", rows[0].TransferState)
}

func TestAttachmentDownloadServiceRejectsForeignAttemptAndCaptureScope(t *testing.T) {
	f := newDownloadFixture(t)
	for name, change := range map[string]func(*ingest.AttachmentDownloadEvent){
		"producer":            func(e *ingest.AttachmentDownloadEvent) { e.ProducerUUID = uuid.NewString() },
		"owner":               func(e *ingest.AttachmentDownloadEvent) { e.OwnerUUID = uuid.NewString() },
		"fence":               func(e *ingest.AttachmentDownloadEvent) { e.Fence++ },
		"collection":          func(e *ingest.AttachmentDownloadEvent) { e.CollectionUUID = uuid.NewString() },
		"collection revision": func(e *ingest.AttachmentDownloadEvent) { e.CollectionRevision++ },
		"root":                func(e *ingest.AttachmentDownloadEvent) { e.RootUUID = uuid.NewString() },
	} {
		t.Run(name, func(t *testing.T) {
			event := f.event
			change(&event)
			_, err := f.report(t, event)
			require.ErrorIs(t, err, ingest.ErrForbidden)
		})
	}
	other := f.captureFixture.event(t)
	other.RootUUID = &f.root.UUID
	_, err := f.submit(t, other)
	require.NoError(t, err)
	event := f.event
	event.CaptureEventUUID = other.EventUUID
	_, err = f.report(t, event)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	event = f.event
	event.Attachment.Value = "missing"
	_, err = f.report(t, event)
	require.ErrorIs(t, err, ingest.ErrNotFound)
	require.Empty(t, f.history(t, 0, 100, f.now))
}

func TestAttachmentDownloadServiceRejectsAnotherAttachmentsFileReceiptAndTransferSequence(t *testing.T) {
	f := newDownloadFixture(t)
	file := f.fileEvent(t)
	file.RunUUID = f.run.UUID
	other := f.attachment(t, 1)
	file.Source.Attachment = ingest.PostReference{Namespace: other.Reference.Namespace, Value: other.Reference.Value}
	_, err := f.submitFile(t, file)
	require.NoError(t, err)
	end := f.event
	end.State, end.FileEventUUID = "downloaded", file.EventUUID
	_, err = f.report(t, end)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	_, err = f.report(t, f.event)
	require.NoError(t, err)
	second := f.event
	second.EventUUID = uuid.NewString()
	second.Attachment = file.Source.Attachment
	_, err = f.report(t, second)
	require.ErrorIs(t, err, models.ErrAttachmentDownloadConflict)
	end.EventUUID, end.State, end.FileEventUUID, end.ReasonCode = uuid.NewString(), "failed", "", "download_failed"
	end.TransferSequence++
	_, err = f.report(t, end)
	require.ErrorIs(t, err, models.ErrAttachmentDownloadConflict)
	require.Len(t, f.history(t, 0, 100, f.now), 1)
}

type failingDownloadReceipt struct {
	models.IngestReaderWriter
	err error
}

func (f failingDownloadReceipt) RecordReceipt(context.Context, models.IngestReceipt) (*models.IngestReceipt, error) {
	return nil, f.err
}

func TestAttachmentDownloadServiceRollsBackReportWhenReceiptFails(t *testing.T) {
	f := newDownloadFixture(t)
	expected := errors.New("late receipt failure")
	previous := f.service.Repo.Ingest
	f.service.Repo.Ingest = failingDownloadReceipt{IngestReaderWriter: previous, err: expected}
	_, err := f.report(t, f.event)
	require.ErrorIs(t, err, expected)
	f.service.Repo.Ingest = previous
	require.Empty(t, f.history(t, 0, 100, f.now))
	_, err = f.report(t, f.event)
	require.NoError(t, err)
}
