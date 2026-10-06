package sqlite_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeEnrichmentRebindSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryScopeSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE enrichment_rebinding_targets; DROP TABLE enrichment_rebindings;
DROP INDEX enrichment_targets_pending_scope; DELETE FROM native_migration_history WHERE version=1000083`)
	require.NoError(t, err)
}

func TestEnrichmentRebindMigrationPreservesPendingWorkAndUnknownObjects(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "migration", true: "collision"}[collision], func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			input, collection := enrichmentInput(t, repo)
			retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending", Priority: 71}, time.Now())
			changedEnrichmentCollection(t, repo, collection)
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			removeEnrichmentRebindSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000082,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"enrichment_targets", "enrichment_target_history", "source_posts", "source_collection_revisions", "scenes", "files"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE enrichment_rebindings(original TEXT); INSERT INTO enrichment_rebindings VALUES('retained unknown record')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
			err = db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				var value string
				require.NoError(t, raw.QueryRow("SELECT original FROM enrichment_rebindings").Scan(&value))
				require.Equal(t, "retained unknown record", value)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='enrichment_rebinding_targets'"))
			} else {
				require.NoError(t, err)
				require.NoError(t, db.ReInitialise())
				require.NoError(t, db.Close())
				require.NoError(t, db.Open(db.DatabasePath()))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_rebindings"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}

func TestEnrichmentRebindStartupRejectsIncompleteReceiptBeforeWriting(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	target := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now)
	collection = changedEnrichmentCollection(t, repo, collection)
	_, err := applyEnrichmentRebind(repo, previewEnrichmentRebind(t, repo, enrichmentRebindInput(collection, target)), now)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("DELETE FROM enrichment_rebinding_targets")
	require.NoError(t, err)
	before, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, db.Open(db.DatabasePath()), "enrichment collection")
	after, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestEnrichmentRebindStartupChecksHistoricalCollectionDefinition(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	target := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now)
	collection = changedEnrichmentCollection(t, repo, collection)
	plan := previewEnrichmentRebind(t, repo, enrichmentRebindInput(collection, target))
	_, err := applyEnrichmentRebind(repo, plan, now)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	var trigger string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='enrichment_rebinding_immutable'").Scan(&trigger))
	_, err = raw.Exec("DROP TRIGGER enrichment_rebinding_immutable")
	require.NoError(t, err)
	plan.Collection.Label = "Different definition with a valid plan digest"
	plan.PlanSHA256, err = archive.EnrichmentRebindDigest(*plan)
	require.NoError(t, err)
	body, err := json.Marshal(plan)
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE enrichment_rebindings SET plan=?,plan_sha256=?", string(body), plan.PlanSHA256)
	require.NoError(t, err)
	_, err = raw.Exec(trigger)
	require.NoError(t, err)
	before, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, db.Open(db.DatabasePath()), "invalid enrichment collection rebindings")
	after, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
