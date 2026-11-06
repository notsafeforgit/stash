package ingest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func linkedGalleryCaptures(t *testing.T, f downloadFixture, url string) (ingest.CaptureEvent, ingest.CaptureEvent) {
	t.Helper()
	seed := map[string]any{
		"category": "redgifs", "id": "linkedpicture", "gallery": "gallery-id", "source_extractor_url": url,
		"urls":    map[string]any{"hd": "https://media.redgifs.com/LinkedPicture.jpg"},
		"_reddit": map[string]any{"id": "gallerypost", "title": "Reddit title", "url": url},
	}
	parent := f.captureFixture.event(t)
	parent.RootUUID, parent.RunUUID = &f.root.UUID, f.run.UUID
	parent.Post = ingest.PostReference{Namespace: "native:reddit", Value: "gallerypost"}
	var err error
	parent.Source, err = json.Marshal(seed)
	require.NoError(t, err)
	parent.Metadata, err = archive.CapturedMetadata(parent.Source)
	require.NoError(t, err)
	child := parent
	child.EventUUID = uuid.NewString()
	child.Post = ingest.PostReference{Namespace: "native:redgifs", Value: "extrapicture"}
	delete(seed, "_reddit")
	seed["id"], seed["description"], seed["createDate"] = "extrapicture", "Host description", 1577836800
	seed["urls"] = map[string]any{"hd": "https://media.redgifs.com/ExtraPicture.jpg"}
	child.Source, err = json.Marshal(seed)
	require.NoError(t, err)
	child.Metadata, err = archive.CapturedMetadata(child.Source)
	require.NoError(t, err)
	return parent, child
}

func TestLinkedGalleryCaptureUsesAcknowledgedParentAndPublishesOwnMetadata(t *testing.T) {
	f := newDownloadFixture(t)
	parent, child := linkedGalleryCaptures(t, f, "https://i.redgifs.com/i/LinkedPicture.jpg")
	_, err := f.submit(t, child)
	require.ErrorIs(t, err, ingest.ErrCaptureDependency, "out-of-order delivery waits for the linked seed")
	_, err = f.submit(t, parent)
	require.NoError(t, err)
	accepted, err := f.submit(t, child)
	require.NoError(t, err)
	replayed, err := f.submit(t, child)
	require.NoError(t, err)
	require.Equal(t, accepted, replayed)
	f.receipt = accepted
	file := f.fileEvent(t)
	file.RunUUID = f.run.UUID
	fileReceipt, err := f.submitFile(t, file)
	require.NoError(t, err)
	report := f.event
	report.CaptureEventUUID, report.Attachment = child.EventUUID, child.Post
	report.State, report.FileEventUUID = "downloaded", file.EventUUID
	_, err = f.report(t, report)
	require.NoError(t, err)
	durable := job.NewDurable(f.service.Repo)
	claimed, err := durable.Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, fileReceipt.JobUUID, claimed.UUID)
	var work ingest.FileWork
	require.NoError(t, json.Unmarshal(claimed.Arguments, &work))
	_, err = durable.Publish(t.Context(), claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		published, err := f.prepared.PublishIntake(ctx, f.service.Repo, work.Publication)
		if err != nil {
			return models.ArchiveJobOutcome{}, err
		}
		body, err := json.Marshal(published.Result)
		return models.ArchiveJobOutcome{State: "succeeded", Result: body}, err
	})
	require.NoError(t, err)
	status, err := f.service.ReceiptStatus(t.Context(), f.token, file.EventUUID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", status.State)
	require.Nil(t, child.Metadata.Title)
	require.Equal(t, "Host description", *child.Metadata.OriginalText)
	require.Equal(t, "2020-01-01T00:00:00.000Z", *child.Metadata.PublishedAt)
}

func TestLinkedGalleryCaptureRejectsUnrelatedEvidenceAndDeliveryScopes(t *testing.T) {
	f := newDownloadFixture(t)
	parent, child := linkedGalleryCaptures(t, f, "https://www.redgifs.com/watch/linkedpicture")
	for _, change := range []struct {
		name string
		edit func(*ingest.CaptureEvent)
		want error
	}{
		{"another run", func(e *ingest.CaptureEvent) { e.RunUUID = uuid.NewString() }, ingest.ErrForbidden},
		{"another gallery", func(e *ingest.CaptureEvent) {
			e.Source = []byte(strings.ReplaceAll(string(e.Source), "gallery-id", "unrelated-gallery"))
		}, ingest.ErrForbidden},
		{"unrelated host", func(e *ingest.CaptureEvent) {
			e.Source = []byte(strings.ReplaceAll(string(e.Source), "www.redgifs.com", "unrelated.test"))
		}, ingest.ErrForbidden},
		{"missing gallery", func(e *ingest.CaptureEvent) {
			e.Source = []byte(strings.ReplaceAll(string(e.Source), `"gallery":"gallery-id",`, ""))
		}, ingest.ErrForbidden},
	} {
		t.Run(change.name, func(t *testing.T) {
			_, err := f.submit(t, parent)
			require.NoError(t, err)
			event := child
			event.EventUUID = uuid.NewString()
			change.edit(&event)
			_, err = f.submit(t, event)
			require.ErrorIs(t, err, change.want)
		})
	}
	// A receipt in the same collection from another producer grants no link.
	var producer *models.IngestProducer
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = f.service.Repo.Ingest.CreateProducer(ctx, "Other worker")
		return err
	}))
	_, token, err := f.service.IssueCredential(t.Context(), producer.UUID,
		[]models.IngestScope{{CollectionUUID: f.collection.UUID, RootUUID: &f.root.UUID}}, nil)
	require.NoError(t, err)
	child.ProducerUUID = producer.UUID
	raw := eventBytes(t, child)
	_, err = f.service.Capture(t.Context(), token, raw, ingest.Digest(raw))
	require.ErrorIs(t, err, ingest.ErrCaptureDependency)
}
