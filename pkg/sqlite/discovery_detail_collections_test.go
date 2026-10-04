package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryDetailCollectionsRespectScopeRuntimeAndCurrentSelection(t *testing.T) {
	f := newDetailFixture(t)
	base := models.EnrichmentCollectionQuery{
		Scopes:       []models.IngestScope{{CollectionUUID: f.listing.CollectionUUID}},
		PolicySHA256: f.input.PolicySHA256, ExtractorVersion: f.input.ExtractorVersion, Limit: 1,
	}
	read := func(q models.EnrichmentCollectionQuery) []models.EnrichmentCollectionCandidate {
		t.Helper()
		var ret []models.EnrichmentCollectionCandidate
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			ret, err = f.repo.DiscoveryDetail.Collections(ctx, q, f.now)
			return err
		}))
		return ret
	}
	require.Empty(t, read(base), "unadmitted candidates cannot start detail work through readiness")
	f.admit(t)
	require.Equal(t, []models.EnrichmentCollectionCandidate{{UUID: f.listing.CollectionUUID}}, read(base))
	foreignRoot := uuid.NewString()
	for _, change := range []func(*models.EnrichmentCollectionQuery){
		func(q *models.EnrichmentCollectionQuery) { q.PolicySHA256 = strings.Repeat("f", 64) },
		func(q *models.EnrichmentCollectionQuery) { q.ExtractorVersion = "another runtime" },
		func(q *models.EnrichmentCollectionQuery) { q.After = f.listing.CollectionUUID },
		func(q *models.EnrichmentCollectionQuery) {
			q.Scopes = []models.IngestScope{{CollectionUUID: uuid.NewString()}}
		},
		func(q *models.EnrichmentCollectionQuery) {
			q.Scopes = []models.IngestScope{{CollectionUUID: f.listing.CollectionUUID, RootUUID: &foreignRoot}}
		},
		func(q *models.EnrichmentCollectionQuery) { q.Scopes = nil; q.Roots = []string{foreignRoot} },
	} {
		changed := base
		change(&changed)
		require.Empty(t, read(changed))
	}
	changed := base
	changed.Limit = -1
	require.ErrorIs(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryDetail.Collections(ctx, changed, f.now)
		return err
	}), models.ErrDiscoveryInvalid)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		collection, err := f.repo.SourceCollection.Find(ctx, f.listing.CollectionUUID)
		if err != nil {
			return err
		}
		definition := collection.SourceCollectionDefinition
		definition.State = "disabled"
		_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	require.Empty(t, read(base), "a stale admitted job does not expose a runnable collection")
}

func TestDiscoveryDetailCollectionReadinessRetainsBackoffAndTerminalJobs(t *testing.T) {
	f := newDetailFixture(t)
	page := func() []models.EnrichmentCollectionCandidate {
		t.Helper()
		result, err := f.worker.ReadyCollections(t.Context(), f.tokens[0], f.input.PolicySHA256, f.input.ExtractorVersion, "", 20)
		require.NoError(t, err)
		return result
	}
	job := f.admit(t)
	require.Len(t, page(), 1)
	running := f.claim(t, job, 0)
	require.Empty(t, page())
	_, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "source_busy")
	require.NoError(t, err)
	require.Empty(t, page())
	f.now = f.now.Add(5 * time.Minute)
	require.Len(t, page(), 1)
	running = f.claim(t, job, 0)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "not_found")
	require.NoError(t, err)
	require.Empty(t, page(), "an ended job requires explicit retry, not discovery readmission")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, attempts, 2)
		return nil
	}))
}
