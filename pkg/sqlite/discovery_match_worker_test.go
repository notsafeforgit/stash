package sqlite_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryComparisonWorkerResumesRetainedPagesWithoutSourceJobs(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	worker := ingest.NewDiscoveryComparisonWorker(ingest.New(f.repo))
	worker.Now = func() time.Time { return f.now }
	result, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.Nil(t, result.Receipt)
	f.append(t, f.page)
	result, err = worker.Process(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, result.Receipt.PageOrdinal)
	require.False(t, result.Receipt.Complete)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	worker = ingest.NewDiscoveryComparisonWorker(ingest.New(f.repo))
	worker.Now = func() time.Time { return f.now }
	result, err = worker.Process(t.Context())
	require.NoError(t, err)
	require.Nil(t, result.Receipt, "a restart does not repeat already compared pages")
	f.append(t, listingContinuation(t, f.page, map[string]string{"after": "t3_abc123"}, nil, true))
	result, err = worker.Process(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, result.Receipt.PageOrdinal)
	require.True(t, result.Receipt.Complete)
	result, err = worker.Process(t.Context())
	require.NoError(t, err)
	require.Nil(t, result.Receipt)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE kind='account.list_page'"), "only producer admission created source jobs")
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_match_candidates"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM discovery_match_pages"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_discovery_resolutions"))
}

func TestDiscoveryComparisonReadinessBoundsEmptyPagesAndSkipsStaleTargets(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	targets := []*models.DiscoveryMatchTarget{f.target}
	listings := map[string]*models.DiscoveryListing{f.target.UUID: f.listing}
	for i := 0; i < 34; i++ {
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
	f.target = targets[len(targets)-1]
	f.listing = listings[f.target.UUID]
	f.append(t, f.page)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		first, err := f.repo.DiscoveryMatch.Pending(ctx, "", 32, f.now)
		require.NoError(t, err)
		require.Empty(t, first.Targets)
		require.True(t, first.HasMore, "waiting targets must still advance the bounded inspection cursor")
		require.Equal(t, targets[31].UUID, first.After)
		last, err := f.repo.DiscoveryMatch.Pending(ctx, first.After, 32, f.now)
		require.NoError(t, err)
		require.Len(t, last.Targets, 1)
		require.Equal(t, f.target.UUID, last.Targets[0].UUID)
		require.False(t, last.HasMore)
		return nil
	}))
	worker := ingest.NewDiscoveryComparisonWorker(ingest.New(f.repo))
	worker.Now = func() time.Time { return f.now }
	first, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.True(t, first.HasMore)
	require.Nil(t, first.Receipt)
	last, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.Equal(t, f.target.UUID, last.Receipt.TargetUUID)
	f.append(t, listingContinuation(t, f.page, map[string]string{"after": "t3_abc123"}, nil, true))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_posts SET revision=revision+1 WHERE uuid=?", f.target.PostUUID)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		result, err := worker.Process(t.Context())
		require.NoError(t, err)
		require.Nil(t, result.Receipt, "native edits invalidate pending comparison without deleting prior evidence")
	}
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_match_pages"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_match_candidates"))
}

func TestDiscoveryComparisonWorkerStopsDuringIdleAndResumesAfterCancellation(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	f.append(t, f.page)
	worker := ingest.NewDiscoveryComparisonWorker(ingest.New(f.repo))
	worker.Now = func() time.Time { return f.now }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	require.Eventually(t, func() bool {
		var target *models.DiscoveryMatchTarget
		err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			target, err = f.repo.DiscoveryMatch.Target(ctx, f.target.UUID)
			return err
		})
		return err == nil && target.LastPage == 1
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop while waiting for source data")
	}
	f.append(t, listingContinuation(t, f.page, map[string]string{"after": "t3_abc123"}, nil, true))
	worker = ingest.NewDiscoveryComparisonWorker(ingest.New(f.repo))
	worker.Now = func() time.Time { return f.now }
	result, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, result.Receipt.PageOrdinal)
	require.True(t, result.Receipt.Complete)
}
