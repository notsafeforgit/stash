package sqlite_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func discoveryCoordinator(f *listingFixture) *ingest.DiscoveryCoordinator {
	w := ingest.NewDiscoveryCoordinator(f.service)
	w.Now = func() time.Time { return f.now }
	return w
}

func TestDiscoveryDispatchBoundsInspectedDefinitionsAndKeepsReadinessReadOnly(t *testing.T) {
	f := newListingFixture(t)
	w := discoveryCoordinator(f)
	expected := []string{f.listing.UUID}
	definitions := []string{f.listing.UUID}
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for i := range 27 {
			input := f.listing.DiscoveryListingInput
			input.UUID = fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
			definitions = append(definitions, input.UUID)
			switch {
			case i < 3:
				input.PolicySHA256 = strings.Repeat("b", 64)
			case i%3 == 0:
				input.NotBefore = f.now.Add(time.Hour)
			case i%3 == 1:
				input.ExtractorVersion = "other-runtime"
			default:
				expected = append(expected, input.UUID)
			}
			if _, err := f.repo.DiscoveryJob.CreateListing(ctx, input, f.now); err != nil {
				return err
			}
		}
		return nil
	}))
	sort.Strings(definitions)
	sort.Strings(expected)
	after := ""
	var seen []string
	for offset := 0; ; offset += 2 {
		page, err := w.ReadyListings(t.Context(), f.tokens[0], f.collection.UUID, f.listing.PolicySHA256, f.listing.ExtractorVersion, after, 2)
		require.NoError(t, err)
		// The cursor moves at most two stored definitions, including rejected
		// policies and future deadlines; filtering cannot scan the whole table.
		last := min(offset+2, len(definitions))
		require.Equal(t, definitions[last-1], page.After)
		require.Equal(t, last < len(definitions), page.HasMore)
		if offset == 0 {
			require.Empty(t, page.Listings)
			require.True(t, page.HasMore)
		}
		for _, item := range page.Listings {
			seen = append(seen, item.UUID)
			require.Len(t, item.Digest, 64)
		}
		after = page.After
		if !page.HasMore {
			break
		}
	}
	require.Equal(t, expected, seen)
	last, err := w.ReadyListings(t.Context(), f.tokens[0], f.collection.UUID, f.listing.PolicySHA256, f.listing.ExtractorVersion, after, 2)
	require.NoError(t, err)
	require.Equal(t, after, last.After)
	require.Empty(t, last.Listings)
	require.False(t, last.HasMore)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, id := range definitions {
			job, err := f.repo.DiscoveryJob.Job(ctx, id)
			require.NoError(t, err)
			require.Nil(t, job, "enumeration must not admit work")
		}
		return nil
	}))
}

