package migrations

import (
	"context"
	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/sqlite"
)

func migrateShares(ctx context.Context, db *sqlx.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS fork_shares (
  id TEXT PRIMARY KEY,
  token_hash BLOB NOT NULL,
  label TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  revoked_at INTEGER NOT NULL DEFAULT 0,
  allow_download BOOLEAN NOT NULL DEFAULT 0,
  show_metadata BOOLEAN NOT NULL DEFAULT 0,
  version INTEGER NOT NULL DEFAULT 1,
  access_count INTEGER NOT NULL DEFAULT 0,
  last_accessed_at INTEGER NOT NULL DEFAULT 0,
  snapshot BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS fork_shares_created ON fork_shares(created_at DESC, id);
CREATE TABLE IF NOT EXISTS fork_share_sessions (
  token_hash BLOB PRIMARY KEY,
  share_id TEXT NOT NULL REFERENCES fork_shares(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  preview BOOLEAN NOT NULL DEFAULT 0,
  exchange_only BOOLEAN NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS fork_share_sessions_share ON fork_share_sessions(share_id);
CREATE INDEX IF NOT EXISTS fork_share_sessions_expiry ON fork_share_sessions(expires_at);
`)
	return err
}

func init() {
	sqlite.RegisterForkMigration(8, "expiring media shares", migrateShares)
	sqlite.RegisterForkReconciler("expiring media shares", migrateShares)
}
