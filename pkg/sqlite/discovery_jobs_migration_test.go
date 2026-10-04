package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryListingMigrationPreservesCheckpointAndPacingHistory(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removeDiscoveryListingSchema(t, raw)
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000070,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"archive_jobs", "archive_job_attempts", "archive_job_submissions", "enrichment_job_targets", "enrichment_job_attempts", "enrichment_checkpoints", "enrichment_checkpoint_receipts", "enrichment_checkpoint_records", "enrichment_job_pacing", "enrichment_attempt_pacing", "source_pacing", "source_service_turns", "scenes", "images", "performers", "source_accounts"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE discovery_listings(original TEXT); INSERT INTO discovery_listings VALUES('preserve original')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			if collision {
				require.Error(t, f.db.RunAllMigrations())
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM discovery_listings").Scan(&original))
				require.Equal(t, "preserve original", original)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='native_listing_archive_job_rows'"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				require.Equal(t, sqlite.NativeSchemaBaseline+71, f.db.Version())
				for _, name := range []string{"archive_jobs_running_resource", "archive_jobs_translation_request", "archive_jobs_enrichment_target", "enrichment_job_success", "source_enrichment_waiter_end", "archive_job_identity", "archive_job_transition"} {
					var count int
					require.NoError(t, raw.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name=?", name).Scan(&count))
					require.Equal(t, 1, count, name)
				}
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}

func TestDiscoveryListingCorruptEvidenceIsRefusedBeforeWrites(t *testing.T) {
	for _, change := range []string{"digest='" + strings.Repeat("f", 64) + "'", "producer_uuid=(SELECT uuid FROM ingest_producers WHERE uuid!=discovery_pages.producer_uuid LIMIT 1)"} {
		t.Run(change, func(t *testing.T) {
			f := newListingFixture(t)
			f.admit(t)
			job := f.claim(t, 0)
			_, err := f.append(t, models.DiscoveryJobLease{ArchiveJobLease: job.Lease(), ProducerUUID: f.producers[0].UUID}, 1, f.page)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var guard string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='discovery_page_immutable'").Scan(&guard))
			_, err = raw.Exec("DROP TRIGGER discovery_page_immutable; UPDATE discovery_pages SET " + change + "; " + guard)
			require.NoError(t, err)
			require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
		})
	}
}

func TestDiscoveryListingChangedSourceStopsNewWorkButKeepsReceipts(t *testing.T) {
	f := newListingFixture(t)
	f.admit(t)
	job := f.claim(t, 0)
	lease := models.DiscoveryJobLease{ArchiveJobLease: job.Lease(), ProducerUUID: f.producers[0].UUID}
	page, err := f.append(t, lease, 1, f.page)
	require.NoError(t, err)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := f.collection.SourceCollectionDefinition
		definition.State = "disabled"
		_, err := f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	replay, err := f.append(t, lease, 1, f.page)
	require.NoError(t, err)
	require.Equal(t, page, replay)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryJob.Admit(ctx, f.listing.UUID, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}
