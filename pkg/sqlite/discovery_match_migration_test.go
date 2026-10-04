package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryComparisonMigrationPreservesSourcePagesAndImportEvidence(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newDiscoveryMatchFixture(t)
			f.append(t, f.page)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removeDiscoveryMatchSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000071,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"discovery_listings", "discovery_listing_legacy", "discovery_listing_jobs", "discovery_job_attempts", "discovery_pages", "archive_jobs", "archive_job_attempts", "archive_job_submissions", "enrichment_job_pacing", "enrichment_attempt_pacing", "source_pacing", "source_service_turns", "automation_snapshots", "automation_snapshot_records", "automation_discovery_imports", "automation_discovery_records", "source_posts", "source_post_identifiers"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE discovery_match_pages(original TEXT); INSERT INTO discovery_match_pages VALUES('preserve unknown input')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			if collision {
				require.Error(t, f.db.RunAllMigrations())
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM discovery_match_pages").Scan(&original))
				require.Equal(t, "preserve unknown input", original)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='discovery_match_targets'"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				require.Equal(t, f.db.AppSchemaVersion(), f.db.Version())
				for _, table := range []string{"discovery_match_targets", "discovery_match_pages", "discovery_match_candidates", "discovery_match_evidence"} {
					require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), "migration must not activate comparisons")
				}
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}

func TestDiscoveryComparisonReopenRejectsCorruptReferencesAndReceipts(t *testing.T) {
	for _, test := range []struct{ name, guard, change string }{
		{"receipt", "discovery_match_page_immutable", "UPDATE discovery_match_pages SET matches_sha256='" + strings.Repeat("f", 64) + "'"},
		{"ordinals", "discovery_match_evidence_immutable", "UPDATE discovery_match_evidence SET record_ordinals='[1,0]'"},
		{"lost-reference", "discovery_match_evidence_immutable", "UPDATE discovery_match_evidence SET record_ordinals='[0,1]'"},
		{"future-revision", "discovery_match_target_transition", "UPDATE discovery_match_targets SET post_revision=post_revision+100"},
		{"missing-candidate", "", "PRAGMA foreign_keys=OFF; DELETE FROM discovery_match_candidates"},
		{"missing-guard", "discovery_match_target_scope", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newDiscoveryMatchFixture(t)
			f.append(t, f.page)
			require.NotNil(t, f.advance(t, 0))
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var guard string
			if test.guard != "" {
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", test.guard).Scan(&guard))
				_, err := raw.Exec("DROP TRIGGER " + test.guard)
				require.NoError(t, err)
			}
			if test.change != "" {
				_, err := raw.Exec(test.change)
				require.NoError(t, err)
				if guard != "" {
					_, err = raw.Exec(guard)
					require.NoError(t, err)
				}
			}
			err := f.db.Open(f.db.DatabasePath())
			if test.name == "missing-guard" {
				require.ErrorContains(t, err, "missing discovery_match_target_scope")
			} else {
				require.ErrorIs(t, err, models.ErrSourcePayloadCorrupt)
			}
		})
	}
}

func TestDiscoveryComparisonAnonymisationRemovesPrivateEvidenceOnlyFromExport(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	f.append(t, f.page)
	f.advance(t, 0)
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, path)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	checked := sqlite.NewDatabase()
	defer checked.Close()
	require.NoError(t, checked.Open(path))
	raw := openRawDB(t, path)
	defer raw.Close()
	for _, table := range []string{"discovery_match_targets", "discovery_match_pages", "discovery_match_candidates", "discovery_match_evidence"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		candidates, err := f.repo.DiscoveryMatch.Candidates(ctx, f.target.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, candidates, 1)
		return nil
	}))
}
