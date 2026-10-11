package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func capturedGapFixture(t *testing.T, f *enrichmentExecutionFixture, raw []byte) string {
	t.Helper()
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", raw)
	require.NoError(t, err)
	metadata, err := archive.CapturedMetadata(raw)
	require.NoError(t, err)
	id := uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: id, PostUUID: f.target.PostUUID, Origin: "gallery-dl", Platform: "reddit",
			CapturedAt: f.now.Add(-time.Hour), RetentionPolicy: archive.SourceRetentionVersion, Metadata: metadata, Payload: *payload})
		return err
	}))
	return id
}

func TestEnrichmentReusesCapturedPostAcrossCollectionsWithoutDuplicatingEvidence(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	id := capturedGapFixture(t, f, []byte(`{"category":"reddit","id":"abc123","title":"Original post","author":"example","date":"2026-10-02T00:00:00Z"}`))
	completion, err := f.worker.ReuseCaptured(t.Context(), f.tokens[0], f.target.UUID, f.target.Revision)
	require.NoError(t, err)
	require.NotNil(t, completion)
	require.Equal(t, []string{id}, completion.CaptureUUIDs)
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", current.State)
	require.Zero(t, current.Fence, "reuse must not claim a website fetch")
	again, err := f.worker.ReuseCaptured(t.Context(), f.tokens[0], f.target.UUID, f.target.Revision)
	require.NoError(t, err)
	require.Equal(t, completion, again, "lost response retry reuses the same completion")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	backupPath := filepath.Join(t.TempDir(), "reused.sqlite")
	require.NoError(t, f.db.Backup(backupPath))
	require.NoError(t, f.db.AuditForTesting(backupPath))
}

func TestEnrichmentCaptureReuseDoesNotHideWrongIdentityOrPendingChildren(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	capturedGapFixture(t, f, []byte(`{"category":"reddit","id":"different","title":"Another post"}`))
	completion, err := f.worker.ReuseCaptured(t.Context(), f.tokens[0], f.target.UUID, f.target.Revision)
	require.NoError(t, err)
	require.Nil(t, completion)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "budget_yield")
	require.NoError(t, err)
	capturedGapFixture(t, f, []byte(`{"category":"reddit","id":"abc123","title":"Original post"}`))
	completion, err = f.worker.ReuseCaptured(t.Context(), f.tokens[0], f.target.UUID, f.target.Revision)
	require.NoError(t, err)
	require.Nil(t, completion, "a partial enrichment still owns its child checkpoint")
}

func TestMetadataRequestYieldsToNewDownloadsWithoutConsumingFailureBudget(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	input := models.SourceRequestInput{RequestUUID: uuid.NewString(), URL: "https://oauth.reddit.com/comments/abc123", Class: "recovery", Operation: "other"}
	permit, err := f.worker.RequestSource(t.Context(), f.tokens[0], running.Lease(), input)
	require.NoError(t, err)
	require.True(t, permit.Ready)
	again, err := f.worker.RequestSource(t.Context(), f.tokens[0], running.Lease(), input)
	require.NoError(t, err)
	require.Equal(t, permit, again)
	require.NoError(t, f.worker.ReportSourceRequest(t.Context(), f.tokens[0], running.Lease(), models.SourceRequestResult{RequestUUID: input.RequestUUID, Status: 200}))
	download := newPacingDownload(t, f, "https://reddit.com/user/new-posts")
	run := download.claim(t)
	require.NotNil(t, run, "a metadata lease cannot block newly queued downloads")
	input.RequestUUID = uuid.NewString()
	permit, err = f.worker.RequestSource(t.Context(), f.tokens[0], running.Lease(), input)
	require.NoError(t, err)
	require.False(t, permit.Ready)
	require.Zero(t, permit.WaitMS)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "budget_yield")
	require.NoError(t, err)
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Zero(t, current.Failures)
	require.Equal(t, "queued", current.State)
	request := input
	request.RequestUUID = uuid.NewString()
	request.Class = "routine"
	grant, err := download.coordinator.RequestSource(t.Context(), download.token, run.Lease(), request)
	require.NoError(t, err)
	require.False(t, grant.Ready, "both paths share the same last-request spacing")
	f.now = f.now.Add(5 * time.Second)
	grant, err = download.coordinator.RequestSource(t.Context(), download.token, run.Lease(), request)
	require.NoError(t, err)
	require.True(t, grant.Ready)
	backupPath := filepath.Join(t.TempDir(), "yielded.sqlite")
	require.NoError(t, f.db.Backup(backupPath))
	require.NoError(t, f.db.AuditForTesting(backupPath))
}

func TestNormalCaptureAutomaticallyFillsHistoricalGap(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	raw := []byte(`{"category":"reddit","id":"abc123","title":"Original caption","date":"2026-10-02T00:00:00Z"}`)
	metadata, err := archive.CapturedMetadata(raw)
	require.NoError(t, err)
	body, err := json.Marshal(ingest.CaptureEvent{Protocol: 1, ProducerUUID: f.producers[0].UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(),
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Kind: "source.capture", ObservedAt: f.now,
		ExtractorVersion: "1.32.15-dev", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "abc123"},
		Metadata: metadata, Source: raw})
	require.NoError(t, err)
	receipt, err := f.service.Capture(t.Context(), f.tokens[0], body, ingest.Digest(body))
	require.NoError(t, err)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		target, err := f.repo.EnrichmentWork.Target(ctx, f.target.UUID)
		require.NoError(t, err)
		require.Equal(t, "completed", target.State)
		completion, err := f.repo.EnrichmentWork.Completion(ctx, *target.CompletionUUID)
		require.NoError(t, err)
		require.Equal(t, []string{receipt.CaptureUUID}, completion.CaptureUUIDs)
		return nil
	}))
}
