package ingest_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func (f intakePublicationFixture) fileEvent(t *testing.T) ingest.FileEvent {
	t.Helper()
	event := ingest.FileEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(),
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: f.root.UUID, Kind: "file.completed",
		ObservedAt: time.Now().UTC(), RelativePath: "image.png", Size: f.prepared.Snapshot().Size, SHA256: f.prepared.SHA256(), MediaKind: models.ArchiveImage}
	if f.receipt != nil {
		attachment := f.attachment(t, 0)
		event.Source = &ingest.FileEventSource{CaptureEventUUID: f.receipt.EventUUID, Attachment: ingest.PostReference{Namespace: attachment.Reference.Namespace, Value: attachment.Reference.Value}}
	}
	return event
}

func (f intakePublicationFixture) submitFile(t *testing.T, event ingest.FileEvent) (*models.IngestReceipt, error) {
	t.Helper()
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	return f.service.FileCompleted(t.Context(), f.token, raw, ingest.Digest(raw))
}

func TestFileEventAcknowledgesQueueAndExposesCommittedResultSeparately(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	event := f.fileEvent(t)
	accepted, err := f.submitFile(t, event)
	require.NoError(t, err)
	require.NotEmpty(t, accepted.JobUUID)
	require.Equal(t, f.receipt.CaptureUUID, accepted.CaptureUUID)
	require.JSONEq(t, `{"status":"queued","media_ingested":false}`, string(accepted.Result))
	var wait sync.WaitGroup
	for range 4 {
		wait.Go(func() {
			replay, err := f.submitFile(t, event)
			require.NoError(t, err)
			require.Equal(t, accepted, replay)
		})
	}
	wait.Wait()
	repo := f.service.Repo
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		file, err := repo.File.FindByPath(ctx, f.path, true)
		require.NoError(t, err)
		require.Nil(t, file, "acceptance cannot claim a producer digest was verified")
		return nil
	}))
	status, err := f.service.ReceiptStatus(t.Context(), f.token, event.EventUUID)
	require.NoError(t, err)
	require.Equal(t, "queued", status.State)
	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	for _, private := range []string{f.root.Binding.Path, "arguments", "owner_uuid", "secret_hash"} {
		require.NotContains(t, string(encoded), private)
	}
	durable := job.NewDurable(repo)
	claimed, err := durable.Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, accepted.JobUUID, claimed.UUID)
	var work ingest.FileWork
	require.NoError(t, json.Unmarshal(claimed.Arguments, &work))
	_, err = durable.Publish(t.Context(), claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		published, err := f.prepared.PublishIntake(ctx, repo, work.Publication)
		if err != nil {
			return models.ArchiveJobOutcome{}, err
		}
		body, err := json.Marshal(published.Result)
		return models.ArchiveJobOutcome{State: "succeeded", Result: body}, err
	})
	require.NoError(t, err)
	status, err = f.service.ReceiptStatus(t.Context(), f.token, event.EventUUID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", status.State)
	var result ingest.IntakePublicationResult
	require.NoError(t, json.Unmarshal(status.Result, &result))
	require.NotEmpty(t, result.MediaUUID)
	require.NotEmpty(t, result.GalleryUUID)
	require.Equal(t, accepted, status.Receipt, "the accepted acknowledgement is immutable after completion")
	replay, err := f.submitFile(t, event)
	require.NoError(t, err)
	require.Equal(t, accepted, replay)
}

func TestFileEventSingleRedditAttachmentLinksMediaWithoutCreatingGallery(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	capture := f.event(t)
	capture.RootUUID = &f.root.UUID
	capture.Post.Value = "singlepost"
	capture.Source = json.RawMessage(`{"category":"reddit","id":"singlepost","url":"https://i.redd.it/actualmedia.png"}`)
	var err error
	f.receipt, err = f.submit(t, capture)
	require.NoError(t, err)
	require.Equal(t, "actualmedia", f.attachment(t, 0).Reference.Value)
	event := f.fileEvent(t)
	accepted, err := f.submitFile(t, event)
	require.NoError(t, err)
	repo := f.service.Repo
	durable := job.NewDurable(repo)
	claimed, err := durable.Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, accepted.JobUUID, claimed.UUID)
	var work ingest.FileWork
	require.NoError(t, json.Unmarshal(claimed.Arguments, &work))
	_, err = durable.Publish(t.Context(), claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		published, err := f.prepared.PublishIntake(ctx, repo, work.Publication)
		if err != nil {
			return models.ArchiveJobOutcome{}, err
		}
		require.Equal(t, "linked", published.Result.SourceMedia)
		require.NotEmpty(t, published.Result.MediaUUID)
		require.Empty(t, published.Result.GalleryUUID, "a singleton is attributable without being an album")
		body, err := json.Marshal(published.Result)
		return models.ArchiveJobOutcome{State: "succeeded", Result: body}, err
	})
	require.NoError(t, err)
	status, err := f.service.ReceiptStatus(t.Context(), f.token, event.EventUUID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", status.State)
}

