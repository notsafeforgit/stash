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

func TestDiscoveryMatchPublicationMigrationPreservesActivationAndComparison(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newDiscoveryPublicationFixture(t)
			plan := previewDiscoveryActivation(t, f, discoveryActivationInput(f))
			_, err := activateDiscoveryPlan(t, f, plan)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removeDiscoveryPublicationSchema(t, raw)
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000073,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"discovery_activations", "discovery_activation_targets", "discovery_match_targets", "discovery_match_pages", "discovery_match_candidates", "discovery_match_evidence", "discovery_pages", "discovery_listings", "discovery_listing_jobs", "source_posts", "source_post_identifiers", "source_captures", "archive_jobs", "automation_discovery_records"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE discovery_published_records(original TEXT); INSERT INTO discovery_published_records VALUES('preserve unknown input')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			if collision {
				require.Error(t, f.db.RunAllMigrations())
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM discovery_published_records").Scan(&original))
				require.Equal(t, "preserve unknown input", original)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='discovery_match_publications'"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				require.Equal(t, f.db.AppSchemaVersion(), f.db.Version())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_match_publications"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_published_records"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}

func TestDiscoveryMatchPublicationCorruptionIsRejectedByAudit(t *testing.T) {
	for _, change := range []string{"DELETE FROM discovery_published_records", "UPDATE discovery_match_publications SET page_sha256='" + strings.Repeat("f", 64) + "'"} {
		t.Run(change, func(t *testing.T) {
			f := newDiscoveryPublicationFixture(t)
			prepared := prepareDiscoveryPublication(t, f)
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := prepared.Publish(ctx, discoveryCaptureWriter(f), f.now)
				return err
			}))
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var guard string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='discovery_match_publication_immutable'").Scan(&guard))
			_, err := raw.Exec("DROP TRIGGER discovery_match_publication_immutable; " + change + "; " + guard)
			require.NoError(t, err)
			require.ErrorIs(t, f.db.AuditForTesting(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
		})
	}
}
