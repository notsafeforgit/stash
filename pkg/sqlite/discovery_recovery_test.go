package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeDiscoveryRecoverySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryDetailSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE discovery_recovery_targets; DROP TABLE discovery_listing_recoveries;
 DROP TRIGGER discovery_recovery_job_scope; DROP TRIGGER discovery_recovery_page_scope;
 DELETE FROM native_migration_history WHERE version=1000075`)
	require.NoError(t, err)
}

func discoveryRecoveryInput(f *discoveryMatchFixture) models.DiscoveryActivationInput {
	input := discoveryActivationInput(f)
	input.Listing.RecoveryOf = &models.DiscoveryListingRecovery{ListingUUID: f.listing.UUID, SHA256: f.listing.Digest}
	input.Listing.InitialCursor, input.Listing.HistoricalPages = nil, 0
	if input.Listing.NotBefore.Before(f.now) {
		input.Listing.NotBefore = f.now
	}
	return input
}

func recoverDiscoveryFixture(t *testing.T, f *discoveryMatchFixture) *models.DiscoveryActivation {
	t.Helper()
	plan := previewDiscoveryActivation(t, f, discoveryRecoveryInput(f))
	receipt, err := activateDiscoveryPlan(t, f, plan)
	require.NoError(t, err)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.listing, err = f.repo.DiscoveryJob.Listing(ctx, receipt.Input.Listing.UUID)
		if err != nil {
			return err
		}
		f.target, err = f.repo.DiscoveryMatch.Target(ctx, receipt.Entries[0].TargetUUID)
		return err
	}))
	return receipt
}

func TestDiscoveryRecoveryPreservesHistoryAndPublishesFreshCoverage(t *testing.T) {
	f := newDiscoveryMatchHistoryFixture(t, true)
	previousListing, previousTarget := f.listing, f.target
	f.append(t, discoveryReviewFinalPage(t, f.page, previousListing.InitialCursor, false))
	f.advance(t, 0)
	require.Contains(t, f.review(t).Blockers, "history_not_retained")
	receipt := recoverDiscoveryFixture(t, f)
	// A producer can recover a lost acknowledgement for its already retained
	// page after replacement, without admitting new work on the old search.
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		page, err := f.repo.DiscoveryJob.Page(ctx, previousListing.UUID, 1)
		if err != nil {
			return err
		}
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, page.JobUUID, page.Fence-1, 1)
		if err != nil {
			return err
		}
		replayed, err := f.repo.DiscoveryJob.AppendPage(ctx, models.DiscoveryJobLease{
			ArchiveJobLease: models.ArchiveJobLease{JobUUID: page.JobUUID, Fence: page.Fence, OwnerUUID: attempts[0].OwnerUUID}, ProducerUUID: f.producer.UUID,
		}, 1, page.Body, f.now)
		require.NoError(t, err)
		require.Equal(t, page.DiscoveryPageReceipt, *replayed)
		return nil
	}))
	review := f.review(t)
	require.Equal(t, previousTarget.UUID, review.RecoveryFrom.TargetUUID)
	require.Equal(t, 1, review.RecoveryFrom.CandidateCount)
	require.False(t, review.Coverage.Complete)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		original, err := f.repo.DiscoveryJob.Listing(ctx, previousListing.UUID)
		require.NoError(t, err)
		require.Equal(t, previousListing, original)
		earlier, err := f.repo.DiscoveryMatch.Review(ctx, previousTarget.UUID)
		require.NoError(t, err)
		require.Contains(t, earlier.Blockers, "search_replaced")
		require.Equal(t, f.listing.UUID, earlier.ReplacementListingUUID)
		return nil
	}))
	f.append(t, discoveryReviewFinalPage(t, f.page, nil, false))
	f.advance(t, 0)
	review = f.review(t)
	require.True(t, review.Coverage.Complete)
	require.Empty(t, review.Blockers)
	worker := ingest.NewDiscoveryPublicationWorker(ingest.New(f.repo))
	worker.Now = func() time.Time { return f.now }
	result, err := worker.Process(t.Context())
	require.NoError(t, err)
	require.NotNil(t, result.Publication)
	require.Equal(t, previousTarget.PostUUID, result.Publication.PostUUID)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	replayed, err := activateDiscoveryPlan(t, f, &receipt.DiscoveryActivationPlan)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed, "lost responses recover after publication changed the native post revision")
	require.Equal(t, result.Publication, f.review(t).Publication)
}

func TestDiscoveryRecoveryCannotDiscardEarlierCandidatesOrUncomparedPages(t *testing.T) {
	for _, competing := range []bool{false, true} {
		t.Run(fmt.Sprint(competing), func(t *testing.T) {
			f := newDiscoveryMatchHistoryFixture(t, true)
			previousListing, previousTarget := f.listing, f.target
			f.append(t, discoveryReviewFinalPage(t, f.page, previousListing.InitialCursor, competing))
			recoverDiscoveryFixture(t, f)
			f.append(t, discoveryReviewFinalPage(t, f.page, nil, false))
			f.advance(t, 0)
			require.Contains(t, f.review(t).Blockers, "earlier_comparison_pending")
			worker := ingest.NewDiscoveryPublicationWorker(ingest.New(f.repo))
			worker.Now = func() time.Time { return f.now }
			result, err := worker.Process(t.Context())
			require.NoError(t, err)
			require.Nil(t, result.Publication)
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.DiscoveryMatch.Advance(ctx, previousTarget.UUID, 0, f.now)
				return err
			}))
			review := f.review(t)
			require.Zero(t, review.RecoveryFrom.UncomparedPages)
			if competing {
				require.Equal(t, 1, review.RecoveryFrom.ConflictingCandidates)
				require.Contains(t, review.Blockers, "earlier_candidates_differ")
			} else {
				require.Empty(t, review.Blockers)
			}
			result, err = worker.Process(t.Context())
			require.NoError(t, err)
			require.Equal(t, !competing, result.Publication != nil)
		})
	}
}

func TestDiscoveryRecoveryCancelsQueuedWorkButWaitsForRunningProducer(t *testing.T) {
	f := newDiscoveryMatchHistoryFixture(t, true)
	previous := f.listing
	var job *models.ArchiveJob
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		job, err = f.repo.DiscoveryJob.Admit(ctx, previous.UUID, f.now)
		return err
	}))
	plan := previewDiscoveryActivation(t, f, discoveryRecoveryInput(f))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		job, err = f.repo.ArchiveJob.ClaimByID(ctx, job.UUID, job.Revision, uuid.NewString(), f.now, time.Minute)
		if err != nil {
			return err
		}
		return f.repo.DiscoveryJob.BindAttempt(ctx, models.DiscoveryJobLease{ArchiveJobLease: job.Lease(), ProducerUUID: f.producer.UUID}, f.now)
	}))
	_, err := activateDiscoveryPlan(t, f, plan)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_listing_recoveries"))
	f.now = f.now.Add(2 * time.Minute)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		result, err := f.repo.DiscoveryJob.Maintain(ctx, f.now)
		require.NoError(t, err)
		require.Equal(t, 1, result.Recovered)
		return err
	}))
	// Preserve the recovered attempt's backoff instead of bypassing it.
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.DiscoveryJob.Job(ctx, previous.UUID)
		require.NoError(t, err)
		plan.Input.Listing.NotBefore = current.AvailableAt
		return nil
	}))
	plan = previewDiscoveryActivation(t, f, plan.Input)
	receipt, err := activateDiscoveryPlan(t, f, plan)
	require.NoError(t, err)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		prior, err := f.repo.DiscoveryJob.Job(ctx, previous.UUID)
		require.NoError(t, err)
		require.Equal(t, "cancelled", prior.State)
		_, err = f.repo.DiscoveryJob.CheckListing(ctx, previous.UUID, f.now.Add(time.Hour))
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		ready, err := f.repo.DiscoveryJob.ReadyListings(ctx, previous.CollectionUUID, previous.PolicySHA256, previous.ExtractorVersion, "", 100, f.now.Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, ready.Listings, 1)
		require.Equal(t, receipt.Input.Listing.UUID, ready.Listings[0].UUID)
		return nil
	}))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryJob.Retry(ctx, previous.UUID, job.UUID, f.now.Add(time.Hour))
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		return nil
	}))
}

func TestDiscoveryRecoveryRollsBackCaughtFailureAndRejectsAnotherReplacement(t *testing.T) {
	f := newDiscoveryMatchHistoryFixture(t, true)
	plan := previewDiscoveryActivation(t, f, discoveryRecoveryInput(f))
	var queued *models.ArchiveJob
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		queued, err = f.repo.DiscoveryJob.Admit(ctx, f.listing.UUID, f.now)
		return err
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("CREATE TRIGGER inject_recovery_failure BEFORE INSERT ON discovery_recovery_targets BEGIN SELECT RAISE(ABORT,'injected recovery failure'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryMatch.Activate(ctx, plan.Input, plan.PlanSHA256, f.now)
		require.ErrorContains(t, err, "injected recovery failure")
		return nil // a caller catching the error cannot commit partial recovery
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_listing_recoveries"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_recovery_targets"))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		original, err := f.repo.ArchiveJob.Find(ctx, queued.UUID)
		require.NoError(t, err)
		require.Equal(t, queued, original, "rollback restores the original queued attempt too")
		return nil
	}))
	_, err = raw.Exec("DROP TRIGGER inject_recovery_failure")
	require.NoError(t, err)
	_, err = activateDiscoveryPlan(t, f, plan)
	require.NoError(t, err)
	another := discoveryRecoveryInput(f)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryMatch.PreviewActivation(ctx, another)
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		return nil
	}))
}

func TestDiscoveryRecoveryCannotChangeOriginalAccountOrEraseHistoryWithoutReference(t *testing.T) {
	f := newDiscoveryMatchHistoryFixture(t, true)
	for name, change := range map[string]func(*models.DiscoveryActivationInput){
		"hash":    func(i *models.DiscoveryActivationInput) { i.Listing.RecoveryOf.SHA256 = strings.Repeat("f", 64) },
		"account": func(i *models.DiscoveryActivationInput) { i.Listing.AccountUUID = uuid.NewString() },
		"profile": func(i *models.DiscoveryActivationInput) {
			i.Listing.ProfileURL = "https://www.reddit.com/user/another/submitted/"
		},
		"reference": func(i *models.DiscoveryActivationInput) { i.Listing.RecoveryOf = nil },
		"deadline":  func(i *models.DiscoveryActivationInput) { i.Listing.NotBefore = f.listing.NotBefore.Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			input := discoveryRecoveryInput(f)
			change(&input)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.DiscoveryMatch.PreviewActivation(ctx, input)
				require.ErrorIs(t, err, models.ErrDiscoveryConflict)
				return nil
			}))
		})
	}
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_listing_recoveries"))
}

func TestDiscoveryRecoveryLostReferencesAreRejectedByAudit(t *testing.T) {
	for _, corruption := range []string{"DELETE FROM discovery_recovery_targets", "DELETE FROM discovery_listing_recoveries"} {
		t.Run(corruption, func(t *testing.T) {
			f := newDiscoveryMatchHistoryFixture(t, true)
			recoverDiscoveryFixture(t, f)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			_, err := raw.Exec(corruption)
			require.NoError(t, err)
			require.ErrorIs(t, f.db.AuditForTesting(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
		})
	}
}

func TestDiscoveryRecoveryMigrationPreservesPublicationAndRejectsUnknownObjects(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newDiscoveryPublicationFixture(t)
			plan := previewDiscoveryActivation(t, f, discoveryActivationInput(f))
			_, err := activateDiscoveryPlan(t, f, plan)
			require.NoError(t, err)
			prepared := prepareDiscoveryPublication(t, f)
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := prepared.Publish(ctx, discoveryCaptureWriter(f), f.now)
				return err
			}))
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"discovery_activations", "discovery_activation_targets", "discovery_listings", "discovery_listing_legacy", "discovery_listing_jobs", "discovery_job_attempts", "discovery_pages", "discovery_match_targets", "discovery_match_pages", "discovery_match_candidates", "discovery_match_evidence", "discovery_match_publications", "discovery_published_records", "source_posts", "source_post_identifiers", "source_captures", "source_payloads", "archive_jobs", "archive_job_attempts", "automation_discovery_records"} {
				before[table] = albumJobRows(t, raw, table)
			}
			removeDiscoveryRecoverySchema(t, raw)
			if collision {
				// A rejected predecessor migration retains its 17-column receipt
				// layout; successful promotion adds the nullable detail reference.
				before["discovery_match_publications"] = albumJobRows(t, raw, "discovery_match_publications")
			}
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000074,dirty=0")
			require.NoError(t, err)
			if collision {
				_, err = raw.Exec("CREATE TABLE discovery_recovery_targets(original TEXT); INSERT INTO discovery_recovery_targets VALUES('preserve unknown input')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.ErrorAs(t, f.db.Open(f.db.DatabasePath()), &needed)
			if collision {
				require.Error(t, f.db.RunAllMigrations())
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM discovery_recovery_targets").Scan(&original))
				require.Equal(t, "preserve unknown input", original)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='discovery_listing_recoveries'"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}
