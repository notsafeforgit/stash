package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentPublicationCompletesNativeCapturesWithOriginalProvenance(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.TranslationPolicy.Put(ctx, models.TranslationPolicyInput{CollectionUUID: f.collection.UUID, ExpectedCollectionRevision: f.collection.Revision,
			Origin: "review", Definition: models.TranslationPolicyDefinition{Enabled: true, ProviderPolicy: models.TranslationBingTextV1, TargetLanguage: "en", Title: true, Caption: true, Priority: 100}})
		return err
	}))
	job := f.admit(t)
	first := f.claim(t, job.UUID, 0)
	initial, err := f.worker.Checkpoint(t.Context(), f.tokens[0], first.Lease(), 0, f.initial)
	require.NoError(t, err)
	_, err = f.worker.Publish(t.Context(), f.tokens[0], first.Lease(), initial.Revision, initial.Digest)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict, "pending children cannot certify completion")
	f.now = f.now.Add(2 * time.Minute)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.repo.ArchiveJob.Recover(ctx, f.now, 1); return err }))
	f.now = f.now.Add(5 * time.Minute)
	second := f.claim(t, job.UUID, 1)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[1], second.Lease(), initial.Revision, f.complete)
	require.NoError(t, err)
	_, err = f.worker.Publish(t.Context(), f.tokens[0], second.Lease(), head.Revision, head.Digest)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	publication, err := f.worker.Publish(t.Context(), f.tokens[1], second.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	require.Equal(t, 5, publication.RecordCount)
	require.Equal(t, 5, publication.CaptureCount)
	require.Equal(t, 1, publication.UnresolvedCount, "unsupported external references remain visible limitations")
	require.EqualValues(t, 2, publication.Fence)
	released, err := f.worker.CheckpointRelease(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	require.NotNil(t, released)
	transcript, err := archive.ParseEnrichmentTranscript(f.complete)
	require.NoError(t, err)
	require.Equal(t, len(transcript.Body()), released.CheckpointBytes)
	require.Equal(t, transcript.Unresolved, released.Unresolved)
	staging, err := f.worker.CheckpointHead(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	require.Nil(t, staging)
	firstReplay, err := f.worker.Checkpoint(t.Context(), f.tokens[0], first.Lease(), 0, f.initial)
	require.NoError(t, err)
	require.Equal(t, initial, firstReplay, "original acknowledgement survives release and worker handoff")
	var records []models.EnrichmentPublishedRecord
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		records, err = f.repo.EnrichmentJob.PublishedRecords(ctx, job.UUID, -1, 100)
		if err != nil {
			return err
		}
		require.Len(t, records, 5)
		for i, record := range records {
			producer := 0
			if i >= 3 {
				producer = 1
			}
			require.Equal(t, f.producers[producer].UUID, record.ProducerUUID)
			capture, err := f.repo.SourceEvidence.FindCapture(ctx, record.CaptureUUID)
			require.NoError(t, err)
			require.Equal(t, f.target.PostUUID, capture.PostUUID)
			require.Equal(t, "Shared caption", *capture.Metadata.Title, "child hosts retain the Reddit post's caption")
			require.Equal(t, "gallery-dl", capture.Origin)
			require.Equal(t, time.Date(2026, 10, 3, 1, 0, 0, 123456000, time.UTC), capture.CapturedAt)
			_, err = archive.RestoreCapture(capture.Payload)
			require.NoError(t, err)
		}
		target, err := f.repo.EnrichmentWork.Target(ctx, f.target.UUID)
		require.NoError(t, err)
		require.Equal(t, "completed", target.State)
		require.Equal(t, publication.CompletionUUID, *target.CompletionUUID)
		completion, err := f.repo.EnrichmentWork.Completion(ctx, publication.CompletionUUID)
		require.NoError(t, err)
		require.Len(t, completion.CaptureUUIDs, 5)
		return nil
	}))
	status, err := f.worker.Find(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", status.State)
	want, err := archive.EnrichmentPublicationResult(*publication)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(status.Result))
	// An explicit audit validates the complete capture/job association, including
	// records whose observing producer differs from the publishing attempt.
	backup := filepath.Join(t.TempDir(), "published.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.AuditForTesting(backup))
	require.NoError(t, f.db.Open(backup))
	f.repo = f.db.Repository()
	f.worker.Service = ingest.New(f.repo)
	f.now = f.now.Add(24 * time.Hour)
	definition := f.collection.SourceCollectionDefinition
	definition.Label = "Renamed after completion"
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	replay, err := f.worker.Publish(t.Context(), f.tokens[1], second.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	require.Equal(t, publication, replay)
	finalReplay, err := f.worker.Checkpoint(t.Context(), f.tokens[1], second.Lease(), initial.Revision, f.complete)
	require.NoError(t, err)
	require.Equal(t, head, finalReplay)
	releaseReplay, err := f.worker.ReleaseCheckpoint(t.Context(), f.tokens[1], job.UUID)
	require.NoError(t, err)
	require.Equal(t, released, releaseReplay)
	_, err = f.worker.Publish(t.Context(), f.tokens[1], second.Lease(), head.Revision-1, head.Digest)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	raw := openRawDB(t, backup)
	defer raw.Close()
	require.EqualValues(t, 5, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM translation_requests"), "equal title/caption text shares a translation request")
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM translation_targets"))
	require.EqualValues(t, 5, queryUint(t, raw, "SELECT count(*) FROM capture_translation_decisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM ingest_receipts"), "enrichment has its own domain receipt, not an invented HTTP event")
	require.Zero(t, queryUint(t, raw, "SELECT byte_size FROM enrichment_checkpoint_usage"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	anonymousPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, anonymousPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	check := sqlite.NewDatabase()
	require.NoError(t, check.Open(anonymousPath))
	require.NoError(t, check.Close())
}

func TestEnrichmentPublicationSharesIdenticalContextCaptures(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	value, err := archive.DecodeJSONObject(f.complete, archive.MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	delete(value["records"].([]any)[2].(map[string]any)["patch"].(map[string]any), "context_marker")
	body, err := archive.EncodeSourceJSON(value)
	require.NoError(t, err)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, body)
	require.NoError(t, err)
	publication, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	require.Equal(t, 5, publication.RecordCount)
	require.Equal(t, 4, publication.CaptureCount)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		records, err := f.repo.EnrichmentJob.PublishedRecords(ctx, job.UUID, -1, 100)
		require.NoError(t, err)
		require.Equal(t, records[0].CaptureUUID, records[2].CaptureUUID)
		return nil
	}))
}

type enrichmentPublicationBeforeCommit struct {
	models.EnrichmentJobReaderWriter
	beforeCommit func(context.Context) error
}

func (s enrichmentPublicationBeforeCommit) Publish(ctx context.Context, lease models.EnrichmentJobLease, expected int, digest string, captures []string, now time.Time) (*models.EnrichmentPublication, error) {
	txn.AddPreCommitHook(ctx, s.beforeCommit)
	return s.EnrichmentJobReaderWriter.Publish(ctx, lease, expected, digest, captures, now)
}

func TestEnrichmentPublicationLateFailuresRollbackAllEffects(t *testing.T) {
	for _, mode := range []string{"capture_identity", "publication_write", "lease_expiry", "source_changed"} {
		t.Run(mode, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			body := f.complete
			if mode == "capture_identity" {
				value, err := archive.DecodeJSONObject(body, archive.MaxEnrichmentTranscriptBytes)
				require.NoError(t, err)
				records := value["records"].([]any)
				records[len(records)-1] = map[string]any{"kind": "post", "base": nil, "parent": nil, "removed": []any{}, "observed_at": "2026-10-03T01:00:00Z",
					"patch": map[string]any{"category": "reddit", "id": "another", "title": "Wrong post", "source_extractor_url": "https://www.reddit.com/comments/another"}}
				body, err = archive.EncodeSourceJSON(value)
				require.NoError(t, err)
			}
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, body)
			require.NoError(t, err)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			switch mode {
			case "publication_write":
				_, err = raw.Exec("CREATE TRIGGER reject_publication_record BEFORE INSERT ON enrichment_published_records WHEN NEW.ordinal=2 BEGIN SELECT RAISE(ABORT,'injected publication failure'); END")
				require.NoError(t, err)
			case "lease_expiry":
				f.service.Repo.EnrichmentJob = enrichmentPublicationBeforeCommit{f.repo.EnrichmentJob, func(context.Context) error { f.now = f.now.Add(2 * time.Minute); return nil }}
			case "source_changed":
				f.service.Repo.EnrichmentJob = enrichmentPublicationBeforeCommit{f.repo.EnrichmentJob, func(ctx context.Context) error {
					definition := f.collection.SourceCollectionDefinition
					definition.State = "disabled"
					_, err := f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
					return err
				}}
			}
			_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.Error(t, err)
			for _, table := range []string{"source_captures", "source_accounts", "source_post_revisions", "source_collection_captures", "enrichment_publications", "enrichment_published_records", "enrichment_completions", "translation_requests"} {
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
			}
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_posts"), "wrong post evidence must not create another post")
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE state='running'"))
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_targets WHERE state='pending' AND revision=1"))
		})
	}
}

