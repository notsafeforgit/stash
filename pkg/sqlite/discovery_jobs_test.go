package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func removeDiscoveryListingSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TRIGGER discovery_listing_success; DROP TRIGGER discovery_listing_pacing_bind;
 DROP INDEX archive_jobs_discovery_listing; DROP TABLE discovery_pages; DROP TABLE discovery_job_attempts;
 DROP TABLE discovery_listing_jobs; DROP TABLE discovery_listing_legacy; DROP TABLE discovery_listings;
 DELETE FROM native_migration_history WHERE version=1000071`)
	require.NoError(t, err)
}

type listingFixture struct {
	*enrichmentExecutionFixture
	listing *models.DiscoveryListing
	page    json.RawMessage
}

func newListingFixture(t *testing.T) *listingFixture {
	t.Helper()
	f := &listingFixture{enrichmentExecutionFixture: newEnrichmentExecutionFixture(t)}
	f.now = time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC)
	raw, err := os.ReadFile("../archive/testdata/discovery-pages-v1.json")
	require.NoError(t, err)
	var corpus struct {
		Pages []struct {
			Page json.RawMessage `json:"page"`
		} `json:"pages"`
	}
	require.NoError(t, json.Unmarshal(raw, &corpus))
	f.page = corpus.Pages[1].Page
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		account, err := f.repo.SourceAccount.Create(ctx, "native:reddit", "Requested feed, not depicted performer")
		if err != nil {
			return err
		}
		f.listing, err = f.repo.DiscoveryJob.CreateListing(ctx, models.DiscoveryListingInput{UUID: uuid.NewString(), AccountUUID: account.UUID,
			CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, ProfileURL: "https://www.reddit.com/user/juniper/submitted/?sort=new",
			PolicySHA256: strings.Repeat("a", 64), ExtractorVersion: "1.32.15-dev", NotBefore: f.now}, f.now)
		return err
	}))
	return f
}

func (f *listingFixture) admit(t *testing.T) *models.ArchiveJob {
	t.Helper()
	var job *models.ArchiveJob
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		job, err = f.repo.DiscoveryJob.Admit(ctx, f.listing.UUID, f.now)
		return err
	}))
	return job
}

func (f *listingFixture) claim(t *testing.T, producer int) *models.ArchiveJob {
	t.Helper()
	var job *models.ArchiveJob
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.DiscoveryJob.Job(ctx, f.listing.UUID)
		if err != nil {
			return err
		}
		job, err = f.repo.ArchiveJob.ClaimByID(ctx, current.UUID, current.Revision, uuid.NewString(), f.now, time.Minute)
		if err != nil || job == nil {
			return err
		}
		return f.repo.DiscoveryJob.BindAttempt(ctx, models.DiscoveryJobLease{ArchiveJobLease: job.Lease(), ProducerUUID: f.producers[producer].UUID}, f.now)
	}))
	return job
}

func (f *listingFixture) append(t *testing.T, lease models.DiscoveryJobLease, n int, body json.RawMessage) (*models.DiscoveryPageReceipt, error) {
	t.Helper()
	var receipt *models.DiscoveryPageReceipt
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		receipt, err = f.repo.DiscoveryJob.AppendPage(ctx, lease, n, body, f.now)
		return err
	})
	return receipt, err
}

func listingContinuation(t *testing.T, original json.RawMessage, cursor, next map[string]string, complete bool) json.RawMessage {
	t.Helper()
	value, err := archive.DecodeJSONObject(original, archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	value["records"], value["cursor"], value["next_cursor"], value["complete"] = []any{}, cursor, next, complete
	body, err := archive.EncodeSourceJSON(value)
	require.NoError(t, err)
	return body
}

func TestDiscoveryListingsResumePagesAndLostAcknowledgements(t *testing.T) {
	f := newListingFixture(t)
	job := f.admit(t)
	require.Equal(t, job, f.admit(t))
	first := f.claim(t, 0)
	require.NotNil(t, first)
	one := models.DiscoveryJobLease{ArchiveJobLease: first.Lease(), ProducerUUID: f.producers[0].UUID}
	receipt, err := f.append(t, one, 1, f.page)
	require.NoError(t, err)
	require.Equal(t, 3, receipt.RecordCount)
	require.False(t, receipt.Complete)
	f.now = f.now.Add(2 * time.Minute)
	replayed, err := f.append(t, one, 1, f.page)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	final := listingContinuation(t, f.page, map[string]string{"after": "t3_abc123"}, nil, true)
	_, err = f.append(t, one, 2, final)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict, "a job owns only its requested page")
	f.admit(t)
	expiring := f.claim(t, 0)
	f.now = f.now.Add(2 * time.Minute)
	_, err = f.append(t, models.DiscoveryJobLease{ArchiveJobLease: expiring.Lease(), ProducerUUID: f.producers[0].UUID}, 2, final)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.repo.ArchiveJob.Recover(ctx, f.now, 100); return err }))
	require.Nil(t, f.claim(t, 1), "expiry retains retry delay")
	f.now = f.now.Add(5 * time.Minute)
	second := f.claim(t, 1)
	require.NotNil(t, second)
	two := models.DiscoveryJobLease{ArchiveJobLease: second.Lease(), ProducerUUID: f.producers[1].UUID}
	last, err := f.append(t, two, 2, final)
	require.NoError(t, err)
	require.True(t, last.Complete)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	replayed, err = f.append(t, one, 1, f.page)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	replayed, err = f.append(t, two, 2, final)
	require.NoError(t, err)
	require.Equal(t, last, replayed)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		pages, err := f.repo.DiscoveryJob.Pages(ctx, f.listing.UUID, 0, 10)
		require.NoError(t, err)
		require.Equal(t, []models.DiscoveryPageReceipt{*receipt, *last}, pages)
		current, err := f.repo.DiscoveryJob.Job(ctx, f.listing.UUID)
		require.NoError(t, err)
		require.Equal(t, "succeeded", current.State)
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_captures"), "listing completion cannot publish a post match")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, path)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	checked := sqlite.NewDatabase()
	defer checked.Close()
	require.NoError(t, checked.Open(path))
}

func TestDiscoveryListingsRejectWrongCursorsOwnersAndCycles(t *testing.T) {
	f := newListingFixture(t)
	f.admit(t)
	job := f.claim(t, 0)
	lease := models.DiscoveryJobLease{ArchiveJobLease: job.Lease(), ProducerUUID: f.producers[0].UUID}
	_, err := f.append(t, lease, 2, f.page)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	wrong := lease
	wrong.ProducerUUID = f.producers[1].UUID
	_, err = f.append(t, wrong, 1, f.page)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	_, err = f.append(t, lease, 1, f.page)
	require.NoError(t, err)
	f.admit(t)
	job = f.claim(t, 0)
	lease.ArchiveJobLease = job.Lease()
	second := listingContinuation(t, f.page, map[string]string{"after": "t3_abc123"}, map[string]string{"after": "t3_next"}, false)
	_, err = f.append(t, lease, 2, second)
	require.NoError(t, err)
	f.admit(t)
	job = f.claim(t, 0)
	lease.ArchiveJobLease = job.Lease()
	cycle := listingContinuation(t, f.page, map[string]string{"after": "t3_next"}, map[string]string{"after": "t3_abc123"}, false)
	_, err = f.append(t, lease, 3, cycle)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	wrongCursor := listingContinuation(t, f.page, map[string]string{"after": "t3_wrong"}, nil, true)
	_, err = f.append(t, lease, 3, wrongCursor)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		ready, err := f.repo.DiscoveryJob.ReserveSource(ctx, lease, f.listing.ProfileURL, f.now)
		require.NoError(t, err)
		require.True(t, ready)
		_, err = f.repo.DiscoveryJob.ReserveSource(ctx, lease, "https://www.reddit.com/user/other/submitted/", f.now)
		require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
		return nil
	}))
}

func TestDiscoveryListingsRollbackAfterPageAndPreserveRetryHistory(t *testing.T) {
	f := newListingFixture(t)
	f.admit(t)
	job := f.claim(t, 0)
	lease := models.DiscoveryJobLease{ArchiveJobLease: job.Lease(), ProducerUUID: f.producers[0].UUID}
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER fail_listing_progress BEFORE UPDATE ON archive_jobs WHEN NEW.kind='account.list_page' AND NEW.result!=OLD.result BEGIN SELECT RAISE(ABORT,'fixture late failure'); END`)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryJob.AppendPage(ctx, lease, 1, f.page, f.now)
		require.ErrorContains(t, err, "fixture late failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_pages"))
	_, err = raw.Exec("DROP TRIGGER fail_listing_progress")
	require.NoError(t, err)
	_, err = f.append(t, lease, 1, f.page)
	require.NoError(t, err)
	f.admit(t)
	job = f.claim(t, 0)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.Finish(ctx, job.Lease(), f.now, models.ArchiveJobOutcome{State: "failed", ErrorCode: "rate_limited", Result: []byte(`{}`)})
		return err
	}))
	var retry *models.ArchiveJob
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		retry, err = f.repo.DiscoveryJob.Retry(ctx, f.listing.UUID, job.UUID, f.now)
		return err
	}))
	require.NotEqual(t, job.UUID, retry.UUID)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		replayed, err := f.repo.DiscoveryJob.Retry(ctx, f.listing.UUID, job.UUID, f.now.Add(time.Second))
		require.NoError(t, err)
		require.Equal(t, retry, replayed)
		return nil
	}))
	require.True(t, retry.AvailableAt.Equal(f.now.Add(time.Hour)))
	require.Nil(t, f.claim(t, 1))
	f.now = f.now.Add(time.Hour)
	next := f.claim(t, 1)
	require.NotNil(t, next)
	_, err = f.append(t, models.DiscoveryJobLease{ArchiveJobLease: next.Lease(), ProducerUUID: f.producers[1].UUID}, 2,
		listingContinuation(t, f.page, map[string]string{"after": "t3_abc123"}, nil, true))
	require.NoError(t, err)
	worker := ingest.NewDiscoveryCoordinator(f.service)
	old, err := worker.Describe(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "failed", old.Job.State)
	require.Nil(t, old.Receipt, "a successor's page cannot acknowledge the failed predecessor")
	completed, err := worker.Describe(t.Context(), f.tokens[1], next.UUID)
	require.NoError(t, err)
	require.NotNil(t, completed.Receipt)
	require.Equal(t, next.UUID, completed.Receipt.JobUUID)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestDiscoveryJobGuardsRequireAtomicBindingsAndRetainedPage(t *testing.T) {
	f := newListingFixture(t)
	input, err := archive.PrepareDiscoveryJob(models.DiscoveryJobArguments{Version: 1, ListingUUID: f.listing.UUID, Generation: 1, PageOrdinal: 1, DefinitionSHA256: f.listing.Digest, CollectionUUID: f.listing.CollectionUUID})
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.Submit(ctx, input, f.now, 100)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	job := f.admit(t)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.ClaimByID(ctx, job.UUID, job.Revision, uuid.NewString(), f.now, time.Minute)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	job = f.claim(t, 0)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.Finish(ctx, job.Lease(), f.now, models.ArchiveJobOutcome{State: "succeeded", Result: []byte(`{}`)})
		return err
	})
	require.ErrorContains(t, err, "listing job success requires its retained page")
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.Finish(ctx, job.Lease(), f.now, models.ArchiveJobOutcome{State: "succeeded", Result: []byte(`{}`)})
		require.Error(t, err)
		return nil
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic, "catching failure cannot commit an ended attempt without its final page")
	changed := f.listing.DiscoveryListingInput
	changed.PolicySHA256 = strings.Repeat("b", 64)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryJob.CreateListing(ctx, changed, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	require.False(t, errors.Is(err, models.ErrEnrichmentConflict))
}