func TestDiscoveryDispatchResumesPagesAndFindsOnlyDueScopedJobs(t *testing.T) {
	f := newListingFixture(t)
	w := discoveryCoordinator(f)
	listings := func() []models.DiscoveryListingCandidate {
		page, err := w.ReadyListings(t.Context(), f.tokens[0], f.collection.UUID, f.listing.PolicySHA256, f.listing.ExtractorVersion, "", 10)
		require.NoError(t, err)
		return page.Listings
	}
	ready := func(after int64) []models.DiscoveryJobCandidate {
		page, err := w.ReadyJobs(t.Context(), f.tokens[0], f.collection.UUID, f.listing.PolicySHA256, f.listing.ExtractorVersion, after, 10)
		require.NoError(t, err)
		return page
	}
	require.Equal(t, []models.DiscoveryListingCandidate{{UUID: f.listing.UUID, Digest: f.listing.Digest}}, listings())
	job := f.admit(t)
	require.Empty(t, listings())
	require.Equal(t, []models.DiscoveryJobCandidate{{UUID: job.UUID, Sequence: job.Sequence}}, ready(0))
	require.Empty(t, ready(job.Sequence))
	for _, profile := range []struct{ policy, runtime string }{{strings.Repeat("b", 64), f.listing.ExtractorVersion}, {f.listing.PolicySHA256, "other"}} {
		page, err := w.ReadyJobs(t.Context(), f.tokens[0], f.collection.UUID, profile.policy, profile.runtime, 0, 10)
		require.NoError(t, err)
		require.Empty(t, page)
	}
	other := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other feed", Kind: "feed", Namespace: "native:reddit", State: "active"}})
	_, token, err := f.service.IssueCredential(t.Context(), f.producers[1].UUID, []models.IngestScope{{CollectionUUID: other.UUID}}, nil)
	require.NoError(t, err)
	_, err = w.ReadyJobs(t.Context(), token, f.collection.UUID, f.listing.PolicySHA256, f.listing.ExtractorVersion, 0, 10)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	_, err = w.ReadyListings(t.Context(), token, f.collection.UUID, f.listing.PolicySHA256, f.listing.ExtractorVersion, "", 10)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	running := f.claim(t, 0)
	require.Empty(t, ready(0))
	_, err = w.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
	require.NoError(t, err)
	require.Empty(t, ready(0))
	require.Empty(t, listings(), "retry uses the already admitted job")
	queued, err := w.Describe(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	f.now = queued.Job.AvailableAt
	require.Len(t, ready(0), 1)
	running = f.claim(t, 0)
	_, err = f.append(t, models.DiscoveryJobLease{ArchiveJobLease: running.Lease(), ProducerUUID: f.producers[0].UUID}, 1, f.page)
	require.NoError(t, err)
	require.Len(t, listings(), 1, "a nonfinal receipt allows the next page")
	require.Empty(t, ready(0))
	f.now = f.now.Add(2 * time.Minute)
	f.admit(t)
	running = f.claim(t, 0)
	final := listingContinuation(t, f.page, map[string]string{"after": "t3_abc123"}, nil, true)
	_, err = f.append(t, models.DiscoveryJobLease{ArchiveJobLease: running.Lease(), ProducerUUID: f.producers[0].UUID}, 2, final)
	require.NoError(t, err)
	require.Empty(t, listings(), "a complete listing stays complete")
	require.Empty(t, ready(0))
}

func TestDiscoveryDispatchDoesNotReviveFailedCancelledOrChangedListings(t *testing.T) {
	for _, state := range []string{"failed", "cancelled", "changed", "future"} {
		t.Run(state, func(t *testing.T) {
			f := newListingFixture(t)
			w := discoveryCoordinator(f)
			if state == "future" {
				input := f.listing.DiscoveryListingInput
				input.UUID, input.NotBefore = uuid.NewString(), f.now.Add(time.Hour)
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					var err error
					f.listing, err = f.repo.DiscoveryJob.CreateListing(ctx, input, f.now)
					return err
				}))
			}
			job := f.admit(t)
			switch state {
			case "failed":
				running := f.claim(t, 0)
				_, err := w.Fail(t.Context(), f.tokens[0], running.Lease(), "authentication")
				require.NoError(t, err)
			case "cancelled":
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := f.repo.ArchiveJob.Cancel(ctx, job.UUID, job.Revision, f.now)
					return err
				}))
			case "changed":
				definition := f.collection.SourceCollectionDefinition
				definition.Label = "Revised definition"
				putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
			}
			page, err := w.ReadyJobs(t.Context(), f.tokens[0], f.collection.UUID, f.listing.PolicySHA256, f.listing.ExtractorVersion, 0, 10)
			require.NoError(t, err)
			require.Empty(t, page)
			listings, err := w.ReadyListings(t.Context(), f.tokens[0], f.collection.UUID, f.listing.PolicySHA256, f.listing.ExtractorVersion, "", 10)
			require.NoError(t, err)
			for _, item := range listings.Listings {
				require.NotEqual(t, f.listing.UUID, item.UUID)
			}
			maintenance := ingest.NewDiscoveryMaintenance(f.service)
			maintenance.Now = func() time.Time { return f.now }
			result, err := maintenance.Process(t.Context())
			require.NoError(t, err)
			if state == "changed" {
				require.Equal(t, 1, result.Cancelled)
			} else {
				require.Equal(t, &models.DiscoveryMaintenanceResult{}, result)
			}
		})
	}
}

