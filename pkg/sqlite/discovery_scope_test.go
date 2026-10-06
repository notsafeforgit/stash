package sqlite_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func changedDiscoveryScope(t *testing.T, f *discoveryMatchFixture, state string) (*models.SourceCollection, *models.MediaRoot) {
	t.Helper()
	var collection *models.SourceCollection
	var root *models.MediaRoot
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = f.repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Reviewed media", State: state}})
		if err != nil {
			return err
		}
		collection, err = f.repo.SourceCollection.Find(ctx, f.listing.CollectionUUID)
		if err != nil {
			return err
		}
		definition := collection.SourceCollectionDefinition
		definition.RootUUID, definition.PathPrefix = &root.UUID, "account"
		collection, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	return collection, root
}

func previewDiscoveryScope(t *testing.T, f *discoveryMatchFixture, collection *models.SourceCollection) *models.DiscoveryScopePlan {
	t.Helper()
	var plan *models.DiscoveryScopePlan
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		plan, err = f.repo.DiscoveryJob.PreviewScope(ctx, models.DiscoveryScopeInput{UUID: uuid.NewString(), ListingUUID: f.listing.UUID, ExpectedDefinitionSHA256: f.listing.Digest, CollectionRevision: collection.Revision, Reason: "Review the media-root association"})
		return err
	}))
	return plan
}

func applyDiscoveryScope(t *testing.T, f *discoveryMatchFixture, plan *models.DiscoveryScopePlan) (*models.DiscoveryScopeReview, error) {
	t.Helper()
	var result *models.DiscoveryScopeReview
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.DiscoveryJob.ReviewScope(ctx, plan.Input, plan.PlanSHA256, f.now)
		return err
	})
	return result, err
}

func refreshDiscoveryScope(t *testing.T, f *discoveryMatchFixture) {
	t.Helper()
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.listing, err = f.repo.DiscoveryJob.Listing(ctx, f.listing.UUID)
		return err
	}))
}

