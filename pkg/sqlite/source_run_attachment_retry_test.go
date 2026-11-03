package sqlite_test

import (
	"encoding/json"
	"strings"
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

func newFocusedAttachmentRetry(t *testing.T) (*sourceRunFixture, *models.SourceRun, *models.SourceRun, models.SourceRunRequest) {
	t.Helper()
	f := newSourceRunFixture(t)
	request := f.request()
	parent := f.claim(t, f.submit(t, request))
	capture := ingest.CaptureEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: parent.UUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: &f.root.UUID, Kind: "source.capture", ObservedAt: f.now,
		ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "retry123"},
		Source: []byte(`{"category":"reddit","id":"retry123","url":"https://i.redd.it/missing.jpg"}`)}
	body, err := json.Marshal(capture)
	require.NoError(t, err)
	_, err = f.service.Capture(t.Context(), f.token, body, ingest.Digest(body))
	require.NoError(t, err)
	failure := ingest.AttachmentDownloadEvent{Protocol: 1, ProducerUUID: f.producer.UUID, EventUUID: uuid.NewString(), RunUUID: parent.UUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: f.root.UUID, Kind: "attachment.download", ObservedAt: f.now,
		OwnerUUID: parent.OwnerUUID, Fence: parent.Fence, TransferSequence: 1, CaptureEventUUID: capture.EventUUID,
		Attachment: ingest.PostReference{Namespace: "native:reddit", Value: "missing"}, State: "failed", ReasonCode: "download_failed"}
	body, err = json.Marshal(failure)
	require.NoError(t, err)
	_, err = f.service.AttachmentDownload(t.Context(), f.token, body, ingest.Digest(body))
	require.NoError(t, err)
	request.RequestUUID = uuid.NewString()
	request.AttachmentRetryEventUUID = failure.EventUUID
	request.RetrievalURL = "https://www.reddit.com/comments/retry123/"
	child := f.submit(t, request)
	parent = f.finish(t, parent, "succeeded")
	f.now = parent.AvailableAt
	return f, parent, child, request
}

func TestFocusedAttachmentRetryExpiresWithoutCompletingMissingCoverageOrBlockingOtherPasses(t *testing.T) {
	for _, code := range []string{"source_download_failed", "not_found"} {
		t.Run(code, func(t *testing.T) {
			f, parent, child, original := newFocusedAttachmentRetry(t)
			deadline := child.CreatedAt.Add(48 * time.Hour)
			for attempt := 0; attempt < 2; attempt++ {
				child = f.claim(t, child)
				require.NotNil(t, child)
				var err error
				child, err = f.coordinator.Finish(t.Context(), f.token, child.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: code})
				require.NoError(t, err)
				require.Equal(t, "queued", child.State)
				if attempt == 0 {
					require.Equal(t, f.now.Add(24*time.Hour), child.AvailableAt)
					other := f.request()
					other.PolicySHA256 = strings.Repeat("b", 64)
					otherRun := f.submit(t, other)
					f.now = f.now.Add(time.Duration(child.CooldownSeconds) * time.Second)
					otherRun = f.claim(t, otherRun)
					require.NotNil(t, otherRun, "the file retry must not block initial or search work")
					f.finish(t, otherRun, "succeeded")
				} else {
					require.Equal(t, deadline, child.AvailableAt)
				}
				f.now = child.AvailableAt
			}
			require.Nil(t, f.claim(t, child), "do not repeat a known missing transfer after its recovery window")
			closed := f.find(t, child.UUID)
			require.Equal(t, "cancelled", closed.State)
			require.Equal(t, "attachment_retry_exhausted", closed.ErrorCode)
			require.Equal(t, child.Fence, closed.Fence, "retirement does not invent another download attempt")
			require.Equal(t, child.Pending, closed.Pending)
			require.Empty(t, closed.Completed)
			require.Equal(t, closed, f.submit(t, original), "original callers retain their exact terminal receipt")
			require.Equal(t, "succeeded", f.find(t, parent.UUID).State)
			attempts, err := f.coordinator.Attempts(t.Context(), f.token, child.UUID, 0)
			require.NoError(t, err)
			require.Len(t, attempts, 2)
			for _, attempt := range attempts {
				require.Equal(t, "retry", attempt.Outcome)
				require.Equal(t, code, attempt.ErrorCode, "actual failure history is retained")
			}
		})
	}
}

func TestFocusedAttachmentRetryPreservesRecoveryAndInfrastructureFailures(t *testing.T) {
	for _, outcome := range []models.SourceRunOutcome{
		{State: "succeeded"},
		{State: "retry", ErrorCode: "worker_storage_unavailable"},
		{State: "retry", ErrorCode: "outbox_capacity"},
		{State: "retry", ErrorCode: "source_download_failed"},
	} {
		t.Run(outcome.State+outcome.ErrorCode, func(t *testing.T) {
			f, _, child, _ := newFocusedAttachmentRetry(t)
			// An attempt that starts just before the boundary must still finish.
			f.now = child.CreatedAt.Add(48*time.Hour - time.Second)
			child = f.claim(t, child)
			require.NotNil(t, child)
			f.now = f.now.Add(2 * time.Second)
			result, err := f.coordinator.Finish(t.Context(), f.token, child.Lease(), outcome)
			require.NoError(t, err)
			switch outcome.ErrorCode {
			case "":
				require.Equal(t, "succeeded", result.State)
				require.NotEmpty(t, result.Completed)
			case "source_download_failed":
				require.Equal(t, "cancelled", result.State)
				require.Equal(t, "attachment_retry_exhausted", result.ErrorCode)
				require.Empty(t, result.Completed)
			default:
				require.Equal(t, "queued", result.State, "local infrastructure failure says nothing about remote media")
				f.now = result.AvailableAt
				require.NotNil(t, f.claim(t, result))
			}
		})
	}
}