func TestFileEventReplaySurvivesRestartMissingFileAndTokenRotation(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	event := f.fileEvent(t)
	accepted, err := f.submitFile(t, event)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.NoError(t, os.Remove(f.path))
	_, replacement, err := f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
	require.NoError(t, err)
	repo := f.service.Repo
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Ingest.RevokeCredential(ctx, f.credential.UUID) }))
	_, err = f.service.ReceiptStatus(t.Context(), f.token, event.EventUUID)
	require.ErrorIs(t, err, ingest.ErrUnauthorized)
	f.token = replacement
	replay, err := f.submitFile(t, event)
	require.NoError(t, err)
	require.Equal(t, accepted, replay)
	status, err := f.service.ReceiptStatus(t.Context(), f.token, event.EventUUID)
	require.NoError(t, err)
	require.Equal(t, "queued", status.State, "revocation controls future API access; it does not erase accepted work")
	event.Size++
	_, err = f.submitFile(t, event)
	require.ErrorIs(t, err, models.ErrIngestReplay)
}

func TestFileEventRejectsInvalidScopeAndClaimsWithoutScheduling(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	for _, tc := range []struct {
		name   string
		change func(*ingest.FileEvent)
	}{
		{"protocol", func(e *ingest.FileEvent) { e.Protocol++ }},
		{"digest", func(e *ingest.FileEvent) { e.SHA256 = "not-a-digest" }},
		{"size", func(e *ingest.FileEvent) { e.Size = -1 }},
		{"path escape", func(e *ingest.FileEvent) { e.RelativePath = "../outside.png" }},
		{"partial", func(e *ingest.FileEvent) { e.RelativePath += ".part" }},
		{"missing file", func(e *ingest.FileEvent) { e.RelativePath = "missing.png" }},
		{"producer", func(e *ingest.FileEvent) { e.ProducerUUID = uuid.NewString() }},
		{"root", func(e *ingest.FileEvent) { e.RootUUID = uuid.NewString() }},
		{"collection", func(e *ingest.FileEvent) { e.CollectionUUID = uuid.NewString() }},
		{"future definition", func(e *ingest.FileEvent) { e.CollectionRevision++ }},
		{"unbound definition", func(e *ingest.FileEvent) { e.CollectionRevision-- }},
		{"media type", func(e *ingest.FileEvent) { e.MediaKind = models.ArchivePerformer }},
		{"capture", func(e *ingest.FileEvent) { e.Source.CaptureEventUUID = uuid.NewString() }},
		{"attachment", func(e *ingest.FileEvent) { e.Source.Attachment.Value = "unobserved" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := f.fileEvent(t)
			tc.change(&event)
			_, err := f.submitFile(t, event)
			require.Error(t, err)
			_, err = f.service.ReceiptStatus(t.Context(), f.token, event.EventUUID)
			require.ErrorIs(t, err, ingest.ErrNotFound)
		})
	}
	repo := f.service.Repo
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		jobs, err := repo.ArchiveJob.List(ctx, models.ArchiveJobVerifyMedia, "queued", 0, 10)
		require.NoError(t, err)
		require.Empty(t, jobs)
		return nil
	}))
}

func TestFileEventReceiptFailureRollsBackJobSubmission(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	event := f.fileEvent(t)
	original := f.service.Repo.Ingest
	f.service.Repo.Ingest = failingReceiptStore{original}
	_, err := f.submitFile(t, event)
	require.ErrorContains(t, err, "receipt storage failed")
	f.service.Repo.Ingest = original
	repo := f.service.Repo
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		request := uuid.NewSHA1(uuid.MustParse(event.ProducerUUID), []byte("file.completed\x00"+event.EventUUID)).String()
		stored, err := repo.ArchiveJob.FindSubmission(ctx, request)
		require.NoError(t, err)
		require.Nil(t, stored)
		return nil
	}))
}

func TestFileEventManualReceiptAndJobAreAnonymisedTogether(t *testing.T) {
	f := newIntakePublicationFixture(t, false)
	var err error
	f.credential, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
	require.NoError(t, err)
	event := f.fileEvent(t)
	receipt, err := f.submitFile(t, event)
	require.NoError(t, err)
	require.Empty(t, receipt.CaptureUUID)
	require.Empty(t, receipt.PostUUID)
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, path)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer raw.Close()
	for _, table := range []string{"ingest_receipts", "archive_jobs", "archive_job_submissions", "ingest_credentials", "source_collection_captures"} {
		var count int
		require.NoError(t, raw.QueryRow("SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count)
	}
	var violations int
	require.NoError(t, raw.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations))
	require.Zero(t, violations)
}
