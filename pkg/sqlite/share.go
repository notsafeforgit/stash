package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"github.com/stashapp/stash/pkg/models"
	"time"
)

type ShareStore struct{}

func (*ShareStore) Find(ctx context.Context, id string) (*models.ShareRecord, error) {
	var row models.ShareRecord
	err := dbWrapper.Get(ctx, &row, `SELECT * FROM fork_shares WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &row, err
}

func (*ShareStore) List(ctx context.Context, limit, offset int) ([]*models.ShareRecord, error) {
	ret := []*models.ShareRecord{}
	err := dbWrapper.Select(ctx, &ret, `SELECT * FROM fork_shares ORDER BY created_at DESC, id LIMIT ? OFFSET ?`, limit, offset)
	return ret, err
}

func (*ShareStore) Create(ctx context.Context, s *models.ShareRecord) error {
	_, err := dbWrapper.Exec(ctx, `INSERT INTO fork_shares
(id, token_hash, label, created_by, created_at, expires_at, allow_download, show_metadata, version, snapshot)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.ID, s.TokenHash, s.Label, s.CreatedBy, s.CreatedAt, s.ExpiresAt, s.AllowDownload, s.ShowMetadata, s.Version, s.SnapshotJSON)
	return err
}

func (*ShareStore) Update(ctx context.Context, s *models.ShareRecord) error {
	_, err := dbWrapper.Exec(ctx, `UPDATE fork_shares SET token_hash = ?, label = ?, expires_at = ?, revoked_at = ?, allow_download = ?, show_metadata = ?, version = ? WHERE id = ?`, s.TokenHash, s.Label, s.ExpiresAt, s.RevokedAt, s.AllowDownload, s.ShowMetadata, s.Version, s.ID)
	return err
}

func (*ShareStore) PutSession(ctx context.Context, s *models.ShareSession) error {
	if _, err := dbWrapper.Exec(ctx, `DELETE FROM fork_share_sessions WHERE expires_at <= ?`, time.Now().Unix()); err != nil {
		return err
	}
	// Bound persistent state even when somebody repeatedly opens a valid link.
	var count int
	if err := dbWrapper.Get(ctx, &count, `SELECT count(*) FROM fork_share_sessions WHERE share_id = ?`, s.ShareID); err != nil {
		return err
	}
	if count >= 1000 {
		return errors.New("share session limit reached")
	}
	_, err := dbWrapper.Exec(ctx, `INSERT INTO fork_share_sessions (token_hash, share_id, version, expires_at, preview, exchange_only) VALUES (?, ?, ?, ?, ?, ?)`, s.TokenHash, s.ShareID, s.Version, s.ExpiresAt, s.Preview, s.ExchangeOnly)
	return err
}

func (*ShareStore) FindSession(ctx context.Context, hash []byte) (*models.ShareSession, error) {
	var row models.ShareSession
	err := dbWrapper.Get(ctx, &row, `SELECT * FROM fork_share_sessions WHERE token_hash = ?`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &row, err
}

func (*ShareStore) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := dbWrapper.Exec(ctx, `DELETE FROM fork_share_sessions WHERE token_hash = ?`, hash)
	return err
}

func (*ShareStore) DeleteSessions(ctx context.Context, id string) error {
	_, err := dbWrapper.Exec(ctx, `DELETE FROM fork_share_sessions WHERE share_id = ?`, id)
	return err
}

func (*ShareStore) RecordAccess(ctx context.Context, id string, at int64) error {
	_, err := dbWrapper.Exec(ctx, `UPDATE fork_shares SET access_count = access_count + 1, last_accessed_at = ? WHERE id = ?`, at, id)
	return err
}