func TestDiscoveryPagesYieldToDownloadsAndShareMetadataReservations(t *testing.T) {
	f := newListingFixture(t)
	f.admit(t)
	d := newPacingDownload(t, f.enrichmentExecutionFixture, "https://reddit.com/user/download")
	require.Nil(t, f.claim(t, 0))
	for i := 0; i < 4; i++ {
		if i > 0 {
			d = newPacingDownload(t, f.enrichmentExecutionFixture, fmt.Sprintf("https://reddit.com/user/download%d", i))
		}
		run := d.claim(t)
		require.NotNil(t, run)
		require.Nil(t, f.claim(t, 0), "running downloads retain their reservation")
		_, err := d.coordinator.Finish(t.Context(), d.token, run.Lease(), models.SourceRunOutcome{State: "succeeded"})
		require.NoError(t, err)
	}
	d = newPacingDownload(t, f.enrichmentExecutionFixture, "https://reddit.com/user/later")
	require.Nil(t, d.claim(t), "a live listing gets its bounded fairness turn")
	job := f.claim(t, 0)
	require.NotNil(t, job)
	enrichment := f.enrichmentExecutionFixture.admit(t)
	require.Nil(t, tryPacingEnrichment(t, f.enrichmentExecutionFixture, enrichment))
	_, err := f.append(t, models.DiscoveryJobLease{ArchiveJobLease: job.Lease(), ProducerUUID: f.producers[0].UUID}, 1, f.page)
	require.NoError(t, err)
	f.admit(t)
	require.Nil(t, f.claim(t, 0), "a second page yields to queued downloads")
	require.NotNil(t, d.claim(t), "retaining one page releases the source reservation")
}

