package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func removeDiscoveryMatchSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryActivationSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE discovery_match_evidence; DROP TABLE discovery_match_candidates; DROP TABLE discovery_match_pages;
 DROP TABLE discovery_match_targets; DELETE FROM native_migration_history WHERE version=1000072`)
	require.NoError(t, err)
}

type discoveryMatchFixture struct {
	*automationSnapshotFixture
	listing  *models.DiscoveryListing
	target   *models.DiscoveryMatchTarget
	input    models.DiscoveryTargetInput
	producer *models.IngestProducer
	page     json.RawMessage
	now      time.Time
}

func newDiscoveryMatchFixture(t *testing.T) *discoveryMatchFixture {
	t.Helper()
	f := &discoveryMatchFixture{now: time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC)}
	f.automationSnapshotFixture = discoveryFixture(t, func(tables map[string][]map[string]any) {
		tables["discovery_candidates"], tables["enrichment_cooldowns"] = nil, nil
		account := tables["discovery_accounts"][0]
		account["cursor_json"], account["pages"] = "null", 0
		for _, target := range tables["discovery_targets"] {
			if target["status"] != "pending" {
				continue
			}
			var evidence map[string]any
			require.NoError(t, json.Unmarshal([]byte(target["evidence_json"].(string)), &evidence))
			evidence["titles"], evidence["dates"] = []string{"album with source positions"}, []string{"2026-10-03"}
			body, err := json.Marshal(evidence)
			require.NoError(t, err)
			target["evidence_json"] = string(body)
		}
	})
	advanceAutomationEnrichment(t, f.automationSnapshotFixture, 0)
	advanceDiscovery(t, f.automationSnapshotFixture, 0)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.AutomationDiscoveryImport.Records(ctx, f.manifest.UUID, 0, 100)
		if err != nil {
			return err
		}
		var account, target models.AutomationDiscoveryRecord
		for _, row := range rows {
			if row.Table == "discovery_accounts" {
				account = row
			}
			if row.Table == "discovery_targets" && row.Disposition == "held" {
				target = row
			}
		}
		collection, err := f.repo.SourceCollection.Find(ctx, *target.CollectionUUID)
		if err != nil {
			return err
		}
		definition := collection.SourceCollectionDefinition
		definition.State = "active"
		collection, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		if err != nil {
			return err
		}
		f.listing, err = f.repo.DiscoveryJob.CreateListing(ctx, models.DiscoveryListingInput{UUID: uuid.NewString(), AccountUUID: *account.AccountUUID, CollectionUUID: collection.UUID,
			CollectionRevision: collection.Revision, RootUUID: collection.RootUUID, ProfileURL: account.ProfileURL, PolicySHA256: strings.Repeat("a", 64), ExtractorVersion: "1.32.15-dev", NotBefore: *account.NotBefore,
			Legacy: &models.DiscoveryListingLegacy{SnapshotUUID: f.manifest.UUID, AccountOrdinal: account.Ordinal}}, f.now)
		if err != nil {
			return err
		}
		post, err := f.repo.SourceEvidence.FindPost(ctx, *target.PostUUID)
		if err != nil {
			return err
		}
		f.input = models.DiscoveryTargetInput{ListingUUID: f.listing.UUID, SourceOrdinal: target.Ordinal, ExpectedSourceSHA256: target.SHA256, ExpectedPostRevision: post.Revision}
		f.target, err = f.repo.DiscoveryMatch.BindTarget(ctx, f.input, f.now)
		if err != nil {
			return err
		}
		f.producer, err = f.repo.Ingest.CreateProducer(ctx, "Listing fixture")
		return err
	}))
	body, err := os.ReadFile("../archive/testdata/discovery-pages-v1.json")
	require.NoError(t, err)
	var corpus struct {
		Pages []struct {
			Page json.RawMessage `json:"page"`
		} `json:"pages"`
	}
	require.NoError(t, json.Unmarshal(body, &corpus))
	page, err := archive.DecodeJSONObject(corpus.Pages[1].Page, archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	page["url"] = f.listing.ProfileURL
	patch := page["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)
	patch["source_extractor_url"], patch["author"] = f.listing.ProfileURL, "deliberately-unlinked"
	f.page, err = archive.EncodeSourceJSON(page)
	require.NoError(t, err)
	return f
}

func (f *discoveryMatchFixture) append(t *testing.T, page json.RawMessage) {
	t.Helper()
	f.now = f.now.Add(2 * time.Minute)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		job, err := f.repo.DiscoveryJob.Admit(ctx, f.listing.UUID, f.now)
		if err != nil {
			return err
		}
		running, err := f.repo.ArchiveJob.ClaimByID(ctx, job.UUID, job.Revision, uuid.NewString(), f.now, time.Minute)
		if err != nil {
			return err
		}
		require.NotNil(t, running)
		lease := models.DiscoveryJobLease{ArchiveJobLease: running.Lease(), ProducerUUID: f.producer.UUID}
		if err := f.repo.DiscoveryJob.BindAttempt(ctx, lease, f.now); err != nil {
			return err
		}
		work, err := archive.DecodeDiscoveryJob(running)
		if err != nil {
			return err
		}
		_, err = f.repo.DiscoveryJob.AppendPage(ctx, lease, work.PageOrdinal, page, f.now)
		return err
	}))
}

func (f *discoveryMatchFixture) advance(t *testing.T, after int) *models.DiscoveryMatchReceipt {
	t.Helper()
	var receipt *models.DiscoveryMatchReceipt
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		receipt, err = f.repo.DiscoveryMatch.Advance(ctx, f.target.UUID, after, f.now)
		return err
	}))
	return receipt
}

func TestDiscoveryComparisonGroupsAcrossPagesAndReopens(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	require.Nil(t, f.advance(t, 0), "waiting for source data cannot finish comparison")
	f.append(t, f.page)
	first := f.advance(t, 0)
	require.NotNil(t, first)
	require.Equal(t, 1, first.CandidateCount)
	require.False(t, first.Complete)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	require.Equal(t, first, f.advance(t, 0))
	page, err := archive.DecodeJSONObject(f.page, archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	page["cursor"], page["next_cursor"], page["complete"] = map[string]string{"after": "t3_abc123"}, nil, true
	records := page["records"].([]any)
	records[0].(map[string]any)["patch"].(map[string]any)["date"] = "2026-10-03"
	page["records"] = append(records, map[string]any{"kind": "post", "base": json.Number("0"), "parent": nil, "observed_at": "2026-10-04T01:02:03.000000004Z", "patch": map[string]any{"id": "def456"}, "removed": []any{}})
	second, err := archive.EncodeSourceJSON(page)
	require.NoError(t, err)
	f.append(t, second)
	last := f.advance(t, 1)
	require.True(t, last.Complete)
	require.Equal(t, 2, last.CandidateCount)
	require.Equal(t, first, f.advance(t, 0))
	require.Equal(t, last, f.advance(t, 1))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		target, err := f.repo.DiscoveryMatch.Target(ctx, f.target.UUID)
		require.NoError(t, err)
		require.Equal(t, 2, target.LastPage)
		require.True(t, target.EnumerationComplete)
		candidates, err := f.repo.DiscoveryMatch.Candidates(ctx, target.UUID, 0, 1)
		require.NoError(t, err)
		require.Len(t, candidates, 1)
		one := candidates[0]
		require.Equal(t, "abc123", one.Value)
		require.Equal(t, 2, one.PageCount)
		require.Equal(t, 2, one.BestPage)
		require.False(t, one.NeedsDetail)
		evidence, err := f.repo.DiscoveryMatch.Evidence(ctx, one.Sequence, 0, 1)
		require.NoError(t, err)
		require.Len(t, evidence, 1)
		require.True(t, evidence[0].NeedsDetail)
		require.Equal(t, []int{0, 1, 2}, evidence[0].RecordOrdinals)
		evidence, err = f.repo.DiscoveryMatch.Evidence(ctx, one.Sequence, 1, 1)
		require.NoError(t, err)
		require.False(t, evidence[0].NeedsDetail)
		candidates, err = f.repo.DiscoveryMatch.Candidates(ctx, target.UUID, one.Sequence, 1)
		require.NoError(t, err)
		require.Len(t, candidates, 1)
		require.Equal(t, "def456", candidates[0].Value, "competing source IDs cannot be folded together")
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_identifiers WHERE value IN ('abc123','def456')"), "candidate processing cannot publish identities")
}

func TestDiscoveryComparisonRollbackAndConcurrentReplay(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	f.append(t, f.page)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER stop_match_evidence BEFORE INSERT ON discovery_match_evidence BEGIN SELECT RAISE(ABORT,'fixture interruption'); END`)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, caught := f.repo.DiscoveryMatch.Advance(ctx, f.target.UUID, 0, f.now)
		require.Error(t, caught)
		return nil
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	for _, table := range []string{"discovery_match_pages", "discovery_match_candidates", "discovery_match_evidence"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	_, err = raw.Exec("DROP TRIGGER stop_match_evidence")
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make([]*models.DiscoveryMatchReceipt, 2)
	errors := make([]error, 2)
	for i := range results {
		wg.Go(func() {
			errors[i] = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				var err error
				results[i], err = f.repo.DiscoveryMatch.Advance(ctx, f.target.UUID, 0, f.now)
				return err
			})
		})
	}
	wg.Wait()
	for _, err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, results[0], results[1])
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_match_candidates"))
}

