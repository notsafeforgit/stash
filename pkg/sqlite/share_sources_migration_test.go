package sqlite_test

import (
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestShareSourcesMigrationPreservesSnapshotsAndCapabilities(t *testing.T) {
	db, _ := archiveTestDatabase(t)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	_, err := raw.Exec(`DROP TRIGGER share_source_immutable;
ALTER TABLE shares DROP COLUMN source;
DELETE FROM native_migration_history WHERE version=1000129;
UPDATE schema_migrations SET version=1000128;
INSERT INTO shares(id,token_hash,token,label,created_by,created_at,expires_at,version,snapshot)
VALUES('existing',X'001122','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','Old grant','owner',1,9999999999,3,'{"entries":[],"media":[],"retained":"opaque historical data"}');
INSERT INTO share_sessions(token_hash,share_id,version,expires_at) VALUES(X'003344','existing',3,9999999999);`)
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	var snapshot, source, token string
	var version int
	require.NoError(t, raw.QueryRow(`SELECT snapshot,source,token,version FROM shares WHERE id='existing'`).Scan(&snapshot, &source, &token, &version))
	require.Equal(t, `{"entries":[],"media":[],"retained":"opaque historical data"}`, snapshot)
	require.Equal(t, `{}`, source)
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", token)
	require.Equal(t, 3, version)
	require.EqualValues(t, 1, queryUint(t, raw, `SELECT count(*) FROM share_sessions WHERE share_id='existing' AND version=3`))
	_, err = raw.Exec(`UPDATE shares SET source='{"mode":"ONGOING"}' WHERE id='existing'`)
	require.ErrorContains(t, err, "immutable")
	require.Zero(t, queryUint(t, raw, `SELECT count(*) FROM pragma_foreign_key_check`))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
}