func TestDiscoveryListingLegacySeedKeepsCursorAndCooldown(t *testing.T) {
	f := discoveryFixture(t, nil)
	advanceAutomationEnrichment(t, f, 0)
	advanceDiscovery(t, f, 0)
	var account models.AutomationDiscoveryRecord
	var collection *models.SourceCollection
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.AutomationDiscoveryImport.Records(ctx, f.manifest.UUID, 0, 100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Table == "discovery_accounts" {
				account = row
			}
		}
		for _, row := range rows {
			if row.Table != "discovery_targets" || row.AccountOrdinal == nil || *row.AccountOrdinal != account.Ordinal {
				continue
			}
			collection, err = f.repo.SourceCollection.Find(ctx, *row.CollectionUUID)
			if err != nil {
				return err
			}
			definition := collection.SourceCollectionDefinition
			definition.State = "active"
			collection, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
			return err
		}
		return fmt.Errorf("fixture target not found")
	}))
	input := models.DiscoveryListingInput{UUID: uuid.NewString(), AccountUUID: *account.AccountUUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
		RootUUID: collection.RootUUID, ProfileURL: account.ProfileURL, PolicySHA256: strings.Repeat("a", 64), ExtractorVersion: "1.32.15-dev", NotBefore: *account.NotBefore,
		InitialCursor: map[string]string{"after": "t3_prior"}, HistoricalPages: 67, Legacy: &models.DiscoveryListingLegacy{SnapshotUUID: f.manifest.UUID, AccountOrdinal: account.Ordinal}}
	for name, change := range map[string]func(*models.DiscoveryListingInput){
		"lost cursor":   func(i *models.DiscoveryListingInput) { i.InitialCursor = nil },
		"lost pages":    func(i *models.DiscoveryListingInput) { i.HistoricalPages = 0 },
		"lost cooldown": func(i *models.DiscoveryListingInput) { i.NotBefore = automationImportNow },
		"changed profile": func(i *models.DiscoveryListingInput) {
			i.ProfileURL = "https://www.reddit.com/user/other/submitted/?sort=new"
		},
	} {
		t.Run(name, func(t *testing.T) {
			altered := input
			change(&altered)
			err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.DiscoveryJob.CreateListing(ctx, altered, automationImportNow)
				return err
			})
			require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		})
	}
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		listing, err := f.repo.DiscoveryJob.CreateListing(ctx, input, automationImportNow)
		require.NoError(t, err)
		require.Equal(t, input.InitialCursor, listing.InitialCursor)
		job, err := f.repo.DiscoveryJob.Admit(ctx, listing.UUID, automationImportNow)
		require.NoError(t, err)
		require.Equal(t, input.NotBefore, job.AvailableAt)
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