func TestDiscoveryComparisonSourceChangeCannotCommitPartialResults(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	f.append(t, f.page)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			collection, err := f.repo.SourceCollection.Find(ctx, f.listing.CollectionUUID)
			if err != nil {
				return err
			}
			definition := collection.SourceCollectionDefinition
			definition.State = "disabled"
			_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
			return err
		})
		_, err := f.repo.DiscoveryMatch.Advance(ctx, f.target.UUID, 0, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	first := f.advance(t, 0)
	require.NotNil(t, first, "the failed source change and partial comparison both rolled back")
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
	require.Equal(t, first, f.advance(t, 0), "original receipts survive later source changes")
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryMatch.Advance(ctx, f.target.UUID, 1, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
}

func TestDiscoveryComparisonPreparationIsReadOnlyAndRechecksNativeEdits(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	f.append(t, f.page)
	prepare := func() models.PreparedDiscoveryComparison {
		var prepared models.PreparedDiscoveryComparison
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			prepared, err = f.repo.DiscoveryMatch.Prepare(ctx, f.target.UUID, 0, f.now)
			return err
		}))
		require.NotNil(t, prepared)
		return prepared
	}
	one, two := prepare(), prepare()
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_match_pages"))
	_, err := raw.Exec("UPDATE source_posts SET revision=revision+1 WHERE uuid=?", f.target.PostUUID)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := one.Commit(ctx, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_match_pages"))
	// Simulate the same reviewed state being restored before the unchanged plan
	// commits, then verify two independently prepared callers share one receipt.
	_, err = raw.Exec("UPDATE source_posts SET revision=revision-1 WHERE uuid=?", f.target.PostUUID)
	require.NoError(t, err)
	var first, replay *models.DiscoveryMatchReceipt
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		first, err = one.Commit(ctx, f.now)
		return err
	}))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		replay, err = two.Commit(ctx, f.now.Add(time.Minute))
		return err
	}))
	require.Equal(t, first, replay)
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", f.target.PostUUID)
	require.NoError(t, err)
	require.Equal(t, first, f.advance(t, 0))
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryMatch.Advance(ctx, f.target.UUID, 1, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()), "historical comparisons survive a later forgotten post")
}

