package sqlite_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceRunAttachmentRetryRequiresDurableScopedFailureAndLeavesCrawlComplete(t *testing.T) {
	f := newSourceRunFixture(t)
	request := f.request()
	parent := f.claim(t, f.submit(t, request))
	capture := ingest.CaptureEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: parent.UUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: &f.root.UUID, Kind: "source.capture", ObservedAt: f.now,
		ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "abc123"},
		Source: []byte(`{"category":"reddit","id":"abc123","url":"https://i.redd.it/first.jpg"}`)}
	body, err := json.Marshal(capture)
	require.NoError(t, err)
	_, err = f.service.Capture(t.Context(), f.token, body, ingest.Digest(body))
	require.NoError(t, err)
	failure := ingest.AttachmentDownloadEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: parent.UUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: f.root.UUID, Kind: "attachment.download", ObservedAt: f.now,
		OwnerUUID: parent.OwnerUUID, Fence: parent.Fence, TransferSequence: 17, CaptureEventUUID: capture.EventUUID,
		Attachment: ingest.PostReference{Namespace: "native:reddit", Value: "first"}, State: "failed", ReasonCode: "download_failed"}
	retry := request
	retry.RequestUUID = uuid.NewString()
	retry.AttachmentRetryEventUUID = failure.EventUUID
	retry.RetrievalURL = "https://www.reddit.com/comments/abc123/"
	_, err = f.coordinator.Submit(t.Context(), f.token, retry)
	require.ErrorIs(t, err, models.ErrSourceRunDependency, "late outbox delivery remains retryable")
	body, err = json.Marshal(failure)
	require.NoError(t, err)
	_, err = f.service.AttachmentDownload(t.Context(), f.token, body, ingest.Digest(body))
	require.NoError(t, err)
	wrong := retry
	wrong.RequestUUID = uuid.NewString()
	wrong.RetrievalURL = "https://www.reddit.com/comments/other/"
	_, err = f.coordinator.Submit(t.Context(), f.token, wrong)
	require.ErrorIs(t, err, models.ErrSourceRunInvalid)
	wrong = retry
	wrong.RequestUUID = uuid.NewString()
	wrong.Window.Since = nil
	_, err = f.coordinator.Submit(t.Context(), f.token, wrong)
	require.ErrorIs(t, err, models.ErrSourceRunInvalid, "a failure cannot authorize a wider crawl")
	child := f.submit(t, retry)
	require.True(t, child.AttachmentRetry)
	require.NotEqual(t, parent.UUID, child.UUID)
	require.Equal(t, retry.RetrievalURL, child.TargetURL)
	require.Equal(t, parent.PathPrefix, child.PathPrefix)
	require.Equal(t, child, f.submit(t, retry))
	parent = f.finish(t, parent, "succeeded")
	require.Equal(t, "succeeded", parent.State)
	require.Equal(t, "queued", f.find(t, child.UUID).State)
	f.now = f.now.Add(time.Minute)
	child = f.claim(t, child)
	require.NotNil(t, child)
	child = f.finish(t, child, "retry")
	require.Equal(t, "succeeded", f.find(t, parent.UUID).State)
	require.Equal(t, "queued", child.State)
	require.Empty(t, child.Completed)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_run_attachment_retries"))
	_, err = raw.Exec("DELETE FROM source_run_attachment_retries")
	require.ErrorContains(t, err, "retained")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.True(t, f.find(t, child.UUID).AttachmentRetry)
	require.Equal(t, "succeeded", f.find(t, parent.UUID).State)
}