func TestDiscoveryMaintenanceRecoversOnceAndRetainsPagesAndBackoff(t *testing.T) {
	f := newListingFixture(t)
	w := discoveryCoordinator(f)
	f.admit(t)
	first := f.claim(t, 0)
	one := models.DiscoveryJobLease{ArchiveJobLease: first.Lease(), ProducerUUID: f.producers[0].UUID}
	receipt, err := f.append(t, one, 1, f.page)
	require.NoError(t, err)
	f.now = f.now.Add(2 * time.Minute)
	job := f.admit(t)
	running := f.claim(t, 0)
	worker := ingest.NewDiscoveryMaintenance(f.service)
	worker.Now = func() time.Time { return f.now }
	result, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.Equal(t, &models.DiscoveryMaintenanceResult{}, result)
	f.now = running.LeaseUntil.Add(time.Second)
	var group sync.WaitGroup
	errors := make(chan error, 2)
	results := make(chan *models.DiscoveryMaintenanceResult, 2)
	for range 2 {
		group.Go(func() { value, err := worker.Process(t.Context()); errors <- err; results <- value })
	}
	group.Wait()
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	require.Equal(t, 1, (<-results).Recovered+(<-results).Recovered)
	current, err := w.Describe(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "queued", current.Job.State)
	require.Equal(t, f.now, current.Job.AvailableAt)
	require.Equal(t, map[string]string{"after": "t3_abc123"}, current.Cursor)
	_, err = w.Renew(t.Context(), f.tokens[0], running.Lease(), time.Minute)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	replayed, err := f.append(t, one, 1, f.page)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, attempts, 1)
		require.Equal(t, "expired", attempts[0].Outcome)
		bound, err := f.repo.DiscoveryJob.Attempt(ctx, job.UUID, running.Fence)
		require.NoError(t, err)
		require.Equal(t, f.producers[0].UUID, bound.ProducerUUID)
		return nil
	}))
	definition := f.collection.SourceCollectionDefinition
	definition.State = "disabled"
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	result, err = worker.Process(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, result.Cancelled)
	replayed, err = f.append(t, one, 1, f.page)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed, "stale queued work cannot erase prior acknowledgements")
}

func TestDiscoveryMaintenanceCaughtWriteFailureRollsBackAttempt(t *testing.T) {
	f := newListingFixture(t)
	job := f.admit(t)
	running := f.claim(t, 0)
	f.now = running.LeaseUntil.Add(time.Second)
	raw := openRawDB(t, f.db.DatabasePath())
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	_, err := raw.Exec("CREATE TRIGGER refuse_discovery_recovery BEFORE UPDATE ON archive_jobs BEGIN SELECT RAISE(ABORT,'fixture'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryJob.Maintain(ctx, f.now)
		require.Error(t, err)
		return nil
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
		require.NoError(t, err)
		require.Equal(t, "running", attempts[0].Outcome)
		return nil
	}))
}

func TestDiscoveryMaintenanceCancelsMergedAccountsAndDisabledRoots(t *testing.T) {
	for _, cause := range []string{"account_merge", "disabled_root"} {
		t.Run(cause, func(t *testing.T) {
			f := newListingFixture(t)
			var root *models.MediaRoot
			if cause == "disabled_root" {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					var err error
					root, err = f.repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Feed root", State: "active"}})
					return err
				}))
				definition := f.collection.SourceCollectionDefinition
				definition.RootUUID = &root.UUID
				definition.PathPrefix = "."
				f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
				input := f.listing.DiscoveryListingInput
				input.UUID, input.RootUUID, input.CollectionRevision = uuid.NewString(), &root.UUID, f.collection.Revision
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					var err error
					f.listing, err = f.repo.DiscoveryJob.CreateListing(ctx, input, f.now)
					return err
				}))
			}
			job := f.admit(t)
			f.claim(t, 0)
			if cause == "account_merge" {
				destination := createSourceAccount(t, f.repo, "native:reddit")
				preview := accountPreview(t, f.repo, f.listing.AccountUUID, destination.UUID)
				_, err := consolidateAccount(f.repo, consolidationInput(preview))
				require.NoError(t, err)
			} else {
				definition := root.MediaRootDefinition
				definition.State = "disabled"
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := f.repo.MediaRoot.Put(ctx, models.MediaRootInput{UUID: root.UUID, ExpectedRevision: root.Revision, Origin: "review", MediaRootDefinition: definition})
					return err
				}))
			}
			worker := ingest.NewDiscoveryMaintenance(f.service)
			worker.Now = func() time.Time { return f.now }
			result, err := worker.Process(t.Context())
			require.NoError(t, err)
			require.Equal(t, &models.DiscoveryMaintenanceResult{Cancelled: 1}, result)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				current, err := f.repo.ArchiveJob.Find(ctx, job.UUID)
				require.NoError(t, err)
				require.Equal(t, "cancelled", current.State)
				attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
				require.NoError(t, err)
				require.Len(t, attempts, 1)
				require.Equal(t, "cancelled", attempts[0].Outcome)
				return nil
			}))
		})
	}
}