type discoveryCommitChange struct {
	models.DiscoveryJobReaderWriter
	change func(context.Context) error
}

func (s discoveryCommitChange) AppendPage(ctx context.Context, lease models.DiscoveryJobLease, ordinal int, body json.RawMessage, now time.Time) (*models.DiscoveryPageReceipt, error) {
	txn.AddPreCommitHook(ctx, s.change)
	return s.DiscoveryJobReaderWriter.AppendPage(ctx, lease, ordinal, body, now)
}

func TestDiscoveryCoordinatorFencesPageCommitAndProducerScope(t *testing.T) {
	for _, mode := range []string{"lease", "definition", "producer"} {
		t.Run(mode, func(t *testing.T) {
			f := newListingFixture(t)
			worker := ingest.NewDiscoveryCoordinator(f.service)
			worker.Now = func() time.Time { return f.now }
			job, err := worker.Admit(t.Context(), f.tokens[0], f.listing.UUID, f.listing.Digest, f.listing.PolicySHA256, f.listing.ExtractorVersion)
			require.NoError(t, err)
			owner := uuid.NewString()
			running, err := worker.Claim(t.Context(), f.tokens[0], job.UUID, job.Revision, owner, f.listing.PolicySHA256, f.listing.ExtractorVersion, time.Minute)
			require.NoError(t, err)
			replayed, err := worker.Claim(t.Context(), f.tokens[0], job.UUID, job.Revision, owner, f.listing.PolicySHA256, f.listing.ExtractorVersion, time.Minute)
			require.NoError(t, err)
			require.Equal(t, running, replayed)
			if mode == "producer" {
				_, err = worker.AppendPage(t.Context(), f.tokens[1], running.Lease(), 1, f.page)
				require.ErrorIs(t, err, models.ErrArchiveJobLease)
			} else {
				f.service.Repo.DiscoveryJob = discoveryCommitChange{DiscoveryJobReaderWriter: f.repo.DiscoveryJob, change: func(ctx context.Context) error {
					if mode == "lease" {
						f.now = f.now.Add(time.Minute)
						return nil
					}
					definition := f.collection.SourceCollectionDefinition
					definition.State = "disabled"
					_, err := f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
					return err
				}}
				_, err = worker.AppendPage(t.Context(), f.tokens[0], running.Lease(), 1, f.page)
				if mode == "lease" {
					require.ErrorIs(t, err, models.ErrArchiveJobLease)
				} else {
					require.ErrorIs(t, err, models.ErrDiscoveryConflict)
				}
			}
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_pages"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_job_attempts WHERE ended_at_ms IS NOT NULL"))
		})
	}
}

