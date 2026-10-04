package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Construct the actual predecessor layout, retaining all original receipt
// columns and guards rather than just changing its version marker.
func removeDiscoveryDetailPublicationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	legacy, err := os.ReadFile("migrations/1000074_discovery_publications.up.sql")
	require.NoError(t, err)
	definition := strings.Split(string(legacy), "CREATE TABLE discovery_published_records")[0]
	guard := strings.Split(strings.Split(string(legacy), "CREATE TRIGGER discovery_published_record_scope")[1], "INSERT INTO native_migration_history")[0]
	_, err = raw.Exec(`PRAGMA foreign_keys=OFF; BEGIN;
 CREATE TABLE original_publication_rows AS SELECT target_uuid,target_revision,listing_uuid,page_ordinal,page_sha256,post_uuid,post_revision,
 namespace,value,policy,basis,witness_ordinal,evidence_uuid,url_evidence_uuid,record_count,capture_count,created_at FROM discovery_match_publications;
 DROP TABLE discovery_match_publications;
 ` + definition + `
 INSERT INTO discovery_match_publications SELECT * FROM original_publication_rows;
 DROP TABLE original_publication_rows;
 DROP TRIGGER discovery_published_record_scope;
 CREATE TRIGGER discovery_published_record_scope` + guard + `
 DROP INDEX discovery_detail_candidate_history;
 DELETE FROM native_migration_history WHERE version=1000077;
 COMMIT; PRAGMA foreign_keys=ON;`)
	require.NoError(t, err)
}

func TestDiscoveryDetailPublicationMigrationPreservesPriorEvidence(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			detail := newDetailFixture(t)
			completeDetailFixture(t, detail)
			f := detail.discoveryMatchFixture
			f.append(t, discoveryReviewFinalPage(t, f.page, map[string]string{"after": "t3_abc123"}, false))
			f.advance(t, 1)
			prepared := prepareDiscoveryPublication(t, f)
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := prepared.Publish(ctx, discoveryCaptureWriter(f), f.now)
				return err
			}))
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removeDiscoveryDetailPublicationSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000076,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, name := range []string{"discovery_published_records", "discovery_detail_jobs", "discovery_detail_results", "discovery_detail_attempts", "discovery_detail_checkpoint_receipts", "discovery_detail_checkpoint_records", "discovery_detail_checkpoints", "discovery_detail_checkpoint_usage", "discovery_match_targets", "discovery_match_evidence", "discovery_match_pages", "discovery_pages", "discovery_listings", "source_posts", "source_post_identifier_evidence", "source_captures", "source_collection_captures", "archive_jobs", "archive_job_attempts", "archive_job_submissions", "enrichment_job_pacing"} {
				before[name] = albumJobRows(t, raw, name)
			}
			priorRows := albumJobRows(t, raw, "discovery_match_publications")
			if collision {
				_, err := raw.Exec("CREATE TABLE native_discovery_publication_rows(original TEXT); INSERT INTO native_discovery_publication_rows VALUES('preserve unknown input')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			if collision {
				require.Error(t, f.db.RunAllMigrations())
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM native_discovery_publication_rows").Scan(&original))
				require.Equal(t, "preserve unknown input", original)
				require.Equal(t, priorRows, albumJobRows(t, raw, "discovery_match_publications"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				require.Equal(t, f.db.AppSchemaVersion(), f.db.Version())
				actual := albumJobRows(t, raw, "discovery_match_publications")
				require.Len(t, actual, len(priorRows))
				for i, row := range actual {
					require.Equal(t, priorRows[i], row[:len(row)-1])
					require.Nil(t, row[len(row)-1])
				}
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			}
			for name, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, name), name)
			}
		})
	}
}

func TestDiscoveryDetailPublicationCorruptionPreventsOpen(t *testing.T) {
	for _, change := range []string{
		"DELETE FROM discovery_published_records",
		"UPDATE discovery_match_publications SET witness_ordinal=witness_ordinal+1",
		"UPDATE discovery_match_publications SET page_sha256='" + strings.Repeat("f", 64) + "'",
		"UPDATE discovery_detail_results SET evidence=json_set(evidence,'$.basis','exact-original-text-and-date')",
	} {
		t.Run(change, func(t *testing.T) {
			f := newDetailFixture(t)
			completeDetailFixture(t, f)
			finishDetailListing(t, f)
			publishDetailFixture(t, f)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var publication, result string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='discovery_match_publication_immutable'").Scan(&publication))
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='discovery_detail_result_immutable'").Scan(&result))
			_, err := raw.Exec("DROP TRIGGER discovery_match_publication_immutable; DROP TRIGGER discovery_detail_result_immutable; " + change + "; " + publication + "; " + result)
			require.NoError(t, err)
			require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
		})
	}
}