func TestEnrichmentPublicationSuccessRequiresCompleteNativeProof(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.Finish(ctx, running.Lease(), f.now, models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{}`)})
		require.ErrorContains(t, err, "enrichment success requires")
		return nil // swallowing the SQL failure cannot commit the finished attempt
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	status, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "running", status.State)
}

func TestEnrichmentPublicationConcurrentResponseRecovery(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
	require.NoError(t, err)
	type result struct {
		publication *models.EnrichmentPublication
		err         error
	}
	start, results := make(chan struct{}), make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			publication, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			results <- result{publication, err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, first.publication, second.publication)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_publications"))
	require.EqualValues(t, 5, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
}

func TestEnrichmentPublicationAuditRejectsChangedCaptureLinks(t *testing.T) {
	for _, mutation := range []string{"missing", "relinked"} {
		t.Run(mutation, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			value, err := archive.DecodeJSONObject(f.complete, archive.MaxEnrichmentTranscriptBytes)
			require.NoError(t, err)
			value["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)["user"] = map[string]any{"id": "u1", "name": "source-account"}
			body, err := archive.EncodeSourceJSON(value)
			require.NoError(t, err)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, body)
			require.NoError(t, err)
			_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(path), "unaltered publication including shared profiles must reopen")
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			if mutation == "missing" {
				_, err = raw.Exec("DELETE FROM enrichment_published_records WHERE ordinal=1")
				require.NoError(t, err)
			} else {
				var trigger string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='enrichment_published_record_immutable'").Scan(&trigger))
				_, err = raw.Exec("DROP TRIGGER enrichment_published_record_immutable")
				require.NoError(t, err)
				// Swap two valid capture references. Cardinality, membership and all
				// foreign keys remain valid, but their observation provenance is wrong.
				var first, second string
				require.NoError(t, raw.QueryRow("SELECT capture_uuid FROM enrichment_published_records WHERE ordinal=0").Scan(&first))
				require.NoError(t, raw.QueryRow("SELECT capture_uuid FROM enrichment_published_records WHERE ordinal=1").Scan(&second))
				_, err = raw.Exec("UPDATE enrichment_published_records SET capture_uuid=CASE ordinal WHEN 0 THEN ? WHEN 1 THEN ? END WHERE ordinal IN (0,1)", second, first)
				require.NoError(t, err)
				_, err = raw.Exec(trigger)
				require.NoError(t, err)
				require.EqualValues(t, 5, queryUint(t, raw, "SELECT count(DISTINCT capture_uuid) FROM enrichment_published_records"))
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			check := sqlite.NewDatabase()
			require.Error(t, check.AuditForTesting(path))
			require.NoError(t, check.Close())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
