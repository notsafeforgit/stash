package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestCanonicalPostMergeEnrichmentKeepsOriginalEvidenceAndCommittedReceipt(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
	require.NoError(t, err)
	survivor := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "reviewed-survivor"}, "")
	sqlite.ConsolidatePostBackfillFixture(t, f.repo, f.target.PostUUID, survivor.UUID, "")
	publication, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err, "retained source acquisition can finish under its original owner after a reviewed post merge")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		records, err := f.repo.EnrichmentJob.PublishedRecords(ctx, job.UUID, -1, 100)
		require.NoError(t, err)
		require.Len(t, records, publication.RecordCount)
		for _, record := range records {
			capture, err := f.repo.SourceEvidence.FindCapture(ctx, record.CaptureUUID)
			require.NoError(t, err)
			require.Equal(t, f.target.PostUUID, capture.PostUUID)
			identity, err := f.repo.SourceEvidence.PostIdentity(ctx, capture.PostUUID)
			require.NoError(t, err)
			require.Equal(t, survivor.UUID, identity.CanonicalUUID)
		}
		return nil
	}))
	final := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "later-survivor"}, "")
	sqlite.ConsolidatePostBackfillFixture(t, f.repo, survivor.UUID, final.UUID, "")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	f.worker.Service = ingest.New(f.repo)
	replayed, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	require.Equal(t, publication, replayed)
}
