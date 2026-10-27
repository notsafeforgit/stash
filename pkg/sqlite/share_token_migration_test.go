package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeShareTokenLookupSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeAccountProfileSchema(t, raw)
	_, err := raw.Exec(`DROP INDEX IF EXISTS shares_token_hash;
DELETE FROM native_migration_history WHERE version=1000133`)
	require.NoError(t, err)
}

func TestShareTokenLookupMigrationPreservesGrantsAndSessions(t *testing.T) {
	const secret = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	hash := sha256.Sum256([]byte(secret))
	db, _ := archiveTestDatabase(t)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeShareTokenLookupSchema(t, raw)
	_, err := raw.Exec(`UPDATE schema_migrations SET version=1000132;
INSERT INTO shares(id,token_hash,token,label,created_by,created_at,expires_at,version,access_count)
VALUES('saved',?,?,'Saved','owner',1,9999999999,3,12),
('hash-only',X'334455','','Historical','owner',2,9999999999,2,4);
INSERT INTO share_sessions(token_hash,share_id,version,expires_at)
VALUES(X'778899','saved',3,9999999999),(X'AABBCC','hash-only',2,9999999999)`, hash[:], secret)
	require.NoError(t, err)
	// Exercise the lookup planner with history large enough to favor an index;
	// SQLite legitimately scans a two-row table after migration optimization.
	_, err = raw.Exec(`WITH RECURSIVE history(n) AS (
SELECT 1 UNION ALL SELECT n+1 FROM history WHERE n<512
)
INSERT INTO shares(id,token_hash,label,created_by,created_at,expires_at)
SELECT 'history-'||n,randomblob(32),'Expired','owner',1,2 FROM history`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, db.Open(path), &needed)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	// EXPLAIN does not execute a statement that refreshes an older connection's
	// schema cache. Inspect the migrated schema through a fresh connection.
	raw = openRawDB(t, path)
	defer raw.Close()
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, id := range []string{"saved", "hash-only"} {
			row, err := repo.Share.Find(ctx, id)
			require.NoError(t, err)
			resolved, err := repo.Share.FindByTokenHash(ctx, row.TokenHash)
			require.NoError(t, err)
			require.Equal(t, row, resolved)
			if id == "saved" {
				require.Equal(t, secret, row.Token)
				require.Equal(t, 3, row.Version)
				require.Equal(t, 12, row.AccessCount)
			} else {
				require.Empty(t, row.Token)
				require.Equal(t, 2, row.Version)
				require.Equal(t, 4, row.AccessCount)
			}
		}
		missing, err := repo.Share.FindByTokenHash(ctx, []byte("missing"))
		require.NoError(t, err)
		require.Nil(t, missing)
		for _, hash := range [][]byte{{0x77, 0x88, 0x99}, {0xaa, 0xbb, 0xcc}} {
			session, err := repo.Share.FindSession(ctx, hash)
			require.NoError(t, err)
			require.NotNil(t, session)
			require.EqualValues(t, 9999999999, session.ExpiresAt)
		}
		return nil
	}))
	var plan string
	var a, b, c int
	require.NoError(t, raw.QueryRow(`EXPLAIN QUERY PLAN SELECT * FROM shares WHERE token_hash = ?`, hash[:]).Scan(&a, &b, &c, &plan))
	require.Contains(t, plan, "USING INDEX shares_token_hash")
	_, err = raw.Exec(`UPDATE shares SET token_hash=? WHERE id='hash-only'`, hash[:])
	require.ErrorContains(t, err, "UNIQUE constraint failed")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
