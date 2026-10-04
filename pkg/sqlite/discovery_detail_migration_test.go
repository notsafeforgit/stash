package sqlite_test

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeDiscoveryDetailSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryDetailPublicationSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER discovery_detail_success; DROP INDEX archive_jobs_discovery_detail;
 DROP TABLE discovery_detail_results; DROP TABLE discovery_detail_checkpoints;
 DROP TABLE discovery_detail_checkpoint_records; DROP TABLE discovery_detail_checkpoint_receipts;
 DROP TABLE discovery_detail_checkpoint_usage; DROP TABLE discovery_detail_attempts; DROP TABLE discovery_detail_jobs;
 DELETE FROM native_migration_history WHERE version=1000076`)
	require.NoError(t, err)
}
func TestDiscoveryDetailMigrationPreservesListingRecoveryAndPacing(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newDiscoveryMatchHistoryFixture(t, true)
			recoverDiscoveryFixture(t, f)
			f.append(t, f.page)
			f.advance(t, 0)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removeDiscoveryDetailSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000075,dirty=0")
			require.NoError(t, err)
			tables := []string{"archive_jobs", "archive_job_attempts", "archive_job_submissions", "discovery_listings", "discovery_listing_jobs", "discovery_listing_recoveries", "discovery_recovery_targets", "discovery_pages", "discovery_match_targets", "discovery_match_evidence", "enrichment_job_pacing", "enrichment_attempt_pacing", "source_pacing", "source_service_turns"}
			before := map[string][][]any{}
			for _, name := range tables {
				before[name] = albumJobRows(t, raw, name)
			}
			if collision {
				_, err := raw.Exec("CREATE TABLE discovery_detail_jobs(original TEXT); INSERT INTO discovery_detail_jobs VALUES('preserve')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			if collision {
				require.Error(t, f.db.RunAllMigrations())
				var value string
				require.NoError(t, raw.QueryRow("SELECT original FROM discovery_detail_jobs").Scan(&value))
				require.Equal(t, "preserve", value)
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				require.Equal(t, f.db.AppSchemaVersion(), f.db.Version())
				for _, name := range []string{"enrichment_job_success", "discovery_listing_success", "discovery_recovery_job_scope", "discovery_recovery_page_scope", "archive_jobs_discovery_listing", "source_enrichment_waiter_end"} {
					require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='"+name+"'"), name)
				}
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='native_detail_archive_job_rows'"))
			for name, values := range before {
				require.Equal(t, values, albumJobRows(t, raw, name), name)
			}
		})
	}
}
func TestDiscoveryDetailCorruptionPreventsOpeningWriter(t *testing.T) {
	for _, change := range []struct{ table, guard, set string }{
		{"discovery_detail_checkpoints", "discovery_detail_checkpoint_transition", "body=replace(body,'2026-10-04','2026-10-03')"},
		{"discovery_detail_checkpoint_records", "discovery_detail_checkpoint_record_immutable", "digest='" + strings.Repeat("f", 64) + "'"},
		{"discovery_detail_results", "discovery_detail_result_immutable", "evidence=json_set(evidence,'$.basis','invented')"},
		{"discovery_detail_jobs", "discovery_detail_job_immutable", "generation=2"},
	} {
		t.Run(change.table, func(t *testing.T) {
			f := newDetailFixture(t)
			running := f.claim(t, f.admit(t), 0)
			checkpoint, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.body)
			require.NoError(t, err)
			_, err = f.worker.Complete(t.Context(), f.tokens[0], running.Lease(), checkpoint.Revision, checkpoint.Digest)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var guard string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", change.guard).Scan(&guard))
			_, err = raw.Exec("DROP TRIGGER " + change.guard + "; UPDATE " + change.table + " SET " + change.set + "; " + guard)
			require.NoError(t, err)
			require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
		})
	}
}
func TestDiscoveryDetailAnonymisationRemovesPrivateTranscripts(t *testing.T) {
	f := newDetailFixture(t)
	running := f.claim(t, f.admit(t), 0)
	_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.body)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, path)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	checked := sqlite.NewDatabase()
	defer checked.Close()
	require.NoError(t, checked.Open(path))
	raw := openRawDB(t, path)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_detail_jobs"))
	require.Zero(t, queryUint(t, raw, "SELECT byte_size FROM discovery_detail_checkpoint_usage"))
}
