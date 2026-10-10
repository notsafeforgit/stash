package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

type detailFixture struct {
	*discoveryMatchFixture
	worker    *ingest.DiscoveryDetailCoordinator
	service   *ingest.Service
	tokens    []string
	producers []string
	input     models.DiscoveryDetailAdmission
	body      json.RawMessage
}

func newDetailFixture(t *testing.T) *detailFixture {
	t.Helper()
	f := &detailFixture{discoveryMatchFixture: newDiscoveryMatchFixture(t)}
	f.append(t, f.page)
	f.advance(t, 0)
	return attachDetailFixture(t, f.discoveryMatchFixture)
}

func attachDetailFixture(t *testing.T, match *discoveryMatchFixture) *detailFixture {
	t.Helper()
	f := &detailFixture{discoveryMatchFixture: match}
	review := f.review(t)
	f.body = discoveryPreviewBody(t, f.page, review.Candidate.URL, f.listing.ExtractorVersion)
	f.input = models.DiscoveryDetailAdmission{TargetUUID: f.target.UUID, ExpectedTargetRevision: review.Target.Revision, CandidateSequence: review.Candidate.Sequence,
		PolicySHA256: f.listing.PolicySHA256, ExtractorVersion: f.listing.ExtractorVersion}
	f.service = ingest.New(f.repo)
	f.worker = ingest.NewDiscoveryDetailCoordinator(f.service)
	f.worker.Now = func() time.Time { return f.now }
	for i := 0; i < 2; i++ {
		var producer *models.IngestProducer
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			producer, err = f.repo.Ingest.CreateProducer(ctx, "Detail worker")
			return err
		}))
		_, token, err := f.service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: f.listing.CollectionUUID, RootUUID: f.listing.RootUUID}}, nil)
		require.NoError(t, err)
		f.tokens = append(f.tokens, token)
		f.producers = append(f.producers, producer.UUID)
	}
	return f
}
func (f *detailFixture) admit(t *testing.T) *models.ArchiveJob {
	t.Helper()
	job, err := f.worker.Admit(t.Context(), f.tokens[0], f.input)
	require.NoError(t, err)
	return job
}
func (f *detailFixture) claim(t *testing.T, job *models.ArchiveJob, producer int) *models.ArchiveJob {
	t.Helper()
	current, err := f.worker.Find(t.Context(), f.tokens[producer], job.UUID)
	require.NoError(t, err)
	ret, err := f.worker.Claim(t.Context(), f.tokens[producer], job.UUID, current.Revision, uuid.NewString(), f.input.PolicySHA256, f.input.ExtractorVersion, time.Minute)
	require.NoError(t, err)
	return ret
}
func detailPartial(t *testing.T, body json.RawMessage) json.RawMessage {
	t.Helper()
	value, err := archive.DecodeJSONObject(body, archive.MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	value["records"] = value["records"].([]any)[:1]
	value["pending"] = []any{map[string]any{"url": "https://imgur.com/child", "parent": 0, "depth": 1, "reason": "timeout"}}
	ret, err := archive.EncodeSourceJSON(value)
	require.NoError(t, err)
	return ret
}

func TestDiscoveryDetailJobsResumeOriginalProvenanceAndKeepReview(t *testing.T) {
	f := newDetailFixture(t)
	before := f.review(t)
	job := f.admit(t)
	require.Equal(t, job, f.admit(t))
	work, err := archive.DecodeDiscoveryDetailJob(job)
	require.NoError(t, err)
	require.Equal(t, before.Candidate.URL, work.URL)
	running := f.claim(t, job, 0)
	require.NotNil(t, running)
	partial := detailPartial(t, f.body)
	finished, err := archive.DecodeJSONObject(f.body, archive.MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	finished["unresolved"] = []any{map[string]any{"url": "https://imgur.com/child", "parent": 0, "depth": 1, "reason": "unsupported_extractor"}}
	f.body, err = archive.EncodeSourceJSON(finished)
	require.NoError(t, err)
	checkpoint, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, partial)
	require.NoError(t, err)
	_, err = f.worker.Complete(t.Context(), f.tokens[0], running.Lease(), checkpoint.Revision, checkpoint.Digest)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	f.now = f.now.Add(2 * time.Minute)
	replay, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, partial)
	require.NoError(t, err)
	require.Equal(t, checkpoint, replay)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		ret, err := f.repo.DiscoveryDetail.Maintain(ctx, f.now)
		require.Equal(t, 1, ret.Recovered)
		return err
	}))
	second := f.claim(t, job, 1)
	require.NotNil(t, second)
	checkpoint2, err := f.worker.Checkpoint(t.Context(), f.tokens[1], second.Lease(), checkpoint.Revision, f.body)
	require.NoError(t, err)
	result, err := f.worker.Complete(t.Context(), f.tokens[1], second.Lease(), checkpoint2.Revision, checkpoint2.Digest)
	require.NoError(t, err)
	require.Equal(t, "corroborated", result.Evidence.Status)
	require.Equal(t, "exact-title-and-date", result.Evidence.Basis)
	before.Detail = result
	before.Blockers = []string{"listing_incomplete"}
	require.Equal(t, before, f.review(t), "corroboration preserves listing coverage and original candidates without publishing identity")
	var records []models.EnrichmentCheckpointRecord
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		records, err = f.repo.DiscoveryDetail.CheckpointRecords(ctx, job.UUID, -1, 100)
		return err
	}))
	require.Len(t, records, 3)
	require.Equal(t, f.producers[0], records[0].ProducerUUID)
	require.Equal(t, f.producers[1], records[1].ProducerUUID)
	captures, err := archive.PrepareDiscoveryDetailCaptures(job.UUID, work.PostUUID, work.PostIdentifier(), work.URL, work.ExtractorVersion, f.body, records)
	require.NoError(t, err)
	require.Len(t, captures, 3)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	f.service.Repo = f.repo
	f.now = f.now.Add(time.Hour)
	result2, err := f.worker.Complete(t.Context(), f.tokens[1], second.Lease(), checkpoint2.Revision, checkpoint2.Digest)
	require.NoError(t, err)
	require.Equal(t, result, result2)
	replay, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, partial)
	require.NoError(t, err)
	require.Equal(t, checkpoint, replay)
	_, err = f.worker.Complete(t.Context(), f.tokens[0], second.Lease(), checkpoint2.Revision, checkpoint2.Digest)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_match_publications"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_identifiers WHERE namespace='native:reddit' AND value='abc123'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestDiscoveryDetailJobsKeepNegativeAndEmptyResultsWithoutDroppingCandidates(t *testing.T) {
	for _, empty := range []bool{false, true} {
		f := newDetailFixture(t)
		original := f.review(t)
		body, err := archive.DecodeJSONObject(f.body, archive.MaxEnrichmentTranscriptBytes)
		require.NoError(t, err)
		if empty {
			body["records"] = []any{}
		} else {
			body["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)["date"] = "2020-01-01"
		}
		raw, err := archive.EncodeSourceJSON(body)
		require.NoError(t, err)
		running := f.claim(t, f.admit(t), 0)
		checkpoint, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, raw)
		require.NoError(t, err)
		result, err := f.worker.Complete(t.Context(), f.tokens[0], running.Lease(), checkpoint.Revision, checkpoint.Digest)
		require.NoError(t, err)
		require.Equal(t, "uncorroborated", result.Evidence.Status)
		require.Nil(t, result.Evidence.WitnessOrdinal)
		original.Detail = result
		require.Equal(t, original, f.review(t))
		require.NoError(t, f.db.Close())
		require.NoError(t, f.db.Open(f.db.DatabasePath()))
	}
}

