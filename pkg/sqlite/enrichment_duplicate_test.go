package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryDuplicatePublicationKeepsFirstIdentityAndCompletesMergedJobs(t *testing.T) {
	for _, mode := range []string{"legacy_first", "native_first", "already_merged"} {
		t.Run(mode, func(t *testing.T) {
			f := newDiscoveryExecutionFixture(t, nil)
			existing := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			first, second := f.target.PostUUID, existing.UUID
			if mode != "legacy_first" {
				first, second = second, first
			}
			_, err := raw.Exec("UPDATE source_posts SET created_at='2026-10-01T00:00:00Z' WHERE uuid=?", first)
			require.NoError(t, err)
			_, err = raw.Exec("UPDATE source_posts SET created_at='2026-10-02T00:00:00Z' WHERE uuid=?", second)
			require.NoError(t, err)
			if mode == "already_merged" {
				sqlite.ConsolidatePostBackfillFixture(t, f.repo, f.target.PostUUID, existing.UUID, "")
			}
			identifiers := albumJobRows(t, raw, "source_post_identifiers")
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			publication, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			require.Equal(t, 2, publication.CaptureCount)
			require.Equal(t, identifiers, albumJobRows(t, raw, "source_post_identifiers"), "original identifiers and captures keep their owners")
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				for _, id := range []string{f.target.PostUUID, existing.UUID} {
					identity, err := f.repo.SourceEvidence.PostIdentity(ctx, id)
					require.NoError(t, err)
					require.Equal(t, first, identity.CanonicalUUID)
				}
				target, err := f.repo.EnrichmentWork.Target(ctx, f.target.UUID)
				require.NoError(t, err)
				require.Equal(t, "completed", target.State)
				return nil
			}))
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_post_consolidations"))
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.AuditForTesting(f.db.DatabasePath()))
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			f.worker.Service = ingest.New(f.db.Repository())
			replay, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			require.Equal(t, publication, replay)
		})
	}
}

func TestDiscoveryDuplicatePublicationFailureRollsBackConsolidation(t *testing.T) {
	f := newDiscoveryExecutionFixture(t, nil)
	sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := albumJobRows(t, raw, "source_post_identities")
	_, err = raw.Exec("CREATE TRIGGER reject_duplicate_publication BEFORE INSERT ON enrichment_published_records WHEN NEW.ordinal=1 BEGIN SELECT RAISE(ABORT,'injected publication failure'); END")
	require.NoError(t, err)
	_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.Error(t, err)
	require.Equal(t, before, albumJobRows(t, raw, "source_post_identities"))
	for _, table := range []string{"source_post_consolidations", "post_consolidation_reviews", "enrichment_publications"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	retained, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, head.Digest, retained.Digest)
}
