package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeSavedShareLinkSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	if queryUint(t, raw, "SELECT count(*) FROM pragma_table_info('shares') WHERE name='token'") == 0 {
		return
	}
	_, err := raw.Exec(`ALTER TABLE shares DROP COLUMN token;
ALTER TABLE share_sessions ADD COLUMN preview BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE share_sessions ADD COLUMN exchange_only BOOLEAN NOT NULL DEFAULT 0;
DELETE FROM native_migration_history WHERE version=1000122`)
	require.NoError(t, err)
}

func TestSavedShareLinkMigrationPreservesLinksAndOpenSessions(t *testing.T) {
	db, _ := archiveTestDatabase(t)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeSavedShareLinkSchema(t, raw)
	_, err := raw.Exec(`UPDATE schema_migrations SET version=1000121;
INSERT INTO shares(id,token_hash,label,created_by,created_at,expires_at,version,access_count,snapshot)
VALUES('existing',X'001122','Existing share','owner',1,9999999999,3,12,'{"media":[]}');
INSERT INTO share_sessions(token_hash,share_id,version,expires_at,preview,exchange_only) VALUES
(X'003344','existing',3,9999999999,0,0),
(X'005566','existing',3,9999999000,1,0),
(X'007788','existing',3,9999998000,1,1);`)
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.EqualValues(t, 1000122, queryUint(t, raw, "SELECT version FROM schema_migrations"))
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		row, err := repo.Share.Find(ctx, "existing")
		require.NoError(t, err)
		require.Empty(t, row.Token, "hash-only links are not silently regenerated")
		require.Equal(t, []byte{0, 0x11, 0x22}, row.TokenHash)
		require.Equal(t, 3, row.Version)
		require.Equal(t, 12, row.AccessCount)
		require.JSONEq(t, `{"media":[]}`, string(row.SnapshotJSON))
		for _, expected := range []models.ShareSession{
			{TokenHash: []byte{0, 0x33, 0x44}, ShareID: "existing", Version: 3, ExpiresAt: 9999999999},
			{TokenHash: []byte{0, 0x55, 0x66}, ShareID: "existing", Version: 3, ExpiresAt: 9999999000},
		} {
			session, err := repo.Share.FindSession(ctx, expected.TokenHash)
			require.NoError(t, err)
			require.Equal(t, expected, *session)
		}
		bootstrap, err := repo.Share.FindSession(ctx, []byte{0, 0x77, 0x88})
		require.NoError(t, err)
		require.Nil(t, bootstrap, "unused preview bootstraps cannot become media credentials")
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM share_sessions"))
}
