package sqlite_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Historical fixtures below 130 need the actual blob column, not a current
// normalized schema with its version marker merely changed.
func removeSnapshotMembershipSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	if queryUint(t, raw, `SELECT count(*) FROM pragma_table_info('shares') WHERE name='snapshot'`) > 0 {
		return
	}
	require.Zero(t, queryUint(t, raw, `SELECT count(*) FROM share_snapshot_media`), "downgrade helper only accepts empty fixture snapshots")
	_, err := raw.Exec(`DROP TRIGGER share_snapshot_entry_insert;
DROP TRIGGER share_snapshot_member_insert;
DROP TRIGGER share_snapshot_media_insert;
DROP TRIGGER share_snapshot_entry_delete;
DROP TRIGGER share_snapshot_member_delete;
DROP TRIGGER share_snapshot_media_delete;
DROP TRIGGER share_snapshot_admission_closed;
DROP TRIGGER share_snapshot_counts_immutable;
DROP TRIGGER share_snapshot_checkpoint;
DROP TRIGGER share_snapshot_ready;
DROP TRIGGER share_snapshot_sealed;
DROP INDEX shares_preparing;
DROP TABLE share_snapshot_members;
DROP TABLE share_snapshot_entries;
DROP TABLE share_snapshot_media;
ALTER TABLE shares DROP COLUMN preparation_state;
ALTER TABLE shares DROP COLUMN preparation_revision;
ALTER TABLE shares DROP COLUMN preparation_error;
ALTER TABLE shares DROP COLUMN prepared_count;
ALTER TABLE shares DROP COLUMN media_count;
ALTER TABLE shares DROP COLUMN selection_count;
ALTER TABLE shares ADD COLUMN snapshot BLOB NOT NULL DEFAULT '{"entries":[],"media":[]}';
DELETE FROM native_migration_history WHERE version=1000130;`)
	require.NoError(t, err)
}

func TestSnapshotMembershipMigrationPreservesHistoricalGrant(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "schema-129.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	migrator, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := migrator.CurrentSchemaVersion(); version < 1000129; version = migrator.CurrentSchemaVersion() {
		require.NoError(t, migrator.RunMigration(t.Context(), migrator.GetNextMigrationVersion(version)))
	}
	migrator.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	payload := `{"entries":[{"key":"gallery-1","kind":"GALLERY","id":7,"uuid":"old-gallery-uuid","title":"Frozen gallery","media_keys":["image-2","scene-1"]},{"kind":"SCENE","id":1,"title":"Frozen scene","media_keys":["scene-1"]}],"media":[{"key":"image-2","kind":"IMAGE","entity_id":2,"uuid":"old-image-uuid","file_id":42,"title":"Frozen image","fingerprint":{"version":1,"size":12345,"mod_time_nano":1760000000123456789}},{"key":"scene-1","kind":"SCENE","entity_id":1,"file_id":43,"title":"Frozen scene","fingerprint":{"version":1,"size":456}}]}`
	_, err = raw.Exec(`INSERT INTO shares(id,token_hash,token,label,created_by,created_at,expires_at,version,snapshot,source) VALUES('old',X'001122','AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA','Old','owner',1,9999999999,4,?,'{}');
INSERT INTO share_sessions(token_hash,share_id,version,expires_at) VALUES(X'334455','old',4,9999999999)`, payload)
	require.NoError(t, err)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	var token, state string
	var version, count, prepared, entries int
	require.NoError(t, raw.QueryRow(`SELECT token,version,preparation_state,media_count,prepared_count,selection_count FROM shares WHERE id='old'`).Scan(&token, &version, &state, &count, &prepared, &entries))
	require.Equal(t, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", token)
	require.Equal(t, 4, version)
	require.Equal(t, "READY", state)
	require.Equal(t, 2, count)
	require.Equal(t, count, prepared)
	require.Equal(t, 2, entries)
	require.EqualValues(t, 1, queryUint(t, raw, `SELECT count(*) FROM shares WHERE token_hash=X'001122'`))
	require.EqualValues(t, 1, queryUint(t, raw, `SELECT count(*) FROM share_sessions WHERE token_hash=X'334455' AND version=4`))
	var body string
	require.NoError(t, raw.QueryRow(`SELECT json_group_array(json(media)) FROM (SELECT media FROM share_snapshot_media WHERE share_id='old' ORDER BY position)`).Scan(&body))
	var original struct {
		Media json.RawMessage `json:"media"`
	}
	require.NoError(t, json.Unmarshal([]byte(payload), &original))
	require.JSONEq(t, string(original.Media), body, "stored fingerprints and metadata are copied, never recalculated")
	require.NoError(t, raw.QueryRow(`SELECT group_concat(media_key,',') FROM (SELECT media_key FROM share_snapshot_members WHERE share_id='old' AND entry_key='gallery-1' ORDER BY position)`).Scan(&body))
	require.Equal(t, "image-2,scene-1", body)
	require.EqualValues(t, 1, queryUint(t, raw, `SELECT count(*) FROM share_snapshot_entries WHERE key='gallery-2' AND entity_uuid=''`), "legacy missing keys retain their public gallery ordinal")
	for _, statement := range []string{
		`UPDATE share_snapshot_entries SET title='changed'`,
		`DELETE FROM share_snapshot_members`,
		`UPDATE share_snapshot_media SET prepared=0`,
		`UPDATE shares SET media_count=3 WHERE id='old'`,
		`UPDATE shares SET preparation_state='PREPARING' WHERE id='old'`,
		`INSERT INTO share_snapshot_entries(share_id,key,position,kind,entity_id,entity_uuid,title) VALUES('old','new',3,'IMAGE',3,'new-uuid','new')`,
	} {
		_, err = raw.Exec(statement)
		require.Error(t, err, statement)
	}
	require.Zero(t, queryUint(t, raw, `SELECT count(*) FROM pragma_foreign_key_check`))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	_, err = raw.Exec(`DELETE FROM shares WHERE id='old'`)
	require.NoError(t, err)
	require.Zero(t, queryUint(t, raw, `SELECT count(*) FROM share_snapshot_media`))
	require.Zero(t, queryUint(t, raw, `SELECT count(*) FROM share_sessions`))
}