func TestDiscoveryDetailJobsRejectChangedSelectionAndRetainOldAcknowledgements(t *testing.T) {
	f := newDetailFixture(t)
	job := f.admit(t)
	different := f.input
	different.ExtractorVersion = "changed"
	_, err := f.worker.Admit(t.Context(), f.tokens[0], different)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	_, err = f.worker.Claim(t.Context(), f.tokens[0], job.UUID, job.Revision, uuid.NewString(), different.PolicySHA256, different.ExtractorVersion, time.Minute)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	running := f.claim(t, job, 0)
	body, err := archive.DecodeJSONObject(f.body, archive.MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	body["url"] = "https://www.reddit.com/comments/wrong"
	changed, err := archive.EncodeSourceJSON(body)
	require.NoError(t, err)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, changed)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[1], running.Lease(), 0, f.body)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	checkpoint, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.body)
	require.NoError(t, err)
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
	_, err = f.worker.Complete(t.Context(), f.tokens[0], running.Lease(), checkpoint.Revision, checkpoint.Digest)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	replay, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.body)
	require.NoError(t, err)
	require.Equal(t, checkpoint, replay)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		result, err := f.repo.DiscoveryDetail.Maintain(ctx, f.now)
		require.NoError(t, err)
		require.Equal(t, 1, result.Cancelled)
		return err
	}))
	replay, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.body)
	require.NoError(t, err)
	require.Equal(t, checkpoint, replay)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestDiscoveryDetailJobsRequireAtomicAdmissionAttemptsAndComparison(t *testing.T) {
	f := newDetailFixture(t)
	job := f.admit(t)
	work, err := archive.DecodeDiscoveryDetailJob(job)
	require.NoError(t, err)
	work.Generation++
	work.CandidateSequence++
	submission, err := archive.PrepareDiscoveryDetailJob(*work)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.Submit(ctx, submission, f.now, 100000)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.ClaimByID(ctx, job.UUID, job.Revision, uuid.NewString(), f.now, time.Minute)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	running := f.claim(t, job, 0)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.Finish(ctx, running.Lease(), f.now, models.ArchiveJobOutcome{State: "succeeded", Result: []byte(`{}`)})
		require.ErrorContains(t, err, "detail success requires its retained comparison")
		return nil
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	_, err = f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.body)
	require.NoError(t, err)
	failure, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
	require.NoError(t, err)
	again, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
	require.NoError(t, err)
	require.Equal(t, failure, again)
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, f.now.Add(time.Hour), current.AvailableAt)
	require.Nil(t, f.claim(t, job, 1))
	f.now = f.now.Add(time.Hour)
	running = f.claim(t, job, 1)
	_, err = f.worker.Fail(t.Context(), f.tokens[1], running.Lease(), "authentication")
	require.NoError(t, err)
	retry, err := f.worker.Retry(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	repeated, err := f.worker.Retry(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	require.Equal(t, retry, repeated)
	require.Equal(t, f.now.Add(24*time.Hour), retry.AvailableAt)
	require.Nil(t, f.claim(t, retry, 0))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

type changingDetailStore struct {
	models.DiscoveryDetailReaderWriter
	change func(context.Context) error
}

func (s changingDetailStore) Complete(ctx context.Context, lease models.EnrichmentJobLease, revision int, digest string, now time.Time) (*models.DiscoveryDetailResult, error) {
	txn.AddPreCommitHook(ctx, s.change)
	return s.DiscoveryDetailReaderWriter.Complete(ctx, lease, revision, digest, now)
}
func TestDiscoveryDetailCompletionRechecksAuthorityAndSourceAtCommit(t *testing.T) {
	for _, mode := range []string{"deadline", "source", "credential"} {
		t.Run(mode, func(t *testing.T) {
			f := newDetailFixture(t)
			running := f.claim(t, f.admit(t), 0)
			checkpoint, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.body)
			require.NoError(t, err)
			credential, err := f.service.Authenticate(t.Context(), f.tokens[0])
			require.NoError(t, err)
			f.service.Repo.DiscoveryDetail = changingDetailStore{DiscoveryDetailReaderWriter: f.repo.DiscoveryDetail, change: func(ctx context.Context) error {
				switch mode {
				case "deadline":
					f.now = f.now.Add(time.Minute)
					return nil
				case "credential":
					return f.repo.Ingest.RevokeCredential(ctx, credential.UUID)
				default:
					collection, err := f.repo.SourceCollection.Find(ctx, f.listing.CollectionUUID)
					if err != nil {
						return err
					}
					definition := collection.SourceCollectionDefinition
					definition.State = "disabled"
					_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
					return err
				}
			}}
			_, err = f.worker.Complete(t.Context(), f.tokens[0], running.Lease(), checkpoint.Revision, checkpoint.Digest)
			switch mode {
			case "deadline":
				require.ErrorIs(t, err, models.ErrArchiveJobLease)
			case "source":
				require.ErrorIs(t, err, models.ErrDiscoveryConflict)
			default:
				require.ErrorIs(t, err, ingest.ErrUnauthorized)
			}
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_detail_results"))
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE kind='post.verify_candidate' AND state='running'"))
		})
	}
}

func TestDiscoveryDetailChildCooldownIsSharedAndSurvivesReopen(t *testing.T) {
	f := newDetailFixture(t)
	running := f.claim(t, f.admit(t), 0)
	partial := detailPartial(t, f.body)
	_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, partial)
	require.NoError(t, err)
	ready, err := f.worker.ReserveSource(t.Context(), f.tokens[0], running.Lease(), "https://imgur.com/child")
	require.NoError(t, err)
	require.True(t, ready)
	_, err = f.worker.ReserveSource(t.Context(), f.tokens[0], running.Lease(), "https://unrelated.invalid/private")
	require.Error(t, err)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var until int64
	require.NoError(t, raw.QueryRow("SELECT available_at_ms FROM source_pacing WHERE scope='service:imgur'").Scan(&until))
	require.Equal(t, f.now.Add(time.Hour).UnixMilli(), until)
	require.NoError(t, raw.QueryRow("SELECT available_at_ms FROM source_pacing WHERE scope='service:reddit'").Scan(&until))
	require.Zero(t, until)
	// An exact-time claim records a waiter for both the parent and the saved child.
	f.now = f.now.Add(time.Hour)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, "UPDATE source_pacing SET available_at_ms=? WHERE scope='service:imgur'", []any{f.now.Add(time.Hour).UnixMilli()})
		return err
	}))
	require.Nil(t, f.claim(t, running, 1))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_enrichment_waiter_scopes"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestDiscoveryDetailNewComparisonCancelsOnlyObsoleteSelectedWork(t *testing.T) {
	f := newDetailFixture(t)
	old := f.admit(t)
	// A queued detail does not prevent the next retained listing batch from being compared.
	final := listingContinuation(t, f.page, map[string]string{"after": "t3_abc123"}, nil, true)
	f.append(t, final)
	f.advance(t, 1)
	current := f.review(t)
	require.Equal(t, 1, current.CandidateCount)
	f.input.ExpectedTargetRevision = current.Target.Revision
	newer := f.admit(t)
	require.NotEqual(t, old.UUID, newer.UUID)
	previous, err := f.worker.Find(t.Context(), f.tokens[0], old.UUID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", previous.State)
	running := f.claim(t, newer, 1)
	require.NotNil(t, running)
	checkpoint, err := f.worker.Checkpoint(t.Context(), f.tokens[1], running.Lease(), 0, f.body)
	require.NoError(t, err)
	result, err := f.worker.Complete(t.Context(), f.tokens[1], running.Lease(), checkpoint.Revision, checkpoint.Digest)
	require.NoError(t, err)
	current.Detail, current.Blockers = result, []string{}
	require.Equal(t, current, f.review(t))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}
