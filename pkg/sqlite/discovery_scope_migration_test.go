package sqlite_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeDiscoveryScopeSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeAttachmentSelectionReviewSchema(t, raw)
	for _, trigger := range []string{"discovery_listing_job_scope", "discovery_detail_job_scope", "discovery_listing_recovery_scope"} {
		var body string
		require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", trigger).Scan(&body))
		_, err := raw.Exec("DROP TRIGGER " + trigger)
		require.NoError(t, err)
		_, err = raw.Exec(strings.ReplaceAll(body, "discovery_effective_listings", "discovery_listings"))
		require.NoError(t, err)
	}
	_, err := raw.Exec(`DROP VIEW discovery_effective_listings; DROP TABLE discovery_scope_reviews;
 DELETE FROM native_migration_history WHERE version=1000084`)
	require.NoError(t, err)
}

func TestDiscoveryScopeMigrationPreservesOriginalSearchesAndUnknownObjects(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "migration", true: "collision"}[collision], func(t *testing.T) {
			f := newDiscoveryMatchHistoryFixture(t, true)
			recoverDiscoveryFixture(t, f)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removeDiscoveryScopeSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000083,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"discovery_listings", "discovery_listing_recoveries", "discovery_match_targets", "discovery_recovery_targets", "discovery_activations", "source_posts", "source_collection_revisions"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE discovery_scope_reviews(original TEXT); INSERT INTO discovery_scope_reviews VALUES('keep unknown data')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			err = f.db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				var value string
				require.NoError(t, raw.QueryRow("SELECT original FROM discovery_scope_reviews").Scan(&value))
				require.Equal(t, "keep unknown data", value)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='discovery_effective_listings'"))
			} else {
				require.NoError(t, err)
				require.NoError(t, f.db.ReInitialise())
				require.NoError(t, f.db.Close())
				require.NoError(t, f.db.Open(f.db.DatabasePath()))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_scope_reviews"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}

func TestDiscoveryScopeStartupRefusesCorruptHistoryAndProjectionBeforeWriting(t *testing.T) {
	for _, projection := range []bool{false, true} {
		t.Run(map[bool]string{false: "historical definition", true: "effective view"}[projection], func(t *testing.T) {
			f := newDiscoveryMatchFixture(t)
			collection, _ := changedDiscoveryScope(t, f, "active")
			plan := previewDiscoveryScope(t, f, collection)
			_, err := applyDiscoveryScope(t, f, plan)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			if projection {
				_, err = raw.Exec("DROP VIEW discovery_effective_listings; CREATE VIEW discovery_effective_listings AS SELECT * FROM discovery_listings")
				require.NoError(t, err)
			} else {
				var trigger string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='discovery_scope_review_immutable'").Scan(&trigger))
				_, err = raw.Exec("DROP TRIGGER discovery_scope_review_immutable")
				require.NoError(t, err)
				plan.Collection.Label = "Incorrect historical label with a consistent plan hash"
				plan.PlanSHA256, err = archive.DiscoveryScopeDigest(*plan)
				require.NoError(t, err)
				body, err := json.Marshal(plan)
				require.NoError(t, err)
				_, err = raw.Exec("UPDATE discovery_scope_reviews SET plan=?,plan_sha256=?", string(body), plan.PlanSHA256)
				require.NoError(t, err)
				_, err = raw.Exec(trigger)
				require.NoError(t, err)
			}
			before, err := os.ReadFile(f.db.DatabasePath())
			require.NoError(t, err)
			require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "invalid discovery collection bindings")
			after, err := os.ReadFile(f.db.DatabasePath())
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