func TestDiscoveryCoordinatorFailuresReplayAndDescriptionsKeepOriginalCursor(t *testing.T) {
	f := newListingFixture(t)
	worker := ingest.NewDiscoveryCoordinator(f.service)
	worker.Now = func() time.Time { return f.now }
	job, err := worker.Admit(t.Context(), f.tokens[0], f.listing.UUID, f.listing.Digest, f.listing.PolicySHA256, f.listing.ExtractorVersion)
	require.NoError(t, err)
	running, err := worker.Claim(t.Context(), f.tokens[0], job.UUID, job.Revision, uuid.NewString(), f.listing.PolicySHA256, f.listing.ExtractorVersion, time.Minute)
	require.NoError(t, err)
	failure, err := worker.Fail(t.Context(), f.tokens[0], running.Lease(), "rate_limited")
	require.NoError(t, err)
	replayed, err := worker.Fail(t.Context(), f.tokens[0], running.Lease(), "rate_limited")
	require.NoError(t, err)
	require.Equal(t, failure, replayed)
	_, err = worker.Fail(t.Context(), f.tokens[0], running.Lease(), "authentication")
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	_, err = worker.Fail(t.Context(), f.tokens[1], running.Lease(), "rate_limited")
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	described, err := worker.Describe(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Nil(t, described.Receipt)
	require.Nil(t, described.Cursor)
	require.Equal(t, f.now.Add(time.Hour), described.Job.AvailableAt)
	f.now = f.now.Add(time.Hour)
	running, err = worker.Claim(t.Context(), f.tokens[0], job.UUID, described.Job.Revision, uuid.NewString(), f.listing.PolicySHA256, f.listing.ExtractorVersion, time.Minute)
	require.NoError(t, err)
	page, err := worker.AppendPage(t.Context(), f.tokens[0], running.Lease(), 1, f.page)
	require.NoError(t, err)
	next, err := worker.Admit(t.Context(), f.tokens[0], f.listing.UUID, f.listing.Digest, f.listing.PolicySHA256, f.listing.ExtractorVersion)
	require.NoError(t, err)
	described, err = worker.Describe(t.Context(), f.tokens[1], next.UUID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"after": "t3_abc123"}, described.Cursor)
	require.Nil(t, described.Receipt)
	described, err = worker.Describe(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Nil(t, described.Cursor)
	require.Equal(t, page, described.Receipt)
	other := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other scope", Kind: "feed", Namespace: "native:reddit", State: "active", TargetURL: "https://reddit.com/user/other"}})
	_, token, err := f.service.IssueCredential(t.Context(), f.producers[1].UUID, []models.IngestScope{{CollectionUUID: other.UUID}}, nil)
	require.NoError(t, err)
	_, err = worker.Describe(t.Context(), token, job.UUID)
	require.ErrorIs(t, err, ingest.ErrForbidden)
}
