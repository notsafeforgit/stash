package sqlite_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func discoveryWaitingTargets(t *testing.T, f *discoveryMatchFixture) ([]*models.DiscoveryMatchTarget, map[string]*models.DiscoveryListing) {
	t.Helper()
	targets := []*models.DiscoveryMatchTarget{f.target}
	listings := map[string]*models.DiscoveryListing{f.target.UUID: f.listing}
	for range 34 {
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			definition := f.listing.DiscoveryListingInput
			definition.UUID = uuid.NewString()
			listing, err := f.repo.DiscoveryJob.CreateListing(ctx, definition, f.now)
			if err != nil {
				return err
			}
			input := f.input
			input.ListingUUID = listing.UUID
			target, err := f.repo.DiscoveryMatch.BindTarget(ctx, input, f.now)
			if err == nil {
				targets = append(targets, target)
				listings[target.UUID] = listing
			}
			return err
		}))
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].UUID < targets[j].UUID })
	return targets, listings
}

func TestDiscoveryPublicationWorkerPublishesOnceAcrossCompetingWorkersAndRestart(t *testing.T) {
	f := newDiscoveryPublicationFixture(t)
	results := make([]*ingest.DiscoveryPublicationProgress, 2)
	errors := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			worker := ingest.NewDiscoveryPublicationWorker(ingest.New(f.repo))
			worker.Now = func() time.Time { return f.now }
			results[i], errors[i] = worker.Process(t.Context())
		})
	}
	wg.Wait()
	var publication *models.DiscoveryMatchPublication
	for i, err := range errors {
		require.NoError(t, err)
		if results[i].Publication != nil {
			publication = results[i].Publication
		}
	}
	require.NotNil(t, publication)
	require.Equal(t, f.target.UUID, publication.TargetUUID)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_match_publications"))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM discovery_published_records"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_pages"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE kind='account.list_page'"), "publication does not admit source work")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	worker := ingest.NewDiscoveryPublicationWorker(ingest.New(f.repo))
	worker.Now = func() time.Time { return f.now }
	result, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.Nil(t, result.Publication, "a restarted worker does not republish completed targets")
	require.False(t, result.HasMore)
	require.Equal(t, publication, f.review(t).Publication)
}

func TestDiscoveryPublicationReadinessBoundsInspectionAndDoesNotStarveReadyTargets(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	targets, listings := discoveryWaitingTargets(t, f)
	f.target = targets[len(targets)-1]
	f.listing = listings[f.target.UUID]
	f.append(t, discoveryReviewFinalPage(t, f.page, nil, false))
	f.advance(t, 0)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		first, err := f.repo.DiscoveryMatch.PendingPublications(ctx, "", 32, f.now)
		require.NoError(t, err)
		require.Empty(t, first.Targets)
		require.True(t, first.HasMore)
		require.Equal(t, targets[31].UUID, first.After)
		last, err := f.repo.DiscoveryMatch.PendingPublications(ctx, first.After, 32, f.now)
		require.NoError(t, err)
		require.Len(t, last.Targets, 1)
		require.Equal(t, f.target.UUID, last.Targets[0].TargetUUID)
		require.False(t, last.HasMore)
		_, err = f.repo.DiscoveryMatch.PendingPublications(ctx, "bad", 32, f.now)
		require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
		_, err = f.repo.DiscoveryMatch.PendingPublications(ctx, "", 101, f.now)
		require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
		return nil
	}))
	worker := ingest.NewDiscoveryPublicationWorker(ingest.New(f.repo))
	worker.Now = func() time.Time { return f.now }
	first, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.True(t, first.HasMore)
	require.Nil(t, first.Publication)
	last, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.NotNil(t, last.Publication)
	require.Equal(t, f.target.UUID, last.Publication.TargetUUID)
	// Traversing two more batches reaches a real idle boundary, despite the
	// remaining waiting targets. It must not wrap forever with HasMore=true.
	first, err = worker.Process(t.Context())
	require.NoError(t, err)
	require.True(t, first.HasMore)
	last, err = worker.Process(t.Context())
	require.NoError(t, err)
	require.False(t, last.HasMore)
	require.Nil(t, last.Publication)
}

func TestDiscoveryWorkersReachIdleAfterAFullWaitingTraversal(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	discoveryWaitingTargets(t, f)
	comparison := ingest.NewDiscoveryComparisonWorker(ingest.New(f.repo))
	comparison.Now = func() time.Time { return f.now }
	publication := ingest.NewDiscoveryPublicationWorker(ingest.New(f.repo))
	publication.Now = func() time.Time { return f.now }
	for range 2 {
		first, err := comparison.Process(t.Context())
		require.NoError(t, err)
		require.True(t, first.HasMore)
		require.Nil(t, first.Receipt)
		last, err := comparison.Process(t.Context())
		require.NoError(t, err)
		require.False(t, last.HasMore, "an exhausted waiting traversal must allow the idle pause")
		require.Nil(t, last.Receipt)
		one, err := publication.Process(t.Context())
		require.NoError(t, err)
		require.True(t, one.HasMore)
		two, err := publication.Process(t.Context())
		require.NoError(t, err)
		require.False(t, two.HasMore)
	}
}

type discoveryPublicationInspection struct {
	models.DiscoveryMatchReaderWriter
	inspected chan struct{}
}

func (s discoveryPublicationInspection) PendingPublications(ctx context.Context, after string, limit int, now time.Time) (*models.DiscoveryPublicationCandidates, error) {
	ret, err := s.DiscoveryMatchReaderWriter.PendingPublications(ctx, after, limit, now)
	select {
	case s.inspected <- struct{}{}:
	default:
	}
	return ret, err
}

func TestDiscoveryPublicationWorkerStopsAfterInspectionAndResumesNewEvidence(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	repo := f.repo
	inspected := make(chan struct{}, 1)
	repo.DiscoveryMatch = discoveryPublicationInspection{DiscoveryMatchReaderWriter: repo.DiscoveryMatch, inspected: inspected}
	worker := ingest.NewDiscoveryPublicationWorker(ingest.New(repo))
	worker.Now = func() time.Time { return f.now }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case <-inspected:
	case <-time.After(5 * time.Second):
		t.Fatal("publication worker did not inspect waiting targets")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("publication worker did not stop")
	}
	f.append(t, discoveryReviewFinalPage(t, f.page, nil, false))
	f.advance(t, 0)
	worker = ingest.NewDiscoveryPublicationWorker(ingest.New(f.repo))
	worker.Now = func() time.Time { return f.now }
	result, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.NotNil(t, result.Publication)
}
