package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Exercise promotion of an actual schema-52-style publication, whose verified
// native captures existed before staging release was available.
type enrichmentKeepCheckpoint struct {
	models.EnrichmentJobReaderWriter
}

func (s enrichmentKeepCheckpoint) ReleaseCheckpoint(context.Context, string, time.Time) (*models.EnrichmentCheckpointRelease, error) {
	return nil, nil
}

func TestEnrichmentCheckpointReleaseMigrationPreservesStagingUntilVerified(t *testing.T) {
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			f.service.Repo.EnrichmentJob = enrichmentKeepCheckpoint{f.repo.EnrichmentJob}
			job := f.admitV1(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			removeSourcePacingSchema(t, raw)
			_, err = raw.Exec(`DROP TRIGGER enrichment_checkpoint_published_delete; DROP TABLE enrichment_checkpoint_releases;
 DELETE FROM native_migration_history WHERE version=1000053; UPDATE schema_migrations SET version=1000052;`)
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"enrichment_publications", "enrichment_published_records", "enrichment_checkpoints", "enrichment_checkpoint_records", "enrichment_checkpoint_receipts", "enrichment_checkpoint_usage", "source_captures", "archive_jobs", "archive_job_attempts"} {
				before[table] = albumJobRows(t, raw, table)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(path), &needed))
			if collision {
				_, err = raw.Exec("CREATE TABLE enrichment_checkpoint_releases(retained TEXT); INSERT INTO enrichment_checkpoint_releases VALUES('Original data')")
				require.NoError(t, err)
			}
			err = f.db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
				var original string
				require.NoError(t, raw.QueryRow("SELECT retained FROM enrichment_checkpoint_releases").Scan(&original))
				require.Equal(t, "Original data", original)
			} else {
				require.NoError(t, err)
				require.NoError(t, f.db.ReInitialise())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoint_releases"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			if !collision {
				f.service.Repo = f.db.Repository()
				f.now = f.now.Add(24 * time.Hour)
				release, err := f.worker.ReleaseCheckpoint(t.Context(), f.tokens[0], job.UUID)
				require.NoError(t, err)
				require.NotNil(t, release)
				require.Zero(t, queryUint(t, raw, "SELECT byte_size FROM enrichment_checkpoint_usage"))
				require.NoError(t, f.db.Close())
				require.NoError(t, f.db.Open(path))
			}
		})
	}
}

func TestEnrichmentCheckpointReleaseRefusesUnpublishedEvidence(t *testing.T) {
	for _, state := range []string{"running", "failed", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			if state != "running" {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					if state == "cancelled" {
						current, err := f.repo.ArchiveJob.Find(ctx, job.UUID)
						if err != nil {
							return err
						}
						_, err = f.repo.ArchiveJob.Cancel(ctx, job.UUID, current.Revision, f.now)
						return err
					}
					_, err := f.repo.ArchiveJob.Finish(ctx, running.Lease(), f.now, models.ArchiveJobOutcome{State: state, ErrorCode: "worker_failed", Result: json.RawMessage(`{}`)})
					return err
				}))
			}
			_, err = f.worker.ReleaseCheckpoint(t.Context(), f.tokens[0], job.UUID)
			require.Error(t, err)
			head, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
			require.NoError(t, err)
			require.NotNil(t, head)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.EqualValues(t, len(head.Body), queryUint(t, raw, "SELECT byte_size FROM enrichment_checkpoint_usage"))
		})
	}
}

func TestEnrichmentCheckpointReleaseDeleteFailureRollsBackPublication(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("CREATE TRIGGER reject_release BEFORE DELETE ON enrichment_checkpoints BEGIN SELECT RAISE(ABORT,'injected release failure'); END")
	require.NoError(t, err)
	_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.ErrorContains(t, err, "injected release failure")
	for _, table := range []string{"source_captures", "enrichment_publications", "enrichment_checkpoint_releases", "enrichment_completions"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE state='running'"))
	require.NotZero(t, queryUint(t, raw, "SELECT byte_size FROM enrichment_checkpoint_usage"))
}

func TestEnrichmentCheckpointReleaseSwallowedFailureDoesNotCommit(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	f.service.Repo.EnrichmentJob = enrichmentKeepCheckpoint{f.repo.EnrichmentJob}
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
	require.NoError(t, err)
	_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	used := queryUint(t, raw, "SELECT byte_size FROM enrichment_checkpoint_usage")
	_, err = raw.Exec("CREATE TRIGGER reject_release BEFORE DELETE ON enrichment_checkpoints BEGIN SELECT RAISE(ABORT,'injected release failure'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.EnrichmentJob.ReleaseCheckpoint(ctx, job.UUID, f.now)
		require.ErrorContains(t, err, "injected release failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoint_releases"))
	require.Equal(t, used, queryUint(t, raw, "SELECT byte_size FROM enrichment_checkpoint_usage"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE state='succeeded'"))
}

func TestEnrichmentCheckpointReleaseStartupRejectsChangedEvidence(t *testing.T) {
	for _, mutation := range []string{"references", "receipt", "missing_release", "payload", "collection"} {
		t.Run(mutation, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			var trigger, query string
			switch mutation {
			case "references":
				trigger, query = "enrichment_checkpoint_release_immutable", "UPDATE enrichment_checkpoint_releases SET unresolved=json_set(unresolved,'$[0].url','https://example.org/another')"
			case "receipt":
				trigger, query = "enrichment_checkpoint_record_immutable", "UPDATE enrichment_checkpoint_records SET digest=printf('%064d',0) WHERE ordinal=0"
			case "missing_release":
				query = "DELETE FROM enrichment_checkpoint_releases"
			case "payload":
				// Native payload validation remains necessary after removing staging.
				trigger, query = "source_payload_immutable", "UPDATE source_payloads SET data=x'7b7d' WHERE digest=(SELECT body_digest FROM source_post_revisions LIMIT 1)"
			case "collection":
				query = "DELETE FROM capture_translation_decisions; DELETE FROM source_collection_captures"
			}
			var definition string
			if trigger != "" {
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", trigger).Scan(&definition))
				_, err = raw.Exec("DROP TRIGGER " + trigger)
				require.NoError(t, err)
			}
			_, err = raw.Exec(query)
			require.NoError(t, err)
			if trigger != "" {
				_, err = raw.Exec(definition)
				require.NoError(t, err)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			check := sqlite.NewDatabase()
			require.Error(t, check.Open(path))
			require.NoError(t, check.Close())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after, "corruption is refused before opening a writer")
		})
	}
}
