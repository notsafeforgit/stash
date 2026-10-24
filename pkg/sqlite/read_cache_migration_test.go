package sqlite_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

// Reconstruct the pre-131 fixture before older migration tests remove any of
// its dependencies. Never make production migrations tolerate object collisions.
func removeReadCacheAndPayloadRetentionSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	if queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='catalog_payload_retirements'") > 0 {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_payload_retirements"), "historical fixture construction requires unretired inputs")
		_, err := raw.Exec(`DROP TRIGGER catalog_payload_retirement_count;
DROP TRIGGER catalog_snapshot_record_immutable;
DROP TRIGGER catalog_payload_retirement_begin;
DROP TRIGGER catalog_payload_retirement_progress;
DROP TRIGGER catalog_payload_retirement_retained;
DROP VIEW catalog_retirable_payloads;
DROP VIEW catalog_retirable_snapshots;
DROP INDEX catalog_retired_payloads;
DROP TABLE catalog_payload_retirements;
ALTER TABLE catalog_snapshot_records DROP COLUMN original_byte_count;
ALTER TABLE catalog_snapshot_records DROP COLUMN retained_asset_id;
CREATE TRIGGER catalog_snapshot_record_immutable BEFORE UPDATE ON catalog_snapshot_records
BEGIN SELECT RAISE(ABORT,'staged catalog source records are immutable'); END;
DELETE FROM native_migration_history WHERE version=1000132;`)
		require.NoError(t, err)
	}
	if queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='library_search_state'") > 0 {
		triggers := func() []string {
			rows, err := raw.Query("SELECT name FROM sqlite_schema WHERE type='trigger' AND name GLOB 'library_search_*'")
			require.NoError(t, err)
			defer rows.Close()
			var names []string
			for rows.Next() {
				var name string
				require.NoError(t, rows.Scan(&name))
				names = append(names, name)
			}
			require.NoError(t, rows.Err())
			return names
		}()
		for _, name := range triggers {
			_, err := raw.Exec(`DROP TRIGGER "` + name + `"`)
			require.NoError(t, err)
		}
		_, err := raw.Exec("DROP TABLE library_search_state; DELETE FROM native_migration_history WHERE version=1000131")
		require.NoError(t, err)
	}
}
