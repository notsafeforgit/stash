package sqlite_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func extraEnrichmentTarget(t *testing.T, f *enrichmentExecutionFixture, key string, priority int) *models.EnrichmentTarget {
	t.Helper()
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: key}, "")
	url, err := observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://www.reddit.com/comments/" + key})
	require.NoError(t, err)
	return retainEnrichment(t, f.repo, models.EnrichmentTargetInput{PostUUID: post.UUID, URLUUID: url.URLUUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Policy: models.EnrichmentGalleryMetadataV1, Origin: "review"},
		models.EnrichmentSchedule{State: "pending", Priority: priority}, f.now)
}

func TestEnrichmentDispatchPagesPendingTargetsAndAdmittedRetries(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	for i := range 27 {
		extraEnrichmentTarget(t, f, fmt.Sprintf("post%d", i), i%3)
	}
	var after *models.EnrichmentTargetCursor
	seen := map[string]bool{}
	for {
		page, err := f.worker.ReadyPage(t.Context(), f.tokens[0], f.collection.UUID, after, 5)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		for _, target := range page {
			require.False(t, seen[target.UUID])
			seen[target.UUID] = true
			if after != nil {
				require.LessOrEqual(t, target.Priority, after.Priority)
			}
			after = &models.EnrichmentTargetCursor{UUID: target.UUID, Priority: target.Priority, NotBefore: target.NotBefore}
		}
	}
	require.Len(t, seen, 28)
	job := f.admit(t)
	ready := func(after int64) []models.EnrichmentJobCandidate {
		page, err := f.worker.ReadyJobs(t.Context(), f.tokens[0], f.collection.UUID, strings.Repeat("a", 64), "1.32.15-dev", after, 5)
		require.NoError(t, err)
		return page
	}
	require.Equal(t, []models.EnrichmentJobCandidate{{Sequence: job.Sequence, UUID: job.UUID}}, ready(0))
	require.Empty(t, ready(job.Sequence))
	for _, item := range []struct{ policy, extractor string }{{strings.Repeat("b", 64), "1.32.15-dev"}, {strings.Repeat("a", 64), "other"}} {
		page, err := f.worker.ReadyJobs(t.Context(), f.tokens[0], f.collection.UUID, item.policy, item.extractor, 0, 5)
		require.NoError(t, err)
		require.Empty(t, page)
	}
	running := f.claim(t, job.UUID, 0)
	require.Empty(t, ready(0))
	_, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
	require.NoError(t, err)
	require.Empty(t, ready(0))
	queued, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	f.now = queued.AvailableAt
	require.Len(t, ready(0), 1, "admitted retries remain discoverable after target admission")
	other := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other source", Kind: "feed", Namespace: "native:reddit", State: "active"}})
	_, otherToken, err := f.service.IssueCredential(t.Context(), f.producers[1].UUID, []models.IngestScope{{CollectionUUID: other.UUID}}, nil)
	require.NoError(t, err)
	_, err = f.worker.ReadyJobs(t.Context(), otherToken, f.collection.UUID, strings.Repeat("a", 64), "1.32.15-dev", 0, 5)
	require.ErrorIs(t, err, ingest.ErrForbidden)
}

func TestEnrichmentMaintenanceRecoveryRetainsEvidenceAndBackoff(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	worker := ingest.NewEnrichmentMaintenance(f.service)
	worker.Now = func() time.Time { return f.now }
	noChange, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.Equal(t, &models.EnrichmentMaintenanceResult{}, noChange)
	f.now = running.LeaseUntil.Add(time.Second)
	var group sync.WaitGroup
	errors := make(chan error, 2)
	results := make(chan *models.EnrichmentMaintenanceResult, 2)
	for range 2 {
		group.Go(func() { result, err := worker.Process(t.Context()); errors <- err; results <- result })
	}
	group.Wait()
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	require.Equal(t, 1, (<-results).Recovered+(<-results).Recovered)
	recovered, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "queued", recovered.State)
	require.Equal(t, f.now, recovered.AvailableAt)
	retained, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, *head, retained.EnrichmentCheckpointReceipt)
	_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.Error(t, err)
	_, err = f.worker.Renew(t.Context(), f.tokens[0], running.Lease(), time.Minute)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, attempts, 1)
		require.Equal(t, "expired", attempts[0].Outcome)
		return nil
	}))
}

func TestEnrichmentMaintenanceCancelsChangedSourcesWithoutDiscardingCheckpoints(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	definition := f.collection.SourceCollectionDefinition
	definition.State = "disabled"
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	worker := ingest.NewEnrichmentMaintenance(f.service)
	worker.Now = func() time.Time { return f.now }
	result, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, result.Cancelled)
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", current.State)
	ack, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	require.Equal(t, head, ack)
	result, err = worker.Process(t.Context())
	require.NoError(t, err)
	require.Equal(t, &models.EnrichmentMaintenanceResult{}, result)
}

func TestEnrichmentMaintenanceRollbackPreservesAttemptWhenWriteFailureIsCaught(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	f.now = running.LeaseUntil.Add(time.Second)
	raw := openRawDB(t, f.db.DatabasePath())
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	_, err := raw.Exec("CREATE TRIGGER refuse_enrichment_recovery BEFORE UPDATE ON archive_jobs BEGIN SELECT RAISE(ABORT,'fixture'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.EnrichmentJob.Maintain(ctx, f.now)
		require.Error(t, err)
		return nil // application must not commit the ended attempt on its own
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
		require.NoError(t, err)
		require.Equal(t, "running", attempts[0].Outcome)
		return nil
	}))
}
