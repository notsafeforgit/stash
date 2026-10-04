package sqlite_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryActivationMigrationPreservesOriginalPagesAndComparisons(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newDiscoveryMatchFixture(t)
			f.append(t, f.page)
			f.advance(t, 0)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removeDiscoveryActivationSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000072,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"discovery_listings", "discovery_listing_legacy", "discovery_listing_jobs", "discovery_job_attempts", "discovery_pages", "discovery_match_targets", "discovery_match_pages", "discovery_match_candidates", "discovery_match_evidence", "automation_discovery_records", "automation_snapshot_records", "archive_jobs", "archive_job_attempts", "source_pacing", "source_posts", "source_post_identifiers"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE discovery_activation_targets(original TEXT); INSERT INTO discovery_activation_targets VALUES('preserve unknown input')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			if collision {
				require.Error(t, f.db.RunAllMigrations())
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM discovery_activation_targets").Scan(&original))
				require.Equal(t, "preserve unknown input", original)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='discovery_activations'"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				require.Equal(t, f.db.AppSchemaVersion(), f.db.Version())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_activations"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_activation_targets"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}

func TestDiscoveryActivationCorruptReceiptOrLostBindingPreventsOpen(t *testing.T) {
	for _, change := range []string{"UPDATE discovery_activations SET plan_sha256='" + strings.Repeat("f", 64) + "'", "DELETE FROM discovery_activation_targets"} {
		t.Run(change, func(t *testing.T) {
			f := newDiscoveryMatchFixture(t)
			plan := previewDiscoveryActivation(t, f, discoveryActivationInput(f))
			_, err := activateDiscoveryPlan(t, f, plan)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var guard string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='discovery_activation_immutable'").Scan(&guard))
			_, err = raw.Exec("DROP TRIGGER discovery_activation_immutable; " + change + "; " + guard)
			require.NoError(t, err)
			require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
		})
	}
}
