package sqlite_test

import (
	"context"
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

type enrichmentExecutionFixture struct {
	db         *sqlite.Database
	repo       models.Repository
	service    *ingest.Service
	worker     *ingest.EnrichmentCoordinator
	now        time.Time
	target     *models.EnrichmentTarget
	collection *models.SourceCollection
	producers  [2]*models.IngestProducer
	tokens     [2]string
	initial    json.RawMessage
	complete   json.RawMessage
}

type enrichmentCheckpointBeforeCommit struct {
	models.EnrichmentJobReaderWriter
	beforeCommit func()
}

func (s enrichmentCheckpointBeforeCommit) Checkpoint(ctx context.Context, lease models.EnrichmentJobLease, revision int, body json.RawMessage, now time.Time) (*models.EnrichmentCheckpointReceipt, error) {
	txn.AddPreCommitHook(ctx, func(context.Context) error { s.beforeCommit(); return nil })
	return s.EnrichmentJobReaderWriter.Checkpoint(ctx, lease, revision, body, now)
}

func newEnrichmentExecutionFixture(t *testing.T) *enrichmentExecutionFixture {
	t.Helper()
	db, repo := archiveTestDatabase(t)
	f := &enrichmentExecutionFixture{db: db, repo: repo, now: time.Date(2026, 10, 3, 2, 0, 0, 0, time.UTC)}
	f.service = ingest.New(repo)
	f.worker = ingest.NewEnrichmentCoordinator(f.service)
	f.worker.Now = func() time.Time { return f.now }
	body, err := os.ReadFile("../archive/testdata/enrichment-transcript-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Initial  json.RawMessage `json:"initial"`
		Complete json.RawMessage `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(body, &fixture))
	f.initial, f.complete = fixture.Initial, fixture.Complete
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
	url, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://www.reddit.com/comments/abc123"})
	require.NoError(t, err)
	f.collection = putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Fixture feed", Kind: "feed", Namespace: "native:reddit", State: "active", TargetURL: "https://www.reddit.com/user/example"}})
	f.target = retainEnrichment(t, repo, models.EnrichmentTargetInput{PostUUID: post.UUID, URLUUID: url.URLUUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Policy: models.EnrichmentGalleryMetadataV1, Origin: "review"},
		models.EnrichmentSchedule{State: "pending", Priority: 20}, f.now)
	for i := range f.producers {
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			f.producers[i], err = repo.Ingest.CreateProducer(ctx, "Fixture worker")
			return err
		}))
		_, f.tokens[i], err = f.service.IssueCredential(t.Context(), f.producers[i].UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID}}, nil)
		require.NoError(t, err)
	}
	return f
}

func (f *enrichmentExecutionFixture) admit(t *testing.T) *models.ArchiveJob {
	t.Helper()
	job, err := f.worker.Admit(t.Context(), f.tokens[0], f.target.UUID, f.target.Revision, strings.Repeat("a", 64), "1.32.15-dev")
	require.NoError(t, err)
	require.NotNil(t, job)
	return job
}

func (f *enrichmentExecutionFixture) claim(t *testing.T, id string, producer int) *models.ArchiveJob {
	t.Helper()
	current, err := f.worker.Find(t.Context(), f.tokens[producer], id)
	require.NoError(t, err)
	job, err := f.worker.Claim(t.Context(), f.tokens[producer], id, current.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, job)
	return job
}

func TestEnrichmentJobsResumedObservationsRetainOriginalProducers(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	replay, err := f.worker.Admit(t.Context(), f.tokens[1], f.target.UUID, f.target.Revision, strings.Repeat("a", 64), "1.32.15-dev")
	require.NoError(t, err)
	require.Equal(t, job, replay)
	_, err = f.worker.Admit(t.Context(), f.tokens[1], f.target.UUID, f.target.Revision, strings.Repeat("b", 64), "1.32.15-dev")
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	first := f.claim(t, job.UUID, 0)
	lostClaim, err := f.worker.Claim(t.Context(), f.tokens[0], job.UUID, job.Revision, first.OwnerUUID, strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	require.Equal(t, first, lostClaim)
	_, err = f.worker.Claim(t.Context(), f.tokens[1], job.UUID, job.Revision, first.OwnerUUID, strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	checkpoint, err := f.worker.Checkpoint(t.Context(), f.tokens[0], first.Lease(), 0, f.initial)
	require.NoError(t, err)
	require.Equal(t, 3, checkpoint.RecordCount)
	require.Equal(t, 1, checkpoint.PendingCount)
	ack, err := f.worker.Checkpoint(t.Context(), f.tokens[0], first.Lease(), 0, f.initial)
	require.NoError(t, err)
	require.Equal(t, checkpoint, ack)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[1], first.Lease(), 1, f.complete)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	f.now = f.now.Add(2 * time.Minute)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		count, err := f.repo.ArchiveJob.Recover(ctx, f.now, 10)
		require.Equal(t, 1, count)
		return err
	}))
	delayed, err := f.worker.Find(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	require.Equal(t, f.now.Add(5*time.Minute), delayed.AvailableAt)
	tooSoon, err := f.worker.Claim(t.Context(), f.tokens[1], job.UUID, delayed.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	require.Nil(t, tooSoon)
	f.now = delayed.AvailableAt
	second := f.claim(t, job.UUID, 1)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], first.Lease(), 1, f.complete)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	final, err := f.worker.Checkpoint(t.Context(), f.tokens[1], second.Lease(), 1, f.complete)
	require.NoError(t, err)
	require.Equal(t, 5, final.RecordCount)
	require.Equal(t, 0, final.PendingCount)
	ack, err = f.worker.Checkpoint(t.Context(), f.tokens[0], first.Lease(), 0, f.initial)
	require.NoError(t, err)
	require.Equal(t, checkpoint, ack, "lost old acknowledgement must not replace the newer head")
	head, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, *final, head.EnrichmentCheckpointReceipt)
	parsed, err := archive.ParseEnrichmentTranscript(f.complete)
	require.NoError(t, err)
	require.Equal(t, parsed.Body(), head.Body)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		records, err := f.repo.EnrichmentJob.CheckpointRecords(ctx, job.UUID, -1, 100)
		require.NoError(t, err)
		require.Len(t, records, 5)
		for i, record := range records {
			producer := 0
			if i >= 3 {
				producer = 1
			}
			require.Equal(t, f.producers[producer].UUID, record.ProducerUUID)
			require.EqualValues(t, producer+1, record.Fence)
			require.Equal(t, producer+1, record.CheckpointRevision)
			digest, err := parsed.RecordDigest(i)
			require.NoError(t, err)
			require.Equal(t, digest, record.Digest)
		}
		page, err := f.repo.EnrichmentJob.CheckpointRecords(ctx, job.UUID, 2, 2)
		require.NoError(t, err)
		require.Equal(t, records[3:], page)
		pending, err := f.repo.EnrichmentWork.Target(ctx, f.target.UUID)
		require.NoError(t, err)
		require.Equal(t, "pending", pending.State, "a transcript cannot certify native capture publication")
		ready, err := f.repo.EnrichmentWork.Ready(ctx, f.collection.UUID, f.now, 10)
		require.NoError(t, err)
		require.Empty(t, ready, "bound work is not repeatedly admitted")
		return nil
	}))
	backup := filepath.Join(t.TempDir(), "enrichment-checkpoints.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	f.worker.Service = ingest.New(f.db.Repository())
	restored, err := f.worker.CheckpointHead(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	require.Equal(t, head, restored)
	raw := openRawDB(t, backup)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.EqualValues(t, len(head.Body), queryUint(t, raw, "SELECT byte_size FROM enrichment_checkpoint_usage"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoints"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoint_receipts"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	anonymousPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, anonymousPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anonymousRaw := openRawDB(t, anonymousPath)
	defer anonymousRaw.Close()
	for _, table := range []string{"enrichment_checkpoints", "enrichment_checkpoint_records", "enrichment_checkpoint_receipts", "enrichment_job_attempts", "enrichment_job_targets"} {
		require.Zero(t, queryUint(t, anonymousRaw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anonymousRaw, "SELECT byte_size FROM enrichment_checkpoint_usage"))
	check := sqlite.NewDatabase()
	require.NoError(t, check.Open(anonymousPath))
	require.NoError(t, check.Close())
}

func TestEnrichmentJobsRequireAtomicBindingsAndCheckpoints(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	input, err := archive.PrepareEnrichmentJob(models.EnrichmentJobArguments{Version: 1, TargetUUID: f.target.UUID, TargetRevision: 1,
		PostUUID: f.target.PostUUID, CollectionUUID: f.collection.UUID, CollectionRevision: 1, PolicySHA256: strings.Repeat("a", 64), ExtractorVersion: "1.32.15-dev"})
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.Submit(ctx, input, f.now, 100)
		return err
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
	job := f.admit(t)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.ClaimByID(ctx, job.UUID, job.Revision, uuid.NewString(), f.now, time.Minute)
		return err
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_job_attempts"))
	running := f.claim(t, job.UUID, 0)
	_, err = raw.Exec("CREATE TRIGGER reject_enrichment_record BEFORE INSERT ON enrichment_checkpoint_records WHEN NEW.ordinal=1 BEGIN SELECT RAISE(ABORT,'injected record failure'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.EnrichmentJob.Checkpoint(ctx, models.EnrichmentJobLease{ArchiveJobLease: running.Lease(), ProducerUUID: f.producers[0].UUID}, 0, f.initial, f.now)
		require.ErrorContains(t, err, "injected record failure")
		return nil // even a caller that swallows the late error cannot commit
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	for _, table := range []string{"enrichment_checkpoints", "enrichment_checkpoint_receipts", "enrichment_checkpoint_records"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT byte_size FROM enrichment_checkpoint_usage"))
	_, err = raw.Exec("DROP TRIGGER reject_enrichment_record")
	require.NoError(t, err)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	prior, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	for _, change := range []func(map[string]any){
		func(v map[string]any) {
			v["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)["title"] = "Rewritten"
		},
		func(v map[string]any) { v["url"] = "https://www.reddit.com/comments/another" },
		func(v map[string]any) { v["extractor_version"] = "another" },
		func(v map[string]any) { v["records"] = v["records"].([]any)[:3] },
	} {
		value, err := archive.DecodeJSONObject(f.complete, archive.MaxEnrichmentTranscriptBytes)
		require.NoError(t, err)
		change(value)
		body, err := archive.EncodeSourceJSON(value)
		require.NoError(t, err)
		_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 1, body)
		require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	}
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	current, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, prior, current)
}

func TestEnrichmentJobsHeldTargetsAndExpiredTransactionsCannotAdvance(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	receipt, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	held, err := scheduleEnrichment(f.repo, f.target, models.EnrichmentSchedule{State: "held", NotBefore: f.target.NotBefore}, f.now)
	require.NoError(t, err)
	status, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", status.State)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 1, f.complete)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	ack, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	require.Equal(t, receipt, ack)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.target, err = f.repo.EnrichmentWork.Retry(ctx, held.UUID, held.Revision, f.now)
		return err
	}))
	newJob := f.admit(t)
	require.NotEqual(t, job.UUID, newJob.UUID)
	running = f.claim(t, newJob.UUID, 0)
	// Inject a clock change after the checkpoint writes but before commit.
	baseStore := f.service.Repo.EnrichmentJob
	f.service.Repo.EnrichmentJob = enrichmentCheckpointBeforeCommit{baseStore, func() { f.now = f.now.Add(2 * time.Minute) }}
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	f.service.Repo.EnrichmentJob = baseStore
	head, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], newJob.UUID)
	require.NoError(t, err)
	require.Nil(t, head, "expired publication rolls back its receipt, body and provenance")
}

func TestEnrichmentJobsCredentialRotationAndHistoricalRootScope(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	_, replacement, err := f.service.IssueCredential(t.Context(), f.producers[0].UUID, []models.IngestScope{{CollectionUUID: f.collection.UUID}}, nil)
	require.NoError(t, err)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.Ingest.RevokeCredential(ctx, f.tokens[0][7:43]) }))
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.ErrorIs(t, err, ingest.ErrUnauthorized)
	_, err = f.worker.Checkpoint(t.Context(), replacement, running.Lease(), 0, f.initial)
	require.NoError(t, err, "credential rotation preserves the same producer's ownership")
	_, err = f.worker.Renew(t.Context(), f.tokens[1], running.Lease(), time.Minute)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	_, err = f.worker.Renew(t.Context(), replacement, running.Lease(), time.Minute)
	require.NoError(t, err)
	var root *models.MediaRoot
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = f.repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "New logical root", State: "active"}})
		return err
	}))
	definition := f.collection.SourceCollectionDefinition
	definition.RootUUID, definition.PathPrefix = &root.UUID, "."
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	_, newRootToken, err := f.service.IssueCredential(t.Context(), f.producers[0].UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	_, err = f.worker.Find(t.Context(), newRootToken, job.UUID)
	require.ErrorIs(t, err, ingest.ErrForbidden, "a new-root grant cannot expose old-root work")
	_, err = f.worker.CheckpointHead(t.Context(), newRootToken, job.UUID)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	_, err = f.worker.Renew(t.Context(), replacement, running.Lease(), time.Minute)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	_, err = f.worker.Checkpoint(t.Context(), replacement, running.Lease(), 1, f.complete)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	_, err = f.worker.Checkpoint(t.Context(), replacement, running.Lease(), 0, f.initial)
	require.NoError(t, err, "historical acknowledgement retains its original root scope")
}

func TestEnrichmentJobsStartupRejectsAlteredCheckpointEvidence(t *testing.T) {
	for _, item := range []struct{ name, trigger, mutation string }{
		{"missing_provenance", "", "DELETE FROM enrichment_checkpoint_records WHERE ordinal=1"},
		{"storage_accounting", "", "UPDATE enrichment_checkpoint_usage SET byte_size=0"},
		{"record_digest", "enrichment_checkpoint_record_immutable", "UPDATE enrichment_checkpoint_records SET digest='" + strings.Repeat("0", 64) + "' WHERE ordinal=0"},
		{"receipt_digest", "enrichment_checkpoint_receipt_immutable", "UPDATE enrichment_checkpoint_receipts SET digest='" + strings.Repeat("0", 64) + "'"},
	} {
		t.Run(item.name, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
			require.NoError(t, err)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			var guard string
			if item.trigger != "" {
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", item.trigger).Scan(&guard))
				_, err := raw.Exec("DROP TRIGGER " + item.trigger)
				require.NoError(t, err)
			}
			_, err = raw.Exec(item.mutation)
			require.NoError(t, err)
			if guard != "" {
				_, err = raw.Exec(guard)
				require.NoError(t, err)
			}
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			check := sqlite.NewDatabase()
			require.Error(t, check.Open(path))
			require.NoError(t, check.Close())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestEnrichmentJobsBoundedAdmissionAndCheckpointStorage(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	first := f.admit(t)
	for i := 1; i <= archive.MaxEnrichmentJobs; i++ {
		key := fmt.Sprintf("extra%d", i)
		post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: key}, "")
		url, err := observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://www.reddit.com/comments/" + key})
		require.NoError(t, err)
		target := retainEnrichment(t, f.repo, models.EnrichmentTargetInput{PostUUID: post.UUID, URLUUID: url.URLUUID,
			CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Policy: models.EnrichmentGalleryMetadataV1, Origin: "review"},
			models.EnrichmentSchedule{State: "pending"}, f.now)
		_, err = f.worker.Admit(t.Context(), f.tokens[0], target.UUID, target.Revision, strings.Repeat("a", 64), "1.32.15-dev")
		if i < archive.MaxEnrichmentJobs {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, models.ErrArchiveJobCapacity)
		}
	}
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, archive.MaxEnrichmentJobs, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
	replay, err := f.worker.Admit(t.Context(), f.tokens[0], f.target.UUID, f.target.Revision, strings.Repeat("a", 64), "1.32.15-dev")
	require.NoError(t, err)
	require.Equal(t, first, replay, "full capacity cannot erase an existing admission")
	running := f.claim(t, first.UUID, 0)
	// Simulate a full staging budget without allocating two GiB in a fixture.
	_, err = raw.Exec("UPDATE enrichment_checkpoint_usage SET byte_size=?", int64(archive.MaxEnrichmentStoredBytes))
	require.NoError(t, err)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.ErrorIs(t, err, models.ErrArchiveJobCapacity)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoint_receipts"))
	_, err = raw.Exec("UPDATE enrichment_checkpoint_usage SET byte_size=0")
	require.NoError(t, err)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
}

func TestEnrichmentJobsExplicitRetryPreservesDeadlineAndPriorEvidence(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	retryAt := f.now.Add(time.Hour)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		queued, err := f.repo.ArchiveJob.Finish(ctx, running.Lease(), f.now, models.ArchiveJobOutcome{State: "retry", ErrorCode: "rate_limited", RetryAt: retryAt, Result: []byte(`{}`)})
		if err != nil {
			return err
		}
		_, err = f.repo.ArchiveJob.Cancel(ctx, queued.UUID, queued.Revision, f.now)
		return err
	}))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.target, err = f.repo.EnrichmentWork.Retry(ctx, f.target.UUID, f.target.Revision, f.now)
		return err
	}))
	require.Equal(t, retryAt, f.target.NotBefore)
	_, err = f.worker.Admit(t.Context(), f.tokens[0], f.target.UUID, f.target.Revision, strings.Repeat("a", 64), "1.32.15-dev")
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	f.now = retryAt
	next := f.admit(t)
	require.NotEqual(t, job.UUID, next.UUID)
	head, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.NotNil(t, head, "a new request cannot delete the previous attempt's retained evidence")
}

func TestEnrichmentJobsConcurrentProducersObtainOneBoundAttempt(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	type result struct {
		job *models.ArchiveJob
		err error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i := range 2 {
		go func() {
			<-start
			claimed, err := f.worker.Claim(t.Context(), f.tokens[i], job.UUID, job.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
			results <- result{claimed, err}
		}()
	}
	close(start)
	winners := 0
	for range 2 {
		value := <-results
		if value.job != nil {
			require.NoError(t, value.err)
			winners++
		} else {
			require.True(t, errors.Is(value.err, models.ErrArchiveJobConflict), "%v", value.err)
		}
	}
	require.Equal(t, 1, winners)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM archive_job_attempts"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_job_attempts"))
}