func TestDiscoveryComparisonBindingUsesExactReviewedInputAndManagedWrites(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	_, err := f.repo.DiscoveryMatch.BindTarget(t.Context(), f.input, f.now)
	require.Error(t, err, "writes must belong to a managed transaction")
	for _, change := range []func(*models.DiscoveryTargetInput){
		func(in *models.DiscoveryTargetInput) { in.ExpectedSourceSHA256 = strings.Repeat("b", 64) },
		func(in *models.DiscoveryTargetInput) { in.ExpectedPostRevision++ },
		func(in *models.DiscoveryTargetInput) { in.SourceOrdinal++ },
		func(in *models.DiscoveryTargetInput) { in.ListingUUID = uuid.NewString() },
	} {
		in := f.input
		change(&in)
		err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.DiscoveryMatch.BindTarget(ctx, in, f.now)
			return err
		})
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	}
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		replayed, err := f.repo.DiscoveryMatch.BindTarget(ctx, f.input, f.now.Add(time.Minute))
		require.NoError(t, err)
		require.Equal(t, f.target, replayed)
		return nil
	}))
}

func TestDiscoveryComparisonCandidateCapacityDoesNotTruncateOrAdvance(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	page, err := archive.DecodeJSONObject(f.page, archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	records := page["records"].([]any)[:1]
	records[0].(map[string]any)["patch"].(map[string]any)["id"] = "a000000"
	for i := 1; i < archive.MaxDiscoveryRecords; i++ {
		records = append(records, map[string]any{"kind": "post", "base": json.Number("0"), "parent": nil, "observed_at": "2026-10-04T01:02:03Z", "patch": map[string]any{"id": fmt.Sprintf("a%06d", i)}, "removed": []any{}})
	}
	page["records"] = records
	first, err := archive.EncodeSourceJSON(page)
	require.NoError(t, err)
	f.append(t, first)
	require.Equal(t, archive.MaxDiscoveryRecords, f.advance(t, 0).CandidateCount)
	page["cursor"], page["next_cursor"], page["complete"] = map[string]string{"after": "t3_abc123"}, nil, true
	page["records"] = page["records"].([]any)[:1]
	page["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)["id"] = "capacity"
	second, err := archive.EncodeSourceJSON(page)
	require.NoError(t, err)
	f.append(t, second)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryMatch.Advance(ctx, f.target.UUID, 1, f.now)
		require.ErrorIs(t, err, models.ErrArchiveJobCapacity)
		return nil // swallowed write failure must still roll back every new row
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, archive.MaxDiscoveryRecords, queryUint(t, raw, "SELECT count(*) FROM discovery_match_candidates"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT last_page FROM discovery_match_targets"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_match_pages"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM discovery_pages"), "retained source pages survive a comparison limit")
}

func TestDiscoveryComparisonEmptyFinalPageCompletesOnlyTheComparedListing(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	empty := listingContinuation(t, f.page, nil, nil, true)
	f.append(t, empty)
	receipt := f.advance(t, 0)
	require.NotNil(t, receipt)
	require.True(t, receipt.Complete)
	require.Zero(t, receipt.CandidateCount)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_match_candidates"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_discovery_resolutions"), "empty enumeration cannot resolve an identity")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}