func TestDiscoveryScopePreservesOriginalAndUsesReviewedWorkerGrant(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	original := *f.listing
	service := ingest.New(f.repo)
	_, oldToken, err := service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: f.listing.CollectionUUID}}, nil)
	require.NoError(t, err)
	collection, root := changedDiscoveryScope(t, f, "disabled")
	plan := previewDiscoveryScope(t, f, collection)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_scope_reviews"), "preview is read-only")
	receipt, err := applyDiscoveryScope(t, f, plan)
	require.NoError(t, err)
	refreshDiscoveryScope(t, f)
	require.Equal(t, plan.DefinitionSHA256, f.listing.Digest)
	require.Equal(t, original.CreatedAt, f.listing.CreatedAt)
	require.Equal(t, original.Legacy, f.listing.Legacy)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		replay, err := f.repo.DiscoveryJob.CreateListing(ctx, original.DiscoveryListingInput, f.now)
		require.NoError(t, err)
		require.Equal(t, original, *replay)
		_, err = f.repo.DiscoveryJob.Admit(ctx, f.listing.UUID, f.now)
		require.ErrorIs(t, err, models.ErrDiscoveryConflict, "a reviewed disabled root cannot fetch")
		return nil
	}))
	worker := ingest.NewDiscoveryCoordinator(service)
	worker.Now = func() time.Time { return f.now }
	_, err = worker.Admit(t.Context(), oldToken, f.listing.UUID, f.listing.Digest, f.listing.PolicySHA256, f.listing.ExtractorVersion)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := root.MediaRootDefinition
		definition.State = "active"
		_, err := f.repo.MediaRoot.Put(ctx, models.MediaRootInput{UUID: root.UUID, ExpectedRevision: root.Revision, Origin: "review", MediaRootDefinition: definition})
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), f.producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	_, err = worker.Admit(t.Context(), token, f.listing.UUID, original.Digest, original.PolicySHA256, original.ExtractorVersion)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	_, err = worker.Admit(t.Context(), token, f.listing.UUID, f.listing.Digest, f.listing.PolicySHA256, f.listing.ExtractorVersion)
	require.NoError(t, err)
	f.append(t, discoveryReviewFinalPage(t, f.page, nil, false))
	f.advance(t, 0)
	publication := ingest.NewDiscoveryPublicationWorker(service)
	publication.Now = func() time.Time { return f.now }
	progress, err := publication.Process(t.Context())
	require.NoError(t, err)
	require.NotNil(t, progress.Publication)
	require.EqualValues(t, 3, queryUint(t, raw, fmt.Sprintf("SELECT count(*) FROM source_collection_captures WHERE collection_revision=%d AND capture_uuid IN (SELECT capture_uuid FROM discovery_published_records)", collection.Revision)))
	var originalDigest string
	require.NoError(t, raw.QueryRow("SELECT digest FROM discovery_listings WHERE uuid=?", original.UUID).Scan(&originalDigest))
	require.Equal(t, original.Digest, originalDigest)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	replayed, err := applyDiscoveryScope(t, f, plan)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed, "receipt recovery remains valid after worker history and publication")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestDiscoveryScopeKeepsRecoveryHistoryAndActivationReceipts(t *testing.T) {
	for _, reviewFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(reviewFirst), func(t *testing.T) {
			f := newDiscoveryMatchHistoryFixture(t, true)
			original := *f.listing
			var activation *models.DiscoveryActivation
			if !reviewFirst {
				activation = recoverDiscoveryFixture(t, f)
			}
			collection, _ := changedDiscoveryScope(t, f, "active")
			plan := previewDiscoveryScope(t, f, collection)
			receipt, err := applyDiscoveryScope(t, f, plan)
			require.NoError(t, err)
			refreshDiscoveryScope(t, f)
			if reviewFirst {
				activation = recoverDiscoveryFixture(t, f)
			}
			f.append(t, discoveryReviewFinalPage(t, f.page, nil, false))
			f.advance(t, 0)
			review := f.review(t)
			require.NotNil(t, review.RecoveryFrom)
			require.Equal(t, original.UUID, review.RecoveryFrom.ListingUUID)
			require.Empty(t, review.Blockers)
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			f.repo = f.db.Repository()
			replayed, err := applyDiscoveryScope(t, f, plan)
			require.NoError(t, err)
			require.Equal(t, receipt, replayed)
			prior, err := activateDiscoveryPlan(t, f, &activation.DiscoveryActivationPlan)
			require.NoError(t, err)
			require.Equal(t, activation, prior)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var definition string
			require.NoError(t, raw.QueryRow("SELECT definition FROM discovery_listings WHERE uuid=?", original.UUID).Scan(&definition))
			require.Contains(t, definition, "t3_prior")
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_listing_recoveries"))
		})
	}
}

func TestDiscoveryScopeRejectsStaleReviewAndAnyWorkerHistory(t *testing.T) {
	for _, attempted := range []bool{false, true} {
		t.Run(fmt.Sprint(attempted), func(t *testing.T) {
			f := newDiscoveryMatchFixture(t)
			if attempted {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					job, err := f.repo.DiscoveryJob.Admit(ctx, f.listing.UUID, f.now)
					if err != nil {
						return err
					}
					_, err = f.repo.ArchiveJob.Cancel(ctx, job.UUID, job.Revision, f.now)
					return err
				}))
			}
			collection, _ := changedDiscoveryScope(t, f, "active")
			if attempted {
				require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					rows, err := f.repo.DiscoveryJob.ScopeCandidates(ctx, collection.UUID, collection.Revision, "", 100)
					require.NoError(t, err)
					require.Len(t, rows, 1)
					require.Equal(t, "worker_history", rows[0].Disposition)
					_, err = f.repo.DiscoveryJob.PreviewScope(ctx, models.DiscoveryScopeInput{UUID: uuid.NewString(), ListingUUID: f.listing.UUID, ExpectedDefinitionSHA256: f.listing.Digest, CollectionRevision: collection.Revision})
					require.ErrorIs(t, err, models.ErrDiscoveryConflict)
					return nil
				}))
				return
			}
			plan := previewDiscoveryScope(t, f, collection)
			competing := previewDiscoveryScope(t, f, collection)
			_, err := applyDiscoveryScope(t, f, plan)
			require.NoError(t, err)
			_, err = applyDiscoveryScope(t, f, competing)
			require.ErrorIs(t, err, models.ErrDiscoveryConflict)
			changed := *plan
			changed.Input.Reason = "Different request"
			_, err = applyDiscoveryScope(t, f, &changed)
			require.ErrorIs(t, err, models.ErrDiscoveryConflict)
			refreshDiscoveryScope(t, f)
			secondCollection, _ := changedDiscoveryScope(t, f, "active")
			second := previewDiscoveryScope(t, f, secondCollection)
			require.Equal(t, plan.Input.UUID, *second.PreviousReviewUUID)
			_, err = applyDiscoveryScope(t, f, second)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			f.repo = f.db.Repository()
			_, err = applyDiscoveryScope(t, f, plan)
			require.NoError(t, err, "historical review replay survives a further binding")
		})
	}
}

func TestDiscoveryScopeDetailPublicationUsesReviewedCollection(t *testing.T) {
	match := newDiscoveryMatchFixture(t)
	collection, _ := changedDiscoveryScope(t, match, "active")
	plan := previewDiscoveryScope(t, match, collection)
	_, err := applyDiscoveryScope(t, match, plan)
	require.NoError(t, err)
	refreshDiscoveryScope(t, match)
	match.append(t, match.page)
	match.advance(t, 0)
	f := attachDetailFixture(t, match)
	completeDetailFixture(t, f)
	finishDetailListing(t, f)
	publication := publishDetailFixture(t, f)
	require.NotNil(t, publication)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestDiscoveryScopeRestoresClearedRootAndRejectsSourceChanges(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	collection, _ := changedDiscoveryScope(t, f, "active")
	first := previewDiscoveryScope(t, f, collection)
	_, err := applyDiscoveryScope(t, f, first)
	require.NoError(t, err)
	refreshDiscoveryScope(t, f)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := collection.SourceCollectionDefinition
		definition.RootUUID, definition.PathPrefix = nil, ""
		var err error
		collection, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	second := previewDiscoveryScope(t, f, collection)
	_, err = applyDiscoveryScope(t, f, second)
	require.NoError(t, err)
	refreshDiscoveryScope(t, f)
	require.Nil(t, f.listing.RootUUID, "a cleared root must not inherit the original or previous binding")
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := collection.SourceCollectionDefinition
		definition.TargetURL = "https://www.reddit.com/user/a_new_source/submitted/"
		var err error
		collection, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.DiscoveryJob.ScopeCandidates(ctx, collection.UUID, collection.Revision, "", 100)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "source_changed", rows[0].Disposition)
		_, err = f.repo.DiscoveryJob.PreviewScope(ctx, models.DiscoveryScopeInput{UUID: uuid.NewString(), ListingUUID: f.listing.UUID, ExpectedDefinitionSHA256: f.listing.Digest, CollectionRevision: collection.Revision})
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		return nil
	}))
	backup := filepath.Join(t.TempDir(), "scope-backup.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	f.repo = f.db.Repository()
	for _, plan := range []*models.DiscoveryScopePlan{first, second} {
		_, err := applyDiscoveryScope(t, f, plan)
		require.NoError(t, err)
	}
	anonPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, anonPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	raw := openRawDB(t, anonPath)
	defer raw.Close()
	for _, table := range []string{"discovery_scope_reviews", "discovery_effective_listings", "discovery_listings"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
